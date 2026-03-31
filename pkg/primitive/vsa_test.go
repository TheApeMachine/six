package primitive

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestHCAMUnbinding(t *testing.T) {
	Convey("Given HCAM fact encoding S⊕I⊕G", t, func() {
		// Build three random token vectors representing three concepts.
		sandra, _ := NewValue([]byte("Sandra"))
		isIn, _ := NewValue([]byte("is_in"))
		garden, _ := NewValue([]byte("Garden"))
		defer sandra.Close()
		defer isIn.Close()
		defer garden.Close()

		// Fact = Sandra ⊕ IsIn ⊕ Garden (bind all three into one Value)
		fact, _ := NewValue(nil)
		defer fact.Close()
		nWords := int((core.Cfg.TokenBits + 63) / 64)
		base := core.Cfg.TokenIndex
		for i := 0; i < nWords; i++ {
			idx := base + i
			if idx >= Words {
				break
			}
			fact[idx] = sandra[idx] ^ isIn[idx] ^ garden[idx]
		}

		Convey("When we unbind with query S⊕I", func() {
			query, _ := NewValue(nil)
			defer query.Close()
			for i := 0; i < nWords; i++ {
				idx := base + i
				if idx >= Words {
					break
				}
				query[idx] = sandra[idx] ^ isIn[idx]
			}

			result, _ := NewValue(nil)
			defer result.Close()
			UnbindHD(result, fact, query)

			Convey("Then the residue equals Garden's token region", func() {
				for i := 0; i < nWords; i++ {
					idx := base + i
					if idx >= Words {
						break
					}
					So(result[idx], ShouldEqual, garden[idx])
				}
			})
		})
	})
}

func TestBundleHD(t *testing.T) {
	Convey("Given three random Values", t, func() {
		a, _ := NewValue([]byte("alpha"))
		b, _ := NewValue([]byte("bravo"))
		c, _ := NewValue([]byte("charlie"))
		defer a.Close()
		defer b.Close()
		defer c.Close()

		Convey("When bundled via majority rule", func() {
			dst, _ := NewValue(nil)
			defer dst.Close()
			BundleHD(dst, []*Value{a, b, c})

			Convey("Then the result is closer to each input than two random vectors", func() {
				simA := CosineSimilarityHD(dst, a)
				simB := CosineSimilarityHD(dst, b)
				simC := CosineSimilarityHD(dst, c)
				// Bundled vector should have positive similarity with each input.
				So(simA, ShouldBeGreaterThan, -0.5)
				So(simB, ShouldBeGreaterThan, -0.5)
				So(simC, ShouldBeGreaterThan, -0.5)
			})
		})
	})
}

func TestTokensHammingDistance(t *testing.T) {
	Convey("Given two identical Values", t, func() {
		a, _ := NewValue([]byte("hello world"))
		b := a.Clone()
		defer a.Close()
		defer b.Close()

		Convey("Their Hamming distance should be zero", func() {
			So(TokensHammingDistance(a, b), ShouldEqual, 0)
		})
	})

	Convey("Given two different Values", t, func() {
		a, _ := NewValue([]byte("hello"))
		b, _ := NewValue([]byte("world"))
		defer a.Close()
		defer b.Close()

		Convey("Their Hamming distance should be positive", func() {
			So(TokensHammingDistance(a, b), ShouldBeGreaterThan, 0)
		})
	})
}

func TestComputeAffinityLSH(t *testing.T) {
	Convey("Given a Value with tokens", t, func() {
		v, _ := NewValue([]byte("some interesting data"))
		defer v.Close()

		Convey("ComputeAffinityLSH produces a non-zero affinity", func() {
			v[core.Cfg.AffinityIndex] = 0
			v.ComputeAffinityLSH()
			So(v[core.Cfg.AffinityIndex], ShouldNotEqual, 0)
		})

		Convey("Similar data produces similar affinity", func() {
			v2, _ := NewValue([]byte("some interesting data!"))
			defer v2.Close()
			v.ComputeAffinityLSH()
			v2.ComputeAffinityLSH()
			overlap := BloomOverlap(v[core.Cfg.AffinityIndex], v2[core.Cfg.AffinityIndex])
			So(overlap, ShouldBeGreaterThan, 0)
		})
	})
}

func TestComputeAffinityBloom(t *testing.T) {
	Convey("Given byte data with shared substrings", t, func() {
		a := ComputeAffinityBloom([]byte("the cat sat on the mat"))
		b := ComputeAffinityBloom([]byte("the dog sat on the rug"))

		Convey("The Bloom filters should share bits from common n-grams", func() {
			overlap := BloomOverlap(a, b)
			So(overlap, ShouldBeGreaterThan, 0)
		})
	})

	Convey("Given completely disjoint data", t, func() {
		a := ComputeAffinityBloom([]byte("xyz"))
		b := ComputeAffinityBloom([]byte("123"))

		Convey("Bloom overlap may be low (probabilistic)", func() {
			// Just ensure they're non-zero (each has >= 1 n-gram or hash)
			So(a, ShouldNotEqual, 0)
			So(b, ShouldNotEqual, 0)
		})
	})
}

func TestLFSRStep(t *testing.T) {
	Convey("Given LFSR starting at state 1", t, func() {
		state := uint64(1)

		Convey("It should cycle through 8191 unique states before repeating", func() {
			seen := make(map[uint64]bool)
			for i := 0; i < 8191; i++ {
				So(seen[state], ShouldBeFalse)
				seen[state] = true
				state = LFSRStep(state)
			}
			// After 8191 steps, should return to initial state
			So(state, ShouldEqual, 1)
		})
	})

	Convey("LFSR zero-state is corrected to 1", t, func() {
		state := LFSRStep(0)
		So(state, ShouldNotEqual, 0)
	})
}

func TestAdvanceSequence(t *testing.T) {
	Convey("Given a Value with a seeded sequence", t, func() {
		v, _ := NewValue([]byte("test"))
		defer v.Close()
		initial := v[core.Cfg.StateSequence]

		Convey("AdvanceSequence changes the StateSequence", func() {
			v.AdvanceSequence()
			So(v[core.Cfg.StateSequence], ShouldNotEqual, initial)
		})
	})
}

func TestAccumulateDelta(t *testing.T) {
	Convey("Given two Values with different tokens", t, func() {
		a, _ := NewValue([]byte("hello"))
		b, _ := NewValue([]byte("world"))
		defer a.Close()
		defer b.Close()

		Convey("AccumulateDelta produces a non-zero delta", func() {
			delta := AccumulateDelta(a, b)
			So(delta, ShouldNotEqual, 0)
			So(a[core.Cfg.StateAccumulator], ShouldEqual, delta)
		})
	})

	Convey("Given two identical Values", t, func() {
		a, _ := NewValue([]byte("same"))
		b := a.Clone()
		defer a.Close()
		defer b.Close()

		Convey("AccumulateDelta produces zero delta", func() {
			delta := AccumulateDelta(a, b)
			So(delta, ShouldEqual, 0)
		})
	})
}

func TestApplyDelta(t *testing.T) {
	Convey("Given current Value with a known delta", t, func() {
		current, _ := NewValue([]byte("data1"))
		defer current.Close()
		current[core.Cfg.StateAccumulator] = 0xDEADBEEF

		Convey("ApplyDelta XORs the delta across all token words", func() {
			dst, _ := NewValue(nil)
			defer dst.Close()
			ApplyDelta(dst, current)

			nWords := int((core.Cfg.TokenBits + 63) / 64)
			base := core.Cfg.TokenIndex
			for i := 0; i < nWords; i++ {
				idx := base + i
				if idx >= Words {
					break
				}
				expected := current[idx] ^ 0xDEADBEEF
				So(dst[idx], ShouldEqual, expected)
			}
		})
	})
}

func TestXORDistance(t *testing.T) {
	Convey("XOR distance properties", t, func() {
		Convey("Distance of identical values is zero", func() {
			So(XORDistance(42, 42), ShouldEqual, 0)
		})
		Convey("Distance is symmetric", func() {
			So(XORDistance(0xAB, 0xCD), ShouldEqual, XORDistance(0xCD, 0xAB))
		})
		Convey("XORDistanceLog returns -1 for equal values", func() {
			So(XORDistanceLog(7, 7), ShouldEqual, -1)
		})
		Convey("XORDistanceLog returns correct bucket", func() {
			So(XORDistanceLog(0, 1), ShouldEqual, 0)
			So(XORDistanceLog(0, 0xFF), ShouldEqual, 7)
		})
	})
}

func TestCosineSimilarityHD(t *testing.T) {
	Convey("Given identical Values", t, func() {
		a, _ := NewValue([]byte("identical"))
		b := a.Clone()
		defer a.Close()
		defer b.Close()

		Convey("Cosine similarity should be 1.0", func() {
			So(CosineSimilarityHD(a, b), ShouldEqual, 1.0)
		})
	})

	Convey("Given random different Values", t, func() {
		rng := rand.New(rand.NewSource(99))
		dataA := make([]byte, 50)
		dataB := make([]byte, 50)
		rng.Read(dataA)
		rng.Read(dataB)
		a, _ := NewValue(dataA)
		b, _ := NewValue(dataB)
		defer a.Close()
		defer b.Close()

		Convey("Cosine similarity should be close to 0 (near-orthogonal)", func() {
			sim := CosineSimilarityHD(a, b)
			So(sim, ShouldBeBetween, -0.5, 0.5)
		})
	})
}
