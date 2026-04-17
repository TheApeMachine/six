package telemetry

import (
	"encoding/binary"
	"encoding/json"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
EnvelopeMagic is the four-byte tag every structured telemetry envelope
starts with. The bytes spell "VZB1" so a human staring at a packet
dump can immediately tell an envelope apart from a raw 1024-byte Value
frame without running a decoder.
*/
var EnvelopeMagic = [4]byte{'V', 'Z', 'B', '1'}

/*
EnvelopeHeaderBytes is the fixed on-wire header size preceding every
envelope payload. 4 bytes of magic + 4 bytes of envelope kind. A
constant keeps the bridge-side quick-peek check a single compare.
*/
const EnvelopeHeaderBytes = 8

/*
Envelope kinds. Raw Value frames have no envelope — they are detected
client-side by "length is a multiple of 1024 AND does not start with
the magic", so new kinds can be added here without breaking readers
that only know kind 1/2.
*/
const (
	EnvelopeKindFieldMetrics = uint32(1)
	EnvelopeKindCausalEvent  = uint32(2)
)

/*
FieldMetricsPayload mirrors mesh.FieldMetrics on the wire. A separate
struct keeps the serialization stable even if mesh grows new internal
fields later — the visualiser depends on this exact schema.

CommunityIdx is set by the emitter so the visualiser can route the
envelope to the right bucket in its community table without a
secondary lookup.
*/
type FieldMetricsPayload struct {
	CommunityIdx    int     `json:"communityIdx"`
	MemberCount     int     `json:"memberCount"`
	LabeledCount    int     `json:"labeledCount"`
	SlotSum         int     `json:"slotSum"`
	Coverage        float64 `json:"coverage"`
	Consensus       float64 `json:"consensus"`
	LabelDensity    float64 `json:"labelDensity"`
	Crystallization float64 `json:"crystallization"`
	DominantRatio   float64 `json:"dominantRatio"`
	ModeCount       int     `json:"modeCount"`
	PressureMult    float64 `json:"pressureMult"`
	Saturated       bool    `json:"saturated"`
}

/*
CausalEventPayload reports causal/falsification state transitions to
the visualiser so it can render when a Value hypothesises, gets
refuted, or iterates its counterfactual. Hashes let the client
correlate with the raw Value frame stream without needing the full
128-word image.
*/
type CausalEventPayload struct {
	ValueID   uint64 `json:"valueId"`
	Kind      string `json:"kind"` // "hypothesize" | "falsified" | "iterate_causal" | "do_intervention"
	Timestamp int64  `json:"timestamp"`
}

/*
EncodeEnvelope writes the 8-byte header and JSON body, and pads the
payload with a single NUL byte when the total length would otherwise
divide evenly by 1024. That guarantees the legacy Value-frame decoder
ignores envelopes even when magic bytes happen to match — the decoder
only advances over whole 128-word frames.
*/
func EncodeEnvelope(kind uint32, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)

	if err != nil {
		return nil, errnie.Error(err)
	}

	total := EnvelopeHeaderBytes + len(body)

	if total%1024 == 0 {
		// Append a sentinel NUL so the legacy decoder's
		// len % 1024 == 0 branch cannot swallow us as frames.
		body = append(body, 0)
		total++
	}

	frame := make([]byte, total)

	frame[0] = EnvelopeMagic[0]
	frame[1] = EnvelopeMagic[1]
	frame[2] = EnvelopeMagic[2]
	frame[3] = EnvelopeMagic[3]

	binary.LittleEndian.PutUint32(frame[4:EnvelopeHeaderBytes], kind)

	copy(frame[EnvelopeHeaderBytes:], body)

	return frame, nil
}

/*
IsEnvelope returns true when the first 4 bytes match EnvelopeMagic. A
cheap O(4) compare is all the bridge/fan-out layer needs to decide
whether to treat a message as structured telemetry.
*/
func IsEnvelope(frame []byte) bool {
	if len(frame) < EnvelopeHeaderBytes {
		return false
	}

	return frame[0] == EnvelopeMagic[0] &&
		frame[1] == EnvelopeMagic[1] &&
		frame[2] == EnvelopeMagic[2] &&
		frame[3] == EnvelopeMagic[3]
}

/*
DecodeEnvelopeKind returns the envelope kind after a successful
IsEnvelope check. The kind byte is little-endian uint32 in the second
word of the header.
*/
func DecodeEnvelopeKind(frame []byte) uint32 {
	if len(frame) < EnvelopeHeaderBytes {
		return 0
	}

	return binary.LittleEndian.Uint32(frame[4:EnvelopeHeaderBytes])
}
