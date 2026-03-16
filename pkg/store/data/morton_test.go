package data

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
				Convey(fmt.Sprintf("It should round-trip pos=%d sym=%c", tc.pos, tc.symbol), func() {
					key := coder.Pack(tc.pos, tc.symbol)
					pos, symbol := coder.Unpack(key)
					So(pos, ShouldEqual, tc.pos)
					So(symbol, ShouldEqual, tc.symbol)
				})
			}
		})

		Convey("When testing sample ranges", func() {
			Convey("SampleRange should return bounds for all entries of a sample", func() {
				lo, hi := coder.SampleRange(0x1234)
				So(lo, ShouldEqual, uint64(0x1234)<<32)
				So(hi, ShouldEqual, (uint64(0x1234)<<32)|0xFFFFFFFF)
			})

			Convey("SampleSequenceRange should return bounds for sequence within sample", func() {
				lo, hi := coder.SampleSequenceRange(0x1234, 0x56)
				base := (uint64(0x1234) << 32) | (uint64(0x56) << 24)
				So(lo, ShouldEqual, base)
				So(hi, ShouldEqual, base|0x00FFFFFF)
			})

			Convey("SampleSequencePosRange should return bounds for position in sequence", func() {
				lo, hi := coder.SampleSequencePosRange(0x1234, 0x56, 0x789A)
				base := (uint64(0x1234) << 32) | (uint64(0x56) << 24) | (uint64(0x789A) << 8)
				So(lo, ShouldEqual, base)
				So(hi, ShouldEqual, base|0xFF)
			})
		})

		Convey("When testing Encode3D (Z-order interleaving)", func() {
			Convey("It should interleave 3D coordinates", func() {
				So(coder.Encode3D(0, 0, 0), ShouldEqual, 0)
				So(coder.Encode3D(1, 1, 1), ShouldEqual, 7)
				So(coder.Encode3D(1, 0, 0), ShouldEqual, 1)
				So(coder.Encode3D(0, 1, 0), ShouldEqual, 2)
				So(coder.Encode3D(0, 0, 1), ShouldEqual, 4)
			})

			Convey("It should round-trip for small coordinates", func() {
				for x := uint32(0); x < 20; x++ {
					for y := uint32(0); y < 20; y++ {
						for z := uint32(0); z < 20; z++ {
							encoded := coder.Encode3D(x, y, z)
							decodedX, decodedY, decodedZ := coder.Decode3D(encoded)
							So(decodedX, ShouldEqual, x)
							So(decodedY, ShouldEqual, y)
							So(decodedZ, ShouldEqual, z)
						}
					}
				}
				
				encoded := coder.Encode3D(1, 2, 3)
				So(encoded, ShouldEqual, 53)
			})
		})
	})
}

func BenchmarkMortonPack(b *testing.B) {
	coder := NewMortonCoder()
	pos := uint32(0x12345678)
	sym := byte(0x42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coder.Pack(pos, sym)
	}
}

func BenchmarkMortonUnpack(b *testing.B) {
	coder := NewMortonCoder()
	morton := uint64(0x123456789ABCDEF0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coder.Unpack(morton)
	}
}

func BenchmarkMortonEncode3D(b *testing.B) {
	coder := NewMortonCoder()
	x, y, z := uint32(100), uint32(200), uint32(150)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coder.Encode3D(x, y, z)
	}
}

func BenchmarkMortonDecode3D(b *testing.B) {
	coder := NewMortonCoder()
	encoded := coder.Encode3D(100, 200, 150)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = coder.Decode3D(encoded)
	}
}


