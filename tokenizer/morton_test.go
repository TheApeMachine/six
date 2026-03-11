package tokenizer

import (
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

func TestEncode(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("When Encoding exhaustively over symbols and positions", func() {
			positions := []uint32{0, 1, 123456, 0x80000000, 0xFFFFFFFF}

			for _, pos := range positions {
				for symbol := range 256 {
					expect := uint64(symbol)<<32 | uint64(pos)
					tokenID := coder.Encode(pos, byte(symbol))

					So(tokenID, ShouldEqual, expect)
				}
			}
		})
	})
}

func TestDecode(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("When Decoding exhaustively over symbols and positions", func() {
			positions := []uint32{0, 1, 123456, 0x80000000, 0xFFFFFFFF}

			for _, pos := range positions {
				for symbol := range 256 {
					tokenID := coder.Encode(pos, byte(symbol))
					decPos, decSymbol := coder.Decode(tokenID)

					So(decPos, ShouldEqual, pos)
					So(decSymbol, ShouldEqual, byte(symbol))
				}
			}
		})
	})
}

func TestChordToBytes(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("When converting a zeroed chord to bytes", func() {
			chord := data.Chord{}
			buf := coder.ChordToBytes(chord)

			So(len(buf), ShouldEqual, config.Numeric.ChordBlocks*8)
			for _, b := range buf {
				So(b, ShouldEqual, 0)
			}
		})

		Convey("When converting a populated chord to bytes", func() {
			buf := make([]byte, config.Numeric.ChordBlocks*8)
			binary.BigEndian.PutUint64(buf[0:8], 0x1122334455667788)
			binary.BigEndian.PutUint64(buf[(config.Numeric.ChordBlocks-1)*8:config.Numeric.ChordBlocks*8], 0x8877665544332211)
			chord := data.ChordFromBytes(buf)

			buf = coder.ChordToBytes(chord)

			So(len(buf), ShouldEqual, config.Numeric.ChordBlocks*8)

			val0 := binary.BigEndian.Uint64(buf[0:8])
			valLast := binary.BigEndian.Uint64(buf[(config.Numeric.ChordBlocks-1)*8 : config.Numeric.ChordBlocks*8])

			So(val0, ShouldEqual, uint64(0x1122334455667788))
			So(valLast, ShouldEqual, uint64(0x8877665544332211))
		})
	})
}

func TestMortonCoder4D_Roundtrip(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("It should roundtrip all four dimensions", func() {
			cases := []struct {
				sample   uint16
				sequence uint8
				pos      uint16
				symbol   byte
			}{
				{0, 0, 0, 0},
				{0, 0, 0, 'H'},
				{0, 0, 1, 'e'},
				{0, 0, 5, ' '},
				{0, 1, 0, 'd'},
				{1, 0, 0, '#'},
				{1, 2, 100, 'x'},
				{65535, 255, 65535, 255},
				{42, 3, 500, 'A'},
			}

			for _, tc := range cases {
				key := coder.Encode4D(tc.sample, tc.sequence, tc.pos, tc.symbol)
				gotSample, gotSeq, gotPos, gotSym := coder.Decode4D(key)

				So(gotSample, ShouldEqual, tc.sample)
				So(gotSeq, ShouldEqual, tc.sequence)
				So(gotPos, ShouldEqual, tc.pos)
				So(gotSym, ShouldEqual, tc.symbol)
			}
		})

		Convey("It should produce strictly increasing keys across sequence/position/symbol", func() {
			var prev uint64
			first := true

			for seq := range uint8(3) {
				for pos := range uint16(10) {
					for sym := range byte(5) {
						key := coder.Encode4D(0, seq, pos, sym)

						if !first {
							So(key, ShouldBeGreaterThan, prev)
						}

						prev = key
						first = false
					}
				}
			}
		})

		Convey("It should keep all entries for a sample in a contiguous range", func() {
			lo, hi := coder.SampleRange(5)
			keyInside := coder.Encode4D(5, 3, 100, 'x')
			keyBefore := coder.Encode4D(4, 255, 65535, 255)
			keyAfter := coder.Encode4D(6, 0, 0, 0)

			So(keyInside, ShouldBeGreaterThanOrEqualTo, lo)
			So(keyInside, ShouldBeLessThanOrEqualTo, hi)
			So(keyBefore, ShouldBeLessThan, lo)
			So(keyAfter, ShouldBeGreaterThan, hi)
		})

		Convey("It should keep all entries for a sample+sequence in a contiguous range", func() {
			lo, hi := coder.SampleSequenceRange(5, 2)
			keyInside := coder.Encode4D(5, 2, 50, 'a')
			keyBefore := coder.Encode4D(5, 1, 65535, 255)
			keyAfter := coder.Encode4D(5, 3, 0, 0)

			So(keyInside, ShouldBeGreaterThanOrEqualTo, lo)
			So(keyInside, ShouldBeLessThanOrEqualTo, hi)
			So(keyBefore, ShouldBeLessThan, lo)
			So(keyAfter, ShouldBeGreaterThan, hi)
		})

		Convey("It should produce a position range that contains all symbols at that position", func() {
			lo, hi := coder.SampleSequencePosRange(5, 2, 10)

			for sym := range 256 {
				key := coder.Encode4D(5, 2, 10, byte(sym))
				So(key, ShouldBeGreaterThanOrEqualTo, lo)
				So(key, ShouldBeLessThanOrEqualTo, hi)
			}

			keyOutside := coder.Encode4D(5, 2, 11, 0)
			So(keyOutside, ShouldBeGreaterThan, hi)
		})
	})
}

func BenchmarkMortonCoder4D_Encode(b *testing.B) {
	coder := NewMortonCoder()

	for i := 0; i < b.N; i++ {
		coder.Encode4D(uint16(i%65536), uint8(i%256), uint16(i%1000), byte(i%256))
	}
}

func BenchmarkMortonCoder4D_Decode(b *testing.B) {
	coder := NewMortonCoder()
	key := coder.Encode4D(42, 3, 500, 'A')

	for i := 0; i < b.N; i++ {
		coder.Decode4D(key)
	}
}

