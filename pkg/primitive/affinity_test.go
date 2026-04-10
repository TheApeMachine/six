package primitive

import (
	"math/bits"
	"math/rand/v2"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

/*
scalarAffinityPopcount is the obvious Go reference the kernel must agree
with. Kept local to the test so production code never has a scalar
shadow path the hot call site could accidentally drift toward.
*/
func scalarAffinityPopcount(vector [AffinityWords]uint64) int {
	total := 0

	for idx, word := range vector {
		if idx == AffinityWords-1 {
			word &= AffinityLastWordMask
		}

		total += bits.OnesCount64(word)
	}

	return total
}

/*
scalarAffinityCoupling mirrors the old in-place Jaccard numerator /
denominator so parity can be asserted bit-for-bit across thousands of
random vectors.
*/
func scalarAffinityCoupling(a, b [AffinityWords]uint64) (int, int) {
	intersection := 0
	union := 0

	for wordIdx := range AffinityWords {
		aWord := a[wordIdx]
		bWord := b[wordIdx]

		if wordIdx == AffinityWords-1 {
			aWord &= AffinityLastWordMask
			bWord &= AffinityLastWordMask
		}

		intersection += bits.OnesCount64(aWord & bWord)
		union += bits.OnesCount64(aWord | bWord)
	}

	return intersection, union
}

/*
randomAffinity produces a pseudo-random vector including garbage bits in
the 63 unused positions of the fifth word, so the test exercises the
kernel's tail-mask obligation rather than giving it a pre-sanitised input.
*/
func randomAffinity(rng *rand.Rand) Affinity {
	var vec [AffinityWords]uint64

	for idx := range AffinityWords {
		vec[idx] = rng.Uint64()
	}

	return AffinityWithVector(vec)
}

func TestAffinityPopcount(t *testing.T) {
	convey.Convey("Given an Affinity whose Popcount delegates to the SIMD kernel", t, func() {
		convey.Convey("All-zero vector returns 0", func() {
			affinity := NewAffinity()
			convey.So(affinity.Popcount(), convey.ShouldEqual, 0)
		})

		convey.Convey("All-ones vector returns 257 (257-bit mask honoured)", func() {
			var vec [AffinityWords]uint64

			for idx := range AffinityWords {
				vec[idx] = ^uint64(0)
			}

			affinity := AffinityWithVector(vec)
			convey.So(affinity.Popcount(), convey.ShouldEqual, 257)
		})

		convey.Convey("Garbage bits beyond bit 256 are masked out", func() {
			var vec [AffinityWords]uint64

			vec[AffinityWords-1] = ^uint64(0) // 64 bits set, only bit 0 counts

			affinity := AffinityWithVector(vec)
			convey.So(affinity.Popcount(), convey.ShouldEqual, 1)
		})

		convey.Convey("Kernel matches scalar reference on random vectors", func() {
			rng := rand.New(rand.NewPCG(1, 2))

			for range 4096 {
				affinity := randomAffinity(rng)
				expected := scalarAffinityPopcount(affinity.vector)
				convey.So(affinity.Popcount(), convey.ShouldEqual, expected)
			}
		})
	})
}

func TestAffinityCoupling(t *testing.T) {
	convey.Convey("Given two Affinity vectors whose Coupling delegates to SIMD", t, func() {
		convey.Convey("Two zero vectors yield 0 (union is empty)", func() {
			a := NewAffinity()
			b := NewAffinity()
			convey.So(a.Coupling(b), convey.ShouldEqual, 0)
		})

		convey.Convey("Identical nonzero vectors yield 1.0", func() {
			vec := [AffinityWords]uint64{
				0xdeadbeefcafebabe,
				0x0123456789abcdef,
				0xfedcba9876543210,
				0xaaaaaaaaaaaaaaaa,
				1,
			}

			a := NewAffinityFromVector(vec)
			b := NewAffinityFromVector(vec)
			convey.So(a.Coupling(b), convey.ShouldEqual, 1.0)
		})

		convey.Convey("Disjoint vectors yield 0", func() {
			aVec := [AffinityWords]uint64{0xAAAAAAAAAAAAAAAA, 0, 0, 0, 0}
			bVec := [AffinityWords]uint64{0x5555555555555555, 0, 0, 0, 0}

			a := NewAffinityFromVector(aVec)
			b := NewAffinityFromVector(bVec)
			convey.So(a.Coupling(b), convey.ShouldEqual, 0)
		})

		convey.Convey("Garbage bits in word 4 do not leak into Jaccard", func() {
			aVec := [AffinityWords]uint64{1, 0, 0, 0, ^uint64(0)}
			bVec := [AffinityWords]uint64{1, 0, 0, 0, ^uint64(0)}

			a := NewAffinityFromVector(aVec)
			b := NewAffinityFromVector(bVec)

			// Both vectors are effectively {bit 0 of word 0, bit 0 of word 4}
			// = 2 bits, fully overlapping → 1.0, not contaminated by the
			// 63 garbage bits.
			convey.So(a.Coupling(b), convey.ShouldEqual, 1.0)
		})

		convey.Convey("Kernel matches scalar reference on random pairs", func() {
			rng := rand.New(rand.NewPCG(3, 4))

			for range 4096 {
				a := randomAffinity(rng)
				b := randomAffinity(rng)

				intersection, union := scalarAffinityCoupling(a.vector, b.vector)

				var expected float64

				if union > 0 {
					expected = float64(intersection) / float64(union)
				}

				convey.So(a.Coupling(&b), convey.ShouldEqual, expected)
			}
		})
	})
}

func BenchmarkAffinityPopcount(b *testing.B) {
	rng := rand.New(rand.NewPCG(7, 8))
	affinity := randomAffinity(rng)

	b.ResetTimer()
	b.ReportAllocs()

	var sink int

	for range b.N {
		sink ^= affinity.Popcount()
	}

	runtimeSink = sink
}

func BenchmarkAffinityCoupling(b *testing.B) {
	rng := rand.New(rand.NewPCG(9, 10))
	a := randomAffinity(rng)
	other := randomAffinity(rng)

	b.ResetTimer()
	b.ReportAllocs()

	var sink float64

	for range b.N {
		sink += a.Coupling(&other)
	}

	runtimeSinkF = sink
}

var (
	runtimeSink  int
	runtimeSinkF float64
)
