package process

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMortonCoder(t *testing.T) {
	Convey("Given a new MortonCoder", t, func() {
		coder := NewMortonCoder()
		So(coder, ShouldNotBeNil)

		Convey("When testing Pack and Unpack", func() {
			cases := []struct {
				pos    uint32
				symbol byte
			}{
				{0, 0},
				{42, 'A'},
				{0xFFFFFFFF, 0xFF},
			}
			
			for _, tc := range cases {
				Convey(fmt.Sprintf("It should correctly round-trip values for pos=%d sym=%c", tc.pos, tc.symbol), func() {
					key := coder.Pack(tc.pos, tc.symbol)
					pos, symbol := coder.Unpack(key)
					So(pos, ShouldEqual, tc.pos)
					So(symbol, ShouldEqual, tc.symbol)
				})
			}
		})

		Convey("When testing Encode4D and Decode4D", func() {
			cases := []struct {
				sampleIdx   uint16
				sequenceIdx uint8
				posIdx      uint16
				symbol      byte
			}{
				{0, 0, 0, 0},
				{1, 2, 3, 'B'},
				{0xFFFF, 0xFF, 0xFFFF, 0xFF},
			}
			
			for _, tc := range cases {
				Convey(fmt.Sprintf("It should correctly round-trip 4D values for %d,%d,%d,%c", tc.sampleIdx, tc.sequenceIdx, tc.posIdx, tc.symbol), func() {
					key := coder.Encode4D(tc.sampleIdx, tc.sequenceIdx, tc.posIdx, tc.symbol)
					smIdx, sqIdx, pIdx, sym := coder.Decode4D(key)
					
					So(smIdx, ShouldEqual, tc.sampleIdx)
					So(sqIdx, ShouldEqual, tc.sequenceIdx)
					So(pIdx, ShouldEqual, tc.posIdx)
					So(sym, ShouldEqual, tc.symbol)
				})
			}
		})

		Convey("When testing sample ranges", func() {
			Convey("SampleRange should return the bounds for all entries of a sample", func() {
				lo, hi := coder.SampleRange(0x1234)
				So(lo, ShouldEqual, uint64(0x1234)<<32)
				So(hi, ShouldEqual, (uint64(0x1234)<<32)|0xFFFFFFFF)
			})

			Convey("SampleSequenceRange should return the bounds for a sequence within a sample", func() {
				lo, hi := coder.SampleSequenceRange(0x1234, 0x56)
				base := (uint64(0x1234) << 32) | (uint64(0x56) << 24)
				So(lo, ShouldEqual, base)
				So(hi, ShouldEqual, base|0x00FFFFFF)
			})

			Convey("SampleSequencePosRange should return bounds for a position in a sequence", func() {
				lo, hi := coder.SampleSequencePosRange(0x1234, 0x56, 0x789A)
				base := (uint64(0x1234) << 32) | (uint64(0x56) << 24) | (uint64(0x789A) << 8)
				So(lo, ShouldEqual, base)
				So(hi, ShouldEqual, base|0xFF)
			})
		})

		Convey("When testing Encode3D (Z-order curve interleaving)", func() {
			Convey("It should properly interleave 3D coordinates", func() {
				// For small coordinates, interleaving is easily verifiable
				// 0 = 0x0
				So(coder.Encode3D(0, 0, 0), ShouldEqual, 0)
				
				// 1,1,1 -> binary 001,001,001 interleaved: 111 (binary) = 7
				So(coder.Encode3D(1, 1, 1), ShouldEqual, 7)
				
				// 1,0,0 -> binary 001,000,000 interleaved: 001 (binary) = 1
				So(coder.Encode3D(1, 0, 0), ShouldEqual, 1)

				// Distinguish axis order (x, y, z => bit 0, 1, 2)
				So(coder.Encode3D(0, 1, 0), ShouldEqual, 2)
				So(coder.Encode3D(0, 0, 1), ShouldEqual, 4)
			})
		})
	})
}

func BenchmarkMortonPack(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coder.Pack(uint32(i), byte(i))
	}
}

func BenchmarkMortonUnpack(b *testing.B) {
	coder := NewMortonCoder()
	var morton uint64 = 0x123456789ABCDEF0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coder.Unpack(morton)
	}
}

func BenchmarkMortonEncode4D(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coder.Encode4D(uint16(i), uint8(i), uint16(i), byte(i))
	}
}

func BenchmarkMortonEncode3D(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coder.Encode3D(uint32(i), uint32(i), uint32(i))
	}
}
