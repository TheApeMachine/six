package tokenizer

import (
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
)

func TestEncode(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("When Encoding exhaustively over symbols and depth", func() {
			positions := []uint32{0, 1, 123456, 0x80000000, 0xFFFFFFFF}

			for _, pos := range positions {
				for z := range 256 {
					for symbol := range 256 {
						expect := uint64(z)<<56 | uint64(symbol)<<32 | uint64(pos)
						tokenID := coder.Encode(uint8(z), pos, byte(symbol))

						So(tokenID, ShouldEqual, expect)
					}
				}
			}
		})
	})
}

func TestDecode(t *testing.T) {
	Convey("Given a MortonCoder", t, func() {
		coder := NewMortonCoder()

		Convey("When Decoding exhaustively over symbols and depth", func() {
			positions := []uint32{0, 1, 123456, 0x80000000, 0xFFFFFFFF}

			for _, pos := range positions {
				for z := range 256 {
					for symbol := range 256 {
						tokenID := coder.Encode(uint8(z), pos, byte(symbol))
						decZ, decPos, decSymbol := coder.Decode(tokenID)

						So(decZ, ShouldEqual, uint8(z))
						So(decPos, ShouldEqual, pos)
						So(decSymbol, ShouldEqual, byte(symbol))
					}
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

			So(len(buf), ShouldEqual, numeric.ChordBlocks*8)
			for _, b := range buf {
				So(b, ShouldEqual, 0)
			}
		})

		Convey("When converting a populated chord to bytes", func() {
			chord := data.Chord{}

			chord[0] = 0x1122334455667788
			chord[numeric.ChordBlocks-1] = 0x8877665544332211

			buf := coder.ChordToBytes(chord)

			So(len(buf), ShouldEqual, numeric.ChordBlocks*8)

			val0 := binary.BigEndian.Uint64(buf[0:8])
			valLast := binary.BigEndian.Uint64(buf[(numeric.ChordBlocks-1)*8 : numeric.ChordBlocks*8])

			So(val0, ShouldEqual, uint64(0x1122334455667788))
			So(valLast, ShouldEqual, uint64(0x8877665544332211))
		})
	})
}
