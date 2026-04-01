package stepwise

/*
EmbeddedHeaderMagic sits in bits 48–63 of the first word of the embedded program
band. Legacy packed 16-bit instructions are extremely unlikely to match this
exact pattern with bits 16–47 clear, so the backend can route stepwise vs RISC.
*/
const EmbeddedHeaderMagic uint16 = 0x5A17

/*
PackEmbeddedHeader builds the word stored at ctx[EmbeddedProgramBase()] before
descriptor words. stepCount is how many uint64 descriptor words follow; 0 means
run zero steps.
*/
func PackEmbeddedHeader(stepCount uint16) uint64 {

	return uint64(EmbeddedHeaderMagic)<<48 | uint64(stepCount)
}

/*
ValidEmbeddedHeader reports whether word is a well-formed stepwise band header.
*/
func ValidEmbeddedHeader(word uint64) bool {

	if uint16(word>>48) != EmbeddedHeaderMagic {
		return false
	}

	if (word>>16)&0xFFFFFFFF != 0 {
		return false
	}

	return true
}

/*
EmbeddedDescriptorCount returns how many descriptor words follow the header, or
false if word is not a header.
*/
func EmbeddedDescriptorCount(word uint64) (steps int, ok bool) {

	if !ValidEmbeddedHeader(word) {
		return 0, false
	}

	return int(word & 0xFFFF), true
}

/*
DetectEmbeddedStepwise returns true when frame a has a valid stepwise header at
the configured program base.
*/
func DetectEmbeddedStepwise(a *[FrameWords]uint64) bool {

	if a == nil {
		return false
	}

	base := EmbeddedProgramBase()
	if base < 0 || base >= FrameWords {
		return false
	}

	return ValidEmbeddedHeader(a[base])
}
