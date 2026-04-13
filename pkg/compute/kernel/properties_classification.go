package kernel

import (
	"hash/fnv"
	"strconv"
	"strings"
)

/*
Properties word 48 (PropertiesStartWord) is the first word of the 512-bit
Properties band. README documents WORD 0 as four packed label slots; we use
four 16-bit lanes in the low 64 bits. The substrate may still write a small
integer class index into the whole word (legacy); PrimaryClassFromPropertiesWord
accepts both conventions.

Higher words (49–55) hold confidence, epoch, role, TTL/noise, probe ABI, etc.
Callers that only care about a single predicted class for N-way classification
should use the helpers below rather than interpreting the raw uint64.
*/

const propertiesLabelSlotSentinel uint16 = 0xffff

/*
PackClassificationLabelSlots packs up to four 16-bit label slots into the
low 64 bits of the Properties labels word. Unused slots should be set to
propertiesLabelSlotSentinel so readers can ignore them.
*/
func PackClassificationLabelSlots(a, b, c, d uint16) uint64 {
	return uint64(a) |
		uint64(b)<<16 |
		uint64(c)<<32 |
		uint64(d)<<48
}

/*
UnpackClassificationLabelSlots returns the four 16-bit lanes from the labels word.
*/
func UnpackClassificationLabelSlots(word uint64) [4]uint16 {
	return [4]uint16{
		uint16(word),
		uint16(word >> 16),
		uint16(word >> 32),
		uint16(word >> 48),
	}
}

/*
PrimaryClassFromPropertiesWord returns the best-effort discrete class index for
small-cardinality tasks (e.g. AG News with four classes).

  - If the upper 48 bits are zero, the low 16 bits are treated as the primary
    index (covers both “write 3 in the word” legacy paths and a single packed slot).
  - Otherwise the first slot strictly less than maxClass and not equal to the
    sentinel is returned.
  - If maxClass <= 0, it defaults to 1024 as a loose upper bound.

If no valid slot is found, ok is false and idx should be ignored.
*/
func PrimaryClassFromPropertiesWord(word uint64, maxClass int) (idx int, ok bool) {
	if maxClass <= 0 {
		maxClass = 1024
	}

	if word>>16 == 0 {
		v := int(uint16(word))

		if v >= 0 && v < maxClass {
			return v, true
		}

		return 0, false
	}

	for _, lane := range UnpackClassificationLabelSlots(word) {
		if lane == propertiesLabelSlotSentinel {
			continue
		}

		v := int(lane)

		if v >= 0 && v < maxClass {
			return v, true
		}
	}

	return 0, false
}

/*
GoldLabelWord packs a single supervised class index for ingest-side stamping
into Properties word 0 (dataset Values). Prediction paths should still prefer
PrimaryClassFromPropertiesWord so the same word layout is used for train and test.
*/
func GoldLabelWord(classIdx int) uint64 {
	if classIdx < 0 || classIdx > int(^uint16(0)) {
		return 0
	}

	return uint64(uint16(classIdx))
}

/*
LabelPropertiesWord maps supervision bytes to a single Properties word for ingest.

Decimal integers in 0..65535 use GoldLabelWord. Any other payload (class names,
free text, non-numeric labels) is folded with FNV-1a 64 so arbitrary []byte
still leaves a deterministic fingerprint in the Properties band.
*/
func LabelPropertiesWord(label []byte) uint64 {
	if len(label) == 0 {
		return 0
	}

	trimmed := strings.TrimSpace(string(label))
	if trimmed != "" {
		if n, err := strconv.ParseUint(trimmed, 10, 16); err == nil {
			return GoldLabelWord(int(n))
		}
	}

	h := fnv.New64a()
	h.Write(label)

	return h.Sum64()
}
