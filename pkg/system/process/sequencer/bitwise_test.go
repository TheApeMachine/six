package sequencer

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

var cases = []struct {
	name   string
	broken [][]string
	healed [][]string
}{
	{
		name: "Roy cluster shares a broken anchor",
		broken: [][]string{
			{"Roy was ", "in t", "he liv", "ing ", "ro", "om"},
			{"Roy is ", "i", "n the k", "itche", "n"},
			{"Roy will be ", "in", " t", "he", " g", "ara", "ge"},
		},
		healed: [][]string{
			{"Roy was in the living room"},
			{"Roy is in the kitchen"},
			{"Roy will be in the garage"},
		},
	},
	{
		name: "Image cluster shares a broken anchor",
		broken: [][]string{
			{"Image of ", "ca", "t ", "is a ", "ca", "t"},
			{"Image of ", "do", "g ", "is a ", "do", "g"},
		},
		healed: [][]string{
			{"Image of ", "cat is a ", "cat"},
			{"Image of ", "dog is a ", "dog"},
		},
	},
}

func TestBitwiseHealerHeal(t *testing.T) {
	for _, tc := range cases {
		gc.Convey("Given "+tc.name, t, func() {
			bitwise := NewBitwiseHealer()

			for _, sequence := range tc.broken {
				for _, fragment := range sequence {
					for idx, b := range []byte(fragment) {
						bitwise.Write(b, idx == len(fragment)-1)
					}
				}
			}

			healed := bitwise.Heal()

			var expected []string

			for _, seq := range tc.healed {
				expected = append(expected, seq...)
			}

			for idx, chunk := range healed {
				t.Logf("healed[%d] = %q", idx, string(chunk))
			}

			gc.So(len(healed), gc.ShouldEqual, len(expected))

			for idx, chunk := range healed {
				gc.So(string(chunk), gc.ShouldEqual, expected[idx])
			}
		})
	}
}

func TestBitwiseHealerSingleSequence(t *testing.T) {
	gc.Convey("Given a single unfragmented sentence", t, func() {
		bitwise := NewBitwiseHealer()

		for _, fragment := range []string{"Hello world"} {
			for idx, b := range []byte(fragment) {
				bitwise.Write(b, idx == len(fragment)-1)
			}
		}

		healed := bitwise.Heal()

		gc.So(len(healed), gc.ShouldEqual, 1)
		gc.So(string(healed[0]), gc.ShouldEqual, "Hello world")
	})
}

func TestBitwiseHealerEmpty(t *testing.T) {
	gc.Convey("Given no input", t, func() {
		bitwise := NewBitwiseHealer()
		healed := bitwise.Heal()

		gc.So(healed, gc.ShouldBeNil)
	})
}

func TestBitwiseHealerSingleByte(t *testing.T) {
	gc.Convey("Given a single byte", t, func() {
		bitwise := NewBitwiseHealer()
		bitwise.Write('A', true)
		healed := bitwise.Heal()

		gc.So(len(healed), gc.ShouldEqual, 1)
		gc.So(string(healed[0]), gc.ShouldEqual, "A")
	})
}

func TestBitwiseHealerCleanFragments(t *testing.T) {
	gc.Convey("Given already-clean word-boundary fragments", t, func() {
		bitwise := NewBitwiseHealer()

		for _, word := range []string{"The ", "quick ", "brown ", "fox"} {
			for idx, b := range []byte(word) {
				bitwise.Write(b, idx == len(word)-1)
			}
		}

		healed := bitwise.Heal()

		full := ""
		for _, chunk := range healed {
			full += string(chunk)
		}

		gc.So(full, gc.ShouldEqual, "The quick brown fox")
	})
}

func TestBitwiseHealerFlush(t *testing.T) {
	gc.Convey("Given bytes without a trailing boundary", t, func() {
		bitwise := NewBitwiseHealer()

		for _, b := range []byte("trailing") {
			bitwise.Write(b, false)
		}

		healed := bitwise.Flush()

		gc.So(len(healed), gc.ShouldEqual, 1)
		gc.So(string(healed[0]), gc.ShouldEqual, "trailing")
	})
}

func TestBitwiseHealerDecodeLookup(t *testing.T) {
	gc.Convey("Given every possible byte value", t, func() {
		bitwise := NewBitwiseHealer()

		for symbol := range 256 {
			bitwise.Write(byte(symbol), true)
		}

		healed := bitwise.Heal()
		flat := []byte{}

		for _, chunk := range healed {
			flat = append(flat, chunk...)
		}

		gc.So(len(flat), gc.ShouldEqual, 256)

		for symbol := range 256 {
			gc.So(flat[symbol], gc.ShouldEqual, byte(symbol))
		}
	})
}

func BenchmarkBitwiseHealerHeal(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		bitwise := NewBitwiseHealer()

		for _, sequence := range cases[0].broken {
			for _, fragment := range sequence {
				for idx, bv := range []byte(fragment) {
					bitwise.Write(bv, idx == len(fragment)-1)
				}
			}
		}

		_ = bitwise.Heal()
	}
}

func BenchmarkBitwiseHealerLargePayload(b *testing.B) {
	fragments := []string{
		"The quick brown ", "fo", "x jumps over the ", "la", "zy dog. ",
		"The quick brown ", "fo", "x runs past the ", "la", "zy cat. ",
		"The quick brown ", "fo", "x leaps over the ", "la", "zy hen. ",
	}

	b.ReportAllocs()

	for b.Loop() {
		bitwise := NewBitwiseHealer()

		for _, fragment := range fragments {
			for idx, bv := range []byte(fragment) {
				bitwise.Write(bv, idx == len(fragment)-1)
			}
		}

		_ = bitwise.Heal()
	}
}

func BenchmarkBitwiseHealerDecode(b *testing.B) {
	bitwise := NewBitwiseHealer()

	for symbol := range 256 {
		bitwise.Write(byte(symbol), true)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = bitwise.decode(bitwise.flatten(bitwise.values))
	}
}
