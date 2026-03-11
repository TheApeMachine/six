package cortex

import (
	"crypto/rand"
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func randomChord() data.Chord {
	c := data.Chord{}
	for i := 0; i < 5; i++ {
		var r uint64
		// just to easily generate 64 bits of noise
		b := make([]byte, 8)
		rand.Read(b)
		r = binary.BigEndian.Uint64(b)
		c[i] = r
	}
	// clear out anything above 257 bits
	c[4] = c[4] & 1 
	return c
}

func TestPathMatrix(t *testing.T) {
	Convey("Given a PathMatrix", t, func() {
		pm := NewPathMatrix(10)

		Convey("When empty", func() {
			idx, res := pm.Evaluate(data.Chord{})
			So(idx, ShouldEqual, -1)
			So(res, ShouldEqual, -1)
		})

		Convey("When evaluating single perfect match", func() {
			c := randomChord()
			pm.Insert(c)

			idx, res := pm.Evaluate(c)
			So(idx, ShouldEqual, 0)
			So(res, ShouldEqual, 0) // Exact XOR cancellation leaves 0 residue
		})

		Convey("When evaluating among multiple paths", func() {
			c1 := randomChord()
			c2 := randomChord()
			c3 := randomChord()

			pm.Insert(c1)
			pm.Insert(c2)
			pm.Insert(c3)

			// Should perfectly match idx 1
			idx, res := pm.Evaluate(c2)
			So(idx, ShouldEqual, 1)
			So(res, ShouldEqual, 0)

			// Test partial cancellation
			// We build c4 that is almost c2, but flipped 1 bit
			c4 := c2
			c4[0] ^= 1 
			idxPartial, resPartial := pm.Evaluate(c4)
			So(idxPartial, ShouldEqual, 1)
			So(resPartial, ShouldEqual, 1) // 1 bit residue
		})
	})
}

// Benchmark the Evaluate loop (the brute-force Hardware POPCNT sweep)
func BenchmarkPathMatrixEvaluate_1Million(b *testing.B) {
	// Create a matrix of 1 million random paths
	size := 1_000_000
	pm := NewPathMatrix(size)

	for i := 0; i < size; i++ {
		pm.paths = append(pm.paths, randomChord())
	}

	target := pm.paths[500_000] // Pick one in the middle

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Evaluates 1 million paths per benchmark loop
		pm.Evaluate(target)
	}
}
