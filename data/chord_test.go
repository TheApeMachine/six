package data

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSanitize(t *testing.T) {
	Convey("Given a chord with polluted high bits", t, func() {
		var chord Chord
		chord.SetC4(0xFFFFFFFFFFFFFFFF)
		chord.SetC5(0xFFFFFFFFFFFFFFFF)
		chord.SetC6(0xFFFFFFFFFFFFFFFF)
		chord.SetC7(0xFFFFFFFFFFFFFFFF)

		Convey("When Sanitize is called", func() {
			chord.Sanitize()

			Convey("It should zero bits above 256 except delimiter face", func() {
				So(chord.C4(), ShouldEqual, uint64(1))
				So(chord.C5(), ShouldEqual, uint64(0))
				So(chord.C6(), ShouldEqual, uint64(0))
				So(chord.C7(), ShouldEqual, uint64(0))
			})
		})
	})

	Convey("Given a chord with low bits set", t, func() {
		var chord Chord
		chord.SetC0(0xDEADBEEF)
		chord.SetC1(0xCAFEBABE)
		chord.SetC2(0x12345678)
		chord.SetC3(0xABCDEF01)
		chord.SetC4(0x03)

		Convey("When Sanitize is called", func() {
			chord.Sanitize()

			Convey("It should preserve low bits and only keep bit 256 in C4", func() {
				So(chord.C0(), ShouldEqual, uint64(0xDEADBEEF))
				So(chord.C1(), ShouldEqual, uint64(0xCAFEBABE))
				So(chord.C2(), ShouldEqual, uint64(0x12345678))
				So(chord.C3(), ShouldEqual, uint64(0xABCDEF01))
				So(chord.C4(), ShouldEqual, uint64(1))
			})
		})
	})
}

func TestChordOR(t *testing.T) {
	Convey("Given two chords with dirty high bits", t, func() {
		var a, b Chord
		a.SetC0(0xFF)
		a.SetC5(0x01)
		b.SetC0(0xFF00)
		b.SetC6(0x01)

		Convey("When ChordOR is called", func() {
			result := a.OR(b)

			Convey("It should OR low bits and sanitize high bits", func() {
				So(result.C0(), ShouldEqual, uint64(0xFFFF))
				So(result.C5(), ShouldEqual, uint64(0))
				So(result.C6(), ShouldEqual, uint64(0))
			})
		})
	})
}

func TestBaseChord(t *testing.T) {
	const logicalBits = 257

	Convey("Given BaseChord for each byte 0-255", t, func() {
		Convey("It should keep all bits within logical width", func() {
			for byteVal := range 256 {
				chord := BaseChord(byte(byteVal))

				for idx := logicalBits; idx < 512; idx++ {
					word := idx / 64
					bit := idx % 64
					So(chord.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
				}

				So(chord.ActiveCount(), ShouldBeGreaterThan, 0)
			}
		})

		Convey("It should produce unique chords per byte", func() {
			chords := make(map[Chord]byte)
			for byteVal := 0; byteVal < 256; byteVal++ {
				chord := BaseChord(byte(byteVal))
				_, exists := chords[chord]
				So(exists, ShouldBeFalse)
				chords[chord] = byte(byteVal)
			}
		})
	})
}

func TestRollLeft(t *testing.T) {
	const logicalBits = 257

	Convey("Given a base chord and RollLeft(42)", t, func() {
		chord := BaseChord('A')
		rolled := chord.RollLeft(42)

		Convey("It should stay within logical width", func() {
			for idx := logicalBits; idx < 512; idx++ {
				word := idx / 64
				bit := idx % 64
				So(rolled.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
			}
		})

		Convey("It should preserve active count", func() {
			So(rolled.ActiveCount(), ShouldEqual, chord.ActiveCount())
		})
	})
}

func TestRotationSeed(t *testing.T) {
	Convey("Given two chords with same density but different structure", t, func() {
		var left Chord
		left.Set(3)
		left.Set(17)
		left.Set(41)

		var right Chord
		right.Set(5)
		right.Set(19)
		right.Set(43)

		Convey("It should use structure not density only for seed", func() {
			So(left.ActiveCount(), ShouldEqual, right.ActiveCount())

			aLeft, bLeft := left.RotationSeed()
			aRight, bRight := right.RotationSeed()

			So([2]uint16{aLeft, bLeft}, ShouldNotEqual, [2]uint16{aRight, bRight})
		})
	})
}

func TestMaskChord(t *testing.T) {
	Convey("Given MaskChord", t, func() {
		mask := MaskChord()

		Convey("It should use the control face", func() {
			So(mask.ActiveCount(), ShouldEqual, 1)
			So(mask.Has(256), ShouldBeTrue)
		})
	})
}

func BenchmarkChordRotationSeed(b *testing.B) {
	chord := BaseChord('x')

	for b.Loop() {
		_, _ = chord.RotationSeed()
	}
}
