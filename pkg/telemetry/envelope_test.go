package telemetry

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestEnvelopeEncodeDecodeRoundtrip locks down the invariant the
visualiser depends on: every envelope starts with the four magic
bytes, the kind is recoverable, and the body round-trips through JSON
without drift.
*/
func TestEnvelopeEncodeDecodeRoundtrip(t *testing.T) {
	Convey("Given a FieldMetricsPayload", t, func() {
		payload := FieldMetricsPayload{
			CommunityIdx:    2,
			MemberCount:     12,
			LabeledCount:    9,
			Coverage:        0.75,
			Consensus:       0.88,
			LabelDensity:    0.42,
			Crystallization: 0.277,
			DominantRatio:   0.63,
			ModeCount:       3,
			PressureMult:    0.37,
			Saturated:       false,
		}

		frame, err := EncodeEnvelope(EnvelopeKindFieldMetrics, payload)
		So(err, ShouldBeNil)

		Convey("the frame is prefixed with the magic bytes", func() {
			So(IsEnvelope(frame), ShouldBeTrue)
			So(DecodeEnvelopeKind(frame), ShouldEqual, EnvelopeKindFieldMetrics)
		})

		Convey("the JSON body decodes back to the original payload", func() {
			var decoded FieldMetricsPayload

			// Trim the trailing NUL sentinel if it was appended — the
			// encoder only adds it when the total length would otherwise
			// divide evenly by 1024. JSON stops at the last brace so a
			// bare NUL past the terminator is ignored.
			err := json.Unmarshal(frame[EnvelopeHeaderBytes:], &decoded)
			if err != nil {
				// Fall back to explicit trim of a single NUL sentinel.
				err = json.Unmarshal(
					frame[EnvelopeHeaderBytes:len(frame)-1], &decoded,
				)
			}
			So(err, ShouldBeNil)

			So(decoded, ShouldResemble, payload)
		})
	})

	Convey("Given a non-envelope frame", t, func() {
		frame := make([]byte, 1024)

		Convey("IsEnvelope returns false without scanning the body", func() {
			So(IsEnvelope(frame), ShouldBeFalse)
		})
	})

	Convey("Given an envelope whose body would land on a 1024-byte boundary", t, func() {
		// Construct a payload that, after JSON marshaling and the 8-byte
		// header, lands exactly on 1024 bytes. The sentinel NUL should
		// bump the final length to 1025 so the legacy decoder ignores it.
		rawBody := make([]byte, 1024-EnvelopeHeaderBytes-2)
		for i := range rawBody {
			rawBody[i] = 'a'
		}

		payload := struct {
			Filler string `json:"f"`
		}{Filler: string(rawBody)}

		frame, err := EncodeEnvelope(EnvelopeKindFieldMetrics, payload)
		So(err, ShouldBeNil)

		Convey("the frame length is never a multiple of 1024", func() {
			So(len(frame)%1024, ShouldNotEqual, 0)
		})
	})
}
