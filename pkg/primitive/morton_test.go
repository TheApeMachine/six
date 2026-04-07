package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestEncode(t *testing.T) {
	Convey("Given a 2D Morton encoder", t, func() {
		morton := NewMorton(2)

		Convey("It should roundtrip the origin", func() {
			code := morton.Encode(0, 0)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, 0)
			So(coords[1], ShouldEqual, 0)
		})

		Convey("It should roundtrip small coordinates", func() {
			code := morton.Encode(3, 5)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, 3)
			So(coords[1], ShouldEqual, 5)
		})

		Convey("It should roundtrip large coordinates", func() {
			code := morton.Encode(0xFFFF, 0xFFFF)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, 0xFFFF)
			So(coords[1], ShouldEqual, 0xFFFF)
		})

		Convey("It should roundtrip the maximum representable value", func() {
			maxCoord := uint32((1 << morton.BitsPerCoord()) - 1)
			code := morton.Encode(maxCoord, maxCoord)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, maxCoord)
			So(coords[1], ShouldEqual, maxCoord)
		})

		Convey("It should preserve spatial locality", func() {
			origin := morton.Encode(10, 10)
			adjacent := morton.Encode(10, 11)
			diagonal := morton.Encode(11, 11)
			far := morton.Encode(200, 200)

			adjDist := absDiff(origin, adjacent)
			diagDist := absDiff(origin, diagonal)
			farDist := absDiff(origin, far)

			So(adjDist, ShouldBeLessThan, farDist)
			So(diagDist, ShouldBeLessThan, farDist)
		})

		Convey("It should produce distinct codes for distinct inputs", func() {
			a := morton.Encode(1, 0)
			b := morton.Encode(0, 1)

			So(a, ShouldNotEqual, b)
		})

		Convey("It should interleave bits correctly", func() {
			code := morton.Encode(0b101, 0b110)

			So(code&1, ShouldEqual, 1)
			So((code>>1)&1, ShouldEqual, 0)
			So((code>>2)&1, ShouldEqual, 0)
			So((code>>3)&1, ShouldEqual, 1)
			So((code>>4)&1, ShouldEqual, 1)
			So((code>>5)&1, ShouldEqual, 1)
		})
	})

	Convey("Given a 3D Morton encoder", t, func() {
		morton := NewMorton(3)

		Convey("It should roundtrip small coordinates", func() {
			code := morton.Encode(7, 13, 21)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, 7)
			So(coords[1], ShouldEqual, 13)
			So(coords[2], ShouldEqual, 21)
		})

		Convey("It should roundtrip the maximum representable value", func() {
			maxCoord := uint32((1 << morton.BitsPerCoord()) - 1)
			code := morton.Encode(maxCoord, maxCoord, maxCoord)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, maxCoord)
			So(coords[1], ShouldEqual, maxCoord)
			So(coords[2], ShouldEqual, maxCoord)
		})

		Convey("It should preserve spatial locality in 3D", func() {
			origin := morton.Encode(5, 5, 5)
			adjacent := morton.Encode(5, 5, 6)
			far := morton.Encode(100, 100, 100)

			So(absDiff(origin, adjacent), ShouldBeLessThan, absDiff(origin, far))
		})
	})

	Convey("Given a 4D Morton encoder", t, func() {
		morton := NewMorton(4)

		Convey("It should roundtrip coordinates", func() {
			code := morton.Encode(3, 7, 11, 15)
			coords := morton.Decode(code)

			So(coords[0], ShouldEqual, 3)
			So(coords[1], ShouldEqual, 7)
			So(coords[2], ShouldEqual, 11)
			So(coords[3], ShouldEqual, 15)
		})
	})
}

func TestDecode(t *testing.T) {
	Convey("Given a 2D Morton encoder", t, func() {
		morton := NewMorton(2)

		Convey("It should decode zero to origin", func() {
			coords := morton.Decode(0)

			So(coords[0], ShouldEqual, 0)
			So(coords[1], ShouldEqual, 0)
		})

		Convey("It should decode 1 to (1,0)", func() {
			coords := morton.Decode(1)

			So(coords[0], ShouldEqual, 1)
			So(coords[1], ShouldEqual, 0)
		})

		Convey("It should decode 2 to (0,1)", func() {
			coords := morton.Decode(2)

			So(coords[0], ShouldEqual, 0)
			So(coords[1], ShouldEqual, 1)
		})

		Convey("It should decode 3 to (1,1)", func() {
			coords := morton.Decode(3)

			So(coords[0], ShouldEqual, 1)
			So(coords[1], ShouldEqual, 1)
		})
	})
}

func TestNeighbourKeys(t *testing.T) {
	Convey("Given a 2D Morton encoder", t, func() {
		morton := NewMorton(2)

		Convey("It should return 8 neighbours for an interior point", func() {
			code := morton.Encode(5, 5)
			neighbours := morton.NeighbourKeys(code)

			So(len(neighbours), ShouldEqual, 8)
		})

		Convey("It should return 3 neighbours for the origin", func() {
			neighbours := morton.NeighbourKeys(morton.Encode(0, 0))

			So(len(neighbours), ShouldEqual, 3)
		})

		Convey("It should return 5 neighbours for an edge point", func() {
			neighbours := morton.NeighbourKeys(morton.Encode(0, 5))

			So(len(neighbours), ShouldEqual, 5)
		})

		Convey("It should include all adjacent coordinates", func() {
			code := morton.Encode(5, 5)
			neighbours := morton.NeighbourKeys(code)

			expected := map[uint64]struct{}{
				morton.Encode(4, 4): {},
				morton.Encode(4, 5): {},
				morton.Encode(4, 6): {},
				morton.Encode(5, 4): {},
				morton.Encode(5, 6): {},
				morton.Encode(6, 4): {},
				morton.Encode(6, 5): {},
				morton.Encode(6, 6): {},
			}

			for _, neighbour := range neighbours {
				_, found := expected[neighbour]
				So(found, ShouldBeTrue)
			}
		})
	})

	Convey("Given a 3D Morton encoder", t, func() {
		morton := NewMorton(3)

		Convey("It should return 26 neighbours for an interior point", func() {
			code := morton.Encode(5, 5, 5)
			neighbours := morton.NeighbourKeys(code)

			So(len(neighbours), ShouldEqual, 26)
		})
	})
}

func TestEncodeBytesWithDepth(t *testing.T) {
	Convey("Given a 2D Morton encoder", t, func() {
		morton := NewMorton(2)

		Convey("It should encode text with depth resetting at spaces", func() {
			codes := morton.EncodeBytesWithDepth(
				[]byte("ab cd"), []byte{' '},
			)

			So(len(codes), ShouldEqual, 5)

			coordsA := morton.Decode(codes[0])
			coordsB := morton.Decode(codes[1])
			coordsSpace := morton.Decode(codes[2])
			coordsC := morton.Decode(codes[3])
			coordsD := morton.Decode(codes[4])

			So(coordsA[0], ShouldEqual, 'a')
			So(coordsA[1], ShouldEqual, 0)

			So(coordsB[0], ShouldEqual, 'b')
			So(coordsB[1], ShouldEqual, 1)

			So(coordsSpace[0], ShouldEqual, ' ')
			So(coordsSpace[1], ShouldEqual, 2)

			So(coordsC[0], ShouldEqual, 'c')
			So(coordsC[1], ShouldEqual, 0)

			So(coordsD[0], ShouldEqual, 'd')
			So(coordsD[1], ShouldEqual, 1)
		})

		Convey("It should return nil for empty input", func() {
			codes := morton.EncodeBytesWithDepth(nil, nil)

			So(codes, ShouldBeNil)
		})
	})
}

func TestMortonMSB(t *testing.T) {
	Convey("Given morton codes", t, func() {
		Convey("It should return 0 for zero", func() {
			So(MortonMSB(0), ShouldEqual, 0)
		})

		Convey("It should return the correct MSB position", func() {
			So(MortonMSB(1), ShouldEqual, 0)
			So(MortonMSB(2), ShouldEqual, 1)
			So(MortonMSB(8), ShouldEqual, 3)
			So(MortonMSB(0xFF), ShouldEqual, 7)
		})
	})
}

func BenchmarkEncode2D(b *testing.B) {
	morton := NewMorton(2)

	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		morton.Encode(uint32(idx), uint32(idx>>16))
	}
}

func BenchmarkDecode2D(b *testing.B) {
	morton := NewMorton(2)

	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		morton.Decode(uint64(idx))
	}
}

func BenchmarkEncode3D(b *testing.B) {
	morton := NewMorton(3)

	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		morton.Encode(uint32(idx), uint32(idx>>8), uint32(idx>>16))
	}
}

func BenchmarkRoundtrip2D(b *testing.B) {
	morton := NewMorton(2)

	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		code := morton.Encode(uint32(idx), uint32(idx>>16))
		morton.Decode(code)
	}
}

func BenchmarkEncodeBytesWithDepth(b *testing.B) {
	morton := NewMorton(2)
	data := []byte("the quick brown fox jumps over the lazy dog")
	boundaries := []byte{' '}

	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		morton.EncodeBytesWithDepth(data, boundaries)
	}
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}

	return b - a
}
