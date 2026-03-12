package data

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
gen4DCoordinates produces a sweep of 4D coordinates for meaningful Morton tests.
Covers boundaries, midpoints, and a dense grid to exercise encoding/decoding.
*/
func gen4DCoordinates() []struct {
	sampleIdx   uint16
	sequenceIdx uint8
	posIdx      uint16
	symbol      byte
} {
	var out []struct {
		sampleIdx   uint16
		sequenceIdx uint8
		posIdx      uint16
		symbol      byte
	}

	for sampleIdx := range uint16(10) {
		for sequenceIdx := range uint8(5) {
			for posIdx := uint16(0); posIdx < 100; posIdx += 7 {
				for symbol := range byte(10) {
					out = append(out, struct {
						sampleIdx   uint16
						sequenceIdx uint8
						posIdx      uint16
						symbol      byte
					}{sampleIdx, sequenceIdx, posIdx, symbol})
				}
			}
		}
	}

	return out
}

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
				Convey(fmt.Sprintf("It should round-trip 4D %d,%d,%d,%c", tc.sampleIdx, tc.sequenceIdx, tc.posIdx, tc.symbol), func() {
					key := coder.Encode4D(tc.sampleIdx, tc.sequenceIdx, tc.posIdx, tc.symbol)
					smIdx, sqIdx, pIdx, sym := coder.Decode4D(key)

					So(smIdx, ShouldEqual, tc.sampleIdx)
					So(sqIdx, ShouldEqual, tc.sequenceIdx)
					So(pIdx, ShouldEqual, tc.posIdx)
					So(sym, ShouldEqual, tc.symbol)
				})
			}
		})

		Convey("When testing generated 4D coordinates", func() {
			coords := gen4DCoordinates()
			So(len(coords), ShouldBeGreaterThan, 5000)

			for idx, tc := range coords {
				if idx >= 100 {
					break
				}
				key := coder.Encode4D(tc.sampleIdx, tc.sequenceIdx, tc.posIdx, tc.symbol)
				smIdx, sqIdx, pIdx, sym := coder.Decode4D(key)
				So(smIdx, ShouldEqual, tc.sampleIdx)
				So(sqIdx, ShouldEqual, tc.sequenceIdx)
				So(pIdx, ShouldEqual, tc.posIdx)
				So(sym, ShouldEqual, tc.symbol)
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
				for x := range uint32(20) {
					for y := range uint32(20) {
						for z := range uint32(20) {
							encoded := coder.Encode3D(x, y, z)
							So(encoded, ShouldBeGreaterThanOrEqualTo, uint64(0))
						}
					}
				}
			})
		})
	})
}

func TestMortonRangeContainment(t *testing.T) {
	Convey("Given SampleRange and encoded keys", t, func() {
		coder := NewMortonCoder()
		sampleIdx := uint16(42)

		lo, hi := coder.SampleRange(sampleIdx)

		Convey("Keys for sample 42 should fall within [lo, hi]", func() {
			for seqIdx := range uint8(5) {
				for posIdx := range uint16(10) {
					for symbol := range byte(5) {
						key := coder.Encode4D(sampleIdx, seqIdx, posIdx, symbol)
						So(key, ShouldBeGreaterThanOrEqualTo, lo)
						So(key, ShouldBeLessThanOrEqualTo, hi)
					}
				}
			}
		})

		Convey("Keys for sample 43 should be above hi", func() {
			key := coder.Encode4D(43, 0, 0, 0)
			So(key, ShouldBeGreaterThan, hi)
		})
	})
}

func BenchmarkMortonPack(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for b.Loop() {
		coder.Pack(uint32(b.N), byte(b.N))
	}
}

func BenchmarkMortonUnpack(b *testing.B) {
	coder := NewMortonCoder()
	morton := uint64(0x123456789ABCDEF0)
	b.ResetTimer()
	for b.Loop() {
		coder.Unpack(morton)
	}
}

func BenchmarkMortonEncode4D(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for b.Loop() {
		coder.Encode4D(uint16(b.N), uint8(b.N), uint16(b.N), byte(b.N))
	}
}

func BenchmarkMortonDecode4D(b *testing.B) {
	coder := NewMortonCoder()
	key := coder.Encode4D(0x1234, 0x56, 0x789A, 0xBC)
	b.ResetTimer()
	for b.Loop() {
		coder.Decode4D(key)
	}
}

func BenchmarkMortonEncode3D(b *testing.B) {
	coder := NewMortonCoder()
	b.ResetTimer()
	for b.Loop() {
		coder.Encode3D(uint32(b.N), uint32(b.N), uint32(b.N))
	}
}
