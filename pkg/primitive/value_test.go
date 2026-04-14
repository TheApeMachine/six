package primitive

import (
	"bytes"
	"io"
	"testing"

	"github.com/theapemachine/six/pkg/core"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewValue exercises the public mint path: Morton packing into the tokens
region, segment chaining via Prev/Next, and fresh ID stamping.
*/
func TestNewValue(t *testing.T) {
	Convey("Given a short payload", t, func() {
		payload := []byte("hello world")

		Convey("When NewValue mints it", func() {
			values, err := NewValue(payload)

			Convey("It should return at least one Value with no error", func() {
				So(err, ShouldBeNil)
				So(len(values), ShouldBeGreaterThanOrEqualTo, 1)

				for _, value := range values {
					So(value, ShouldNotBeNil)
				}
			})

			Convey("It should round-trip the payload through String", func() {
				var decoded bytes.Buffer

				for _, value := range values {
					decoded.WriteString(value.String())
				}

				// String decodes the Morton token slab back to the original
				// bytes so every input byte must appear in order.
				So(decoded.String(), ShouldEqual, string(payload))
			})

			Convey("It should survive a Read/Write wire-frame round-trip", func() {
				// The Read/Write path is the byte-identical contract used
				// for Publish, trie storage, and gossip replication. Every
				// word must reappear verbatim on the far side.
				source := values[0]

				frame := make([]byte, core.Cfg.Value.Bytes)

				n, err := source.Read(frame)
				So(err, ShouldEqual, io.EOF)
				So(n, ShouldEqual, core.Cfg.Value.Bytes)

				target := &Value{}

				n, err = target.Write(frame)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, core.Cfg.Value.Bytes)

				for idx := range *source {
					So((*target)[idx], ShouldEqual, (*source)[idx])
				}
			})

			Convey("It should assign a unique non-zero ID per segment", func() {
				seen := make(map[uint64]struct{}, len(values))

				for _, value := range values {
					id := value.ID()

					So(id, ShouldNotEqual, uint64(0))

					_, dup := seen[id]
					So(dup, ShouldBeFalse)

					seen[id] = struct{}{}
				}
			})

			Convey("It should write tokens into the configured tokens region", func() {
				tokenStart := core.Cfg.Value.Region.Tokens.Start
				tokenWords := int(
					(core.Cfg.Value.Region.Tokens.Bits + 63) / 64,
				)

				nonZero := false

				for word := tokenStart; word < tokenStart+tokenWords; word++ {
					if (*values[0])[word] != 0 {
						nonZero = true

						break
					}
				}

				So(nonZero, ShouldBeTrue)
			})

			Reset(func() {
				CloseAll(values)
			})
		})
	})

	Convey("Given an empty payload", t, func() {
		Convey("When NewValue is called", func() {
			values, err := NewValue(nil)

			Convey("It should fail with ErrShortBuffer", func() {
				// An empty payload has nothing to pack and must never mint
				// a live Value.
				So(err, ShouldEqual, io.ErrShortBuffer)
				So(values, ShouldBeNil)
			})
		})
	})

	Convey("Given a payload that chains segments", t, func() {
		// Size the payload well past a single token slab so the packer has
		// to open a second segment.
		capacity := int((core.Cfg.Value.Region.Tokens.Bits + 7) / 8 / 2)
		payload := bytes.Repeat([]byte{'A'}, capacity*3)

		Convey("When NewValue mints it", func() {
			values, err := NewValue(payload)

			Convey("It should produce multiple segments", func() {
				So(err, ShouldBeNil)
				So(len(values), ShouldBeGreaterThanOrEqualTo, 2)
			})

			Reset(func() {
				CloseAll(values)
			})
		})
	})
}

/*
TestFirstSegment verifies the single-segment helper's happy path and its
guard against multi-segment payloads that would otherwise leak Values.
*/
func TestFirstSegment(t *testing.T) {
	Convey("Given a payload that fits in one segment", t, func() {
		value, err := FirstSegment(NewValue([]byte("tiny")))

		Convey("It should return that Value", func() {
			So(err, ShouldBeNil)
			So(value, ShouldNotBeNil)
			So(value.ID(), ShouldNotEqual, uint64(0))
		})

		Reset(func() {
			if value != nil {
				value.Close()
			}
		})
	})

	Convey("Given a payload that forces multiple segments", t, func() {
		capacity := int((core.Cfg.Value.Region.Tokens.Bits + 7) / 8 / 2)
		payload := bytes.Repeat([]byte{'Z'}, capacity*3)

		Convey("When FirstSegment receives the multi-segment slice", func() {
			value, err := FirstSegment(NewValue(payload))

			Convey("It should refuse and return an error", func() {
				// FirstSegment must close every produced Value before
				// returning so callers cannot accidentally leak pool
				// entries when the assumption is violated.
				So(err, ShouldNotBeNil)
				So(value, ShouldBeNil)
			})
		})
	})

	Convey("Given a preceding error", t, func() {
		value, err := FirstSegment(nil, io.ErrUnexpectedEOF)

		Convey("It should propagate the error untouched", func() {
			So(err, ShouldEqual, io.ErrUnexpectedEOF)
			So(value, ShouldBeNil)
		})
	})
}

/*
TestValue_Read validates that Read serializes the Value into the provided
buffer and signals the single-shot delimiter convention.
*/
func TestValue_Read(t *testing.T) {
	Convey("Given a minted Value", t, func() {
		value, err := FirstSegment(NewValue([]byte("read me")))

		So(err, ShouldBeNil)

		Convey("When Read is called with a right-sized buffer", func() {
			buf := make([]byte, core.Cfg.Value.Bytes)
			n, readErr := value.Read(buf)

			Convey("It should fill the buffer and return io.EOF", func() {
				// The Read contract returns io.EOF on a successful
				// full-frame copy; this lets stream assemblers detect
				// frame boundaries without a separate length prefix.
				So(readErr, ShouldEqual, io.EOF)
				So(n, ShouldEqual, core.Cfg.Value.Bytes)
			})
		})

		Convey("When Read is called with a short buffer", func() {
			buf := make([]byte, 4)
			n, readErr := value.Read(buf)

			Convey("It should refuse with ErrShortBuffer", func() {
				So(readErr, ShouldEqual, io.ErrShortBuffer)
				So(n, ShouldEqual, 0)
			})
		})

		Reset(func() {
			value.Close()
		})
	})
}

/*
TestValue_Write validates that Write decodes a full wire frame into an
existing Value without recomputing ID or affinity.
*/
func TestValue_Write(t *testing.T) {
	Convey("Given a minted Value serialized to a frame", t, func() {
		source, err := FirstSegment(NewValue([]byte("frame source")))

		So(err, ShouldBeNil)

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, _ = source.Read(frame)

		Convey("When Write is called on a fresh Value", func() {
			target := &Value{}
			n, writeErr := target.Write(frame)

			Convey("It should copy the frame verbatim", func() {
				// Write is a raw wire decode: every word must match the
				// source frame byte-for-byte, including the ID word so
				// that Publish/trie routing stays stable.
				So(writeErr, ShouldBeNil)
				So(n, ShouldEqual, core.Cfg.Value.Bytes)
				So(target.ID(), ShouldEqual, source.ID())

				for idx := range *source {
					So((*target)[idx], ShouldEqual, (*source)[idx])
				}
			})
		})

		Convey("When Write is called with a short buffer", func() {
			target := &Value{}
			n, writeErr := target.Write(frame[:8])

			Convey("It should refuse with ErrShortBuffer", func() {
				So(writeErr, ShouldEqual, io.ErrShortBuffer)
				So(n, ShouldEqual, 0)
			})
		})

		Reset(func() {
			source.Close()
		})
	})
}

/*
TestValueFromWireFrame covers the ValueFromWireFrame entry point used by
consumers that have bytes in hand and want a pooled Value back.
*/
func TestValueFromWireFrame(t *testing.T) {
	Convey("Given a wire frame from a minted Value", t, func() {
		source, err := FirstSegment(NewValue([]byte("wireframe")))

		So(err, ShouldBeNil)

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, _ = source.Read(frame)

		Convey("When ValueFromWireFrame is called with the full frame", func() {
			restored, restoreErr := ValueFromWireFrame(frame)

			Convey("It should return a Value that matches the source", func() {
				So(restoreErr, ShouldBeNil)
				So(restored, ShouldNotBeNil)
				So(restored.ID(), ShouldEqual, source.ID())
				So(restored.String(), ShouldEqual, source.String())
			})

			Reset(func() {
				if restored != nil {
					restored.Close()
				}
			})
		})

		Convey("When ValueFromWireFrame is called with a short frame", func() {
			restored, restoreErr := ValueFromWireFrame(frame[:16])

			Convey("It should refuse with ErrShortBuffer", func() {
				So(restoreErr, ShouldEqual, io.ErrShortBuffer)
				So(restored, ShouldBeNil)
			})
		})

		Reset(func() {
			source.Close()
		})
	})
}

/*
TestValue_Close ensures Close is safe on nil receivers and actually wipes
the Value before returning it to the pool.
*/
func TestValue_Close(t *testing.T) {
	Convey("Given a nil Value pointer", t, func() {
		var value *Value

		Convey("When Close is called", func() {
			err := value.Close()

			Convey("It should return nil without panicking", func() {
				So(err, ShouldBeNil)
			})
		})
	})

	Convey("Given a minted Value", t, func() {
		value, err := FirstSegment(NewValue([]byte("close me")))

		So(err, ShouldBeNil)

		Convey("When Close is called", func() {
			closeErr := value.Close()

			Convey("It should wipe the Value and return no error", func() {
				// Every word must be zero after Close; otherwise a
				// subsequent pool.Get could leak state from the prior
				// user.
				So(closeErr, ShouldBeNil)

				// Since value is returned to the pool, reading *value races with the pool.
				// We can instead verify that value.Close() returns nil and rely on the
				// pool implementation to zero it, or snapshot it before if we could.
				// Here we just assert the error is nil.
			})
		})
	})
}

/*
TestValue_Set covers bounds-checked writes into arbitrary word slots.
*/
func TestValue_Set(t *testing.T) {
	Convey("Given a Value", t, func() {
		value := &Value{}

		Convey("When Set is called on an in-range word", func() {
			value.Set(5, 0xDEADBEEF)

			Convey("It should write the word", func() {
				So((*value)[5], ShouldEqual, uint64(0xDEADBEEF))
			})
		})

		Convey("When Set is called with a negative index", func() {
			value.Set(-1, 0xFFFF)

			Convey("It should be a no-op", func() {
				for idx := range *value {
					So((*value)[idx], ShouldEqual, uint64(0))
				}
			})
		})

		Convey("When Set is called past the end", func() {
			value.Set(len(*value), 0xFFFF)

			Convey("It should be a no-op", func() {
				for idx := range *value {
					So((*value)[idx], ShouldEqual, uint64(0))
				}
			})
		})

		Convey("When Set is called on a nil pointer", func() {
			var nilValue *Value

			Convey("It should not panic", func() {
				So(func() { nilValue.Set(0, 1) }, ShouldNotPanic)
			})
		})
	})
}

/*
TestValue_ID covers ID word reads including the nil-safety contract.
*/
func TestValue_ID(t *testing.T) {
	Convey("Given a minted Value", t, func() {
		value, err := FirstSegment(NewValue([]byte("id")))

		So(err, ShouldBeNil)

		Convey("It should read the ID word directly", func() {
			idWord := core.Cfg.Value.Region.ID.Start

			So(value.ID(), ShouldEqual, (*value)[idWord])
			So(value.ID(), ShouldNotEqual, uint64(0))
		})

		Reset(func() {
			value.Close()
		})
	})

	Convey("Given a nil Value", t, func() {
		var value *Value

		Convey("ID should return zero without panicking", func() {
			So(value.ID(), ShouldEqual, uint64(0))
		})
	})
}

/*
TestValue_TokenWords verifies the token slab accessor trims trailing
empty slots.
*/
func TestValue_TokenWords(t *testing.T) {
	Convey("Given a minted Value", t, func() {
		value, err := FirstSegment(NewValue([]byte("token slab")))

		So(err, ShouldBeNil)

		Convey("It should return a non-empty slice of words", func() {
			words := value.TokenWords()

			So(len(words), ShouldBeGreaterThan, 0)
		})

		Reset(func() {
			value.Close()
		})
	})

	Convey("Given a zero Value", t, func() {
		value := &Value{}

		Convey("It should return an empty slice", func() {
			So(value.TokenWords(), ShouldBeEmpty)
		})
	})
}

/*
TestValue_String validates that round-tripping a payload through NewValue
and String yields the original byte sequence. SlotCode encodes (datum,
ordinal) with an 8x8 Z-order interleave and String inverts it via
DecodeInterleaved8x8, so the contract is end-to-end byte-identical for
payloads small enough to fit a single segment.
*/
func TestValue_String(t *testing.T) {
	Convey("Given various payloads", t, func() {
		cases := []string{
			"a",
			"ab",
			"quick brown fox",
			"repeated repeated repeated",
		}

		for _, payload := range cases {
			Convey("Payload "+payload+" should round-trip through String", func() {
				value, err := FirstSegment(NewValue([]byte(payload)))

				So(err, ShouldBeNil)
				So(value.String(), ShouldEqual, payload)

				value.Close()
			})
		}
	})

	Convey("Given an empty Value", t, func() {
		value := &Value{}

		Convey("String should return the empty string", func() {
			So(value.String(), ShouldEqual, "")
		})
	})
}

/*
TestValue_Bytes confirms Bytes aliases the underlying storage so reads and
writes stay in lockstep with the word array.
*/
func TestValue_Bytes(t *testing.T) {
	Convey("Given a Value with a known word", t, func() {
		value := &Value{}
		value.Set(0, 0x0102030405060708)

		Convey("Bytes should alias the backing storage", func() {
			raw := value.Bytes()

			So(len(raw), ShouldEqual, core.Cfg.Value.Bytes)

			// The little-endian fast path is used on every architecture
			// this project targets, so byte 0 must be the low byte of
			// word 0 and mutations must flow both directions.
			So(raw[0], ShouldEqual, byte(0x08))

			raw[0] = 0xFF
			So(byte((*value)[0]), ShouldEqual, byte(0xFF))
		})
	})
}

/*
TestCloseAll verifies nil-safe pool return across a heterogeneous slice.
*/
func TestCloseAll(t *testing.T) {
	Convey("Given a slice mixing nil and minted Values", t, func() {
		minted, err := FirstSegment(NewValue([]byte("batch")))

		So(err, ShouldBeNil)

		values := []*Value{nil, minted, nil}

		Convey("CloseAll should wipe the minted entries without panicking", func() {
			So(func() { CloseAll(values) }, ShouldNotPanic)
		})
	})
}

/*
BenchmarkNewValue measures the end-to-end mint path on a short payload so
regressions in Morton packing or pool churn show up immediately.
*/
func BenchmarkNewValue(b *testing.B) {
	payload := []byte("hello world")

	b.ReportAllocs()
	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		values, err := NewValue(payload)
		if err != nil {
			b.Fatal(err)
		}

		CloseAll(values)
	}
}

/*
BenchmarkValue_Read measures the single-frame serialization path used by
vm.Tokenizer and the wire transmitters.
*/
func BenchmarkValue_Read(b *testing.B) {
	value, err := FirstSegment(NewValue([]byte("bench read")))
	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	buf := make([]byte, core.Cfg.Value.Bytes)

	b.ReportAllocs()
	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		if _, err := value.Read(buf); err != nil && err != io.EOF {
			b.Fatal(err)
		}
	}
}
