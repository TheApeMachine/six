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
