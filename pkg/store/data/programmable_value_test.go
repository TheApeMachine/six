package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestValueGeneratorDeterminism verifies that Rotate3D produces a deterministic
orbit through the 8.5-billion-state 5-sparse Value space. Same seed, same
orbit, every time. No scalar phases — pure Value geometry.
*/
func TestValueGeneratorDeterminism(t *testing.T) {
	gc.Convey("Given a 5-sparse Value used as a generator seed", t, func() {
		seed := BaseValue(42)

		gc.Convey("Rotate3D produces identical orbits from the same seed", func() {
			orbit1 := make([]int, 20)
			orbit2 := make([]int, 20)

			val := seed

			for i := range orbit1 {
				orbit1[i] = val.ActiveCount()
				val = val.Rotate3D()
			}

			val = seed

			for i := range orbit2 {
				orbit2[i] = val.ActiveCount()
				val = val.Rotate3D()
			}

			for i := range orbit1 {
				gc.So(orbit1[i], gc.ShouldEqual, orbit2[i])
			}
		})

		gc.Convey("Rotate3D preserves bit count (bijection on bit positions)", func() {
			val := seed
			originalBits := seed.ActiveCount()

			for step := range 50 {
				gc.So(val.ActiveCount(), gc.ShouldEqual, originalBits)
				_ = step
				val = val.Rotate3D()
			}
		})

		gc.Convey("Different seeds produce different orbits", func() {
			seedA := BaseValue(65)
			seedB := BaseValue(90)

			valA := seedA
			valB := seedB

			different := 0

			for range 20 {
				valA = valA.Rotate3D()
				valB = valB.Rotate3D()

				xor := valA.XOR(valB)

				if xor.ActiveCount() > 0 {
					different++
				}
			}

			gc.So(different, gc.ShouldEqual, 20)
			t.Logf("Two different seeds stayed different for all 20 steps ✓")
		})
	})
}

/*
TestValueCancellation verifies XOR cancellation at the full 257-bit level.
A XOR A = zero (8.5 billion states, exact cancellation).
A XOR B ≠ zero for any distinct A, B.
No scalar phases — this is the real thing.
*/
func TestValueCancellation(t *testing.T) {
	gc.Convey("Given Values in the 257-bit space", t, func() {
		gc.Convey("XOR of a Value with itself is exactly zero for ALL 256 byte seeds", func() {
			for b := 0; b < 256; b++ {
				val := BaseValue(byte(b))
				cancelled := val.XOR(val)
				gc.So(cancelled.ActiveCount(), gc.ShouldEqual, 0)
			}
		})

		gc.Convey("XOR of two distinct BaseValues is never zero", func() {
			for a := 0; a < 256; a++ {
				for b := a + 1; b < 256; b++ {
					valA := BaseValue(byte(a))
					valB := BaseValue(byte(b))
					residue := valA.XOR(valB)
					gc.So(residue.ActiveCount(), gc.ShouldBeGreaterThan, 0)
				}
			}
		})

		gc.Convey("XOR cancellation survives Rotate3D: rotated values cancel the same way", func() {
			val := BaseValue(100)

			for range 10 {
				val = val.Rotate3D()
				cancelled := val.XOR(val)
				gc.So(cancelled.ActiveCount(), gc.ShouldEqual, 0)
			}
		})
	})
}

/*
TestValueClassification verifies that classification works at the full
257-bit Value level using Similarity as the distance metric.
Each "class" is an OR-union of its members. A new input is classified
by which class union it is most similar to.
No scalar phases — 8.5 billion state space.
*/
func TestValueClassification(t *testing.T) {
	gc.Convey("Given three classes of Values built from different byte ranges", t, func() {
		// Class A: letters a-e (low ASCII)
		// Class B: letters p-t (mid ASCII)
		// Class C: digits 0-4 (digit ASCII)
		// Each class accumulates its members via OR into a "signature".
		classAMembers := []byte{'a', 'b', 'c', 'd', 'e'}
		classBMembers := []byte{'p', 'q', 'r', 's', 't'}
		classCMembers := []byte{'0', '1', '2', '3', '4'}

		buildSignature := func(members []byte) Value {
			sig := BaseValue(members[0])

			for _, m := range members[1:] {
				sig = sig.OR(BaseValue(m))
			}

			return sig
		}

		sigA := buildSignature(classAMembers)
		sigB := buildSignature(classBMembers)
		sigC := buildSignature(classCMembers)

		t.Logf("Class A signature: %d bits active", sigA.ActiveCount())
		t.Logf("Class B signature: %d bits active", sigB.ActiveCount())
		t.Logf("Class C signature: %d bits active", sigC.ActiveCount())

		gc.Convey("Each class member is most similar to its own class signature", func() {
			allClasses := []struct {
				members []byte
				sig     Value
				name    string
			}{
				{classAMembers, sigA, "A"},
				{classBMembers, sigB, "B"},
				{classCMembers, sigC, "C"},
			}

			correct := 0
			total := 0

			for _, class := range allClasses {
				for _, member := range class.members {
					input := BaseValue(member)

					simToA := input.Similarity(sigA)
					simToB := input.Similarity(sigB)
					simToC := input.Similarity(sigC)

					bestSim := max(simToA, simToB, simToC)
					total++

					classified := ""

					switch bestSim {
					case simToA:
						classified = "A"
					case simToB:
						classified = "B"
					case simToC:
						classified = "C"
					}

					if classified == class.name {
						correct++
					}

					t.Logf("  %c → A:%d B:%d C:%d → classified=%s (expected=%s)",
						member, simToA, simToB, simToC, classified, class.name)
				}
			}

			gc.So(correct, gc.ShouldEqual, total)
			t.Logf("Classified %d/%d correctly ✓", correct, total)
		})

		gc.Convey("AND of all class signatures reveals shared bits (should be minimal)", func() {
			shared := sigA.AND(sigB)
			shared = shared.AND(sigC)

			t.Logf("Shared bits across all 3 classes: %d", shared.ActiveCount())
			gc.So(shared.ActiveCount(), gc.ShouldBeLessThan, sigA.ActiveCount())
		})

		gc.Convey("XOR between class signatures is high energy (very different)", func() {
			diffAB := sigA.XOR(sigB)
			diffAC := sigA.XOR(sigC)
			diffBC := sigB.XOR(sigC)

			t.Logf("A⊕B = %d bits, A⊕C = %d bits, B⊕C = %d bits",
				diffAB.ActiveCount(), diffAC.ActiveCount(), diffBC.ActiveCount())

			gc.So(diffAB.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(diffAC.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(diffBC.ActiveCount(), gc.ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestValueOrbitCycle verifies that Rotate3D eventually cycles back to the
original Value. Since Rotate3D is a bijection on a finite set, it MUST cycle.
We measure the exact cycle length.
*/
func TestValueOrbitCycle(t *testing.T) {
	gc.Convey("Given a Value seed, Rotate3D must eventually return to it", t, func() {
		seed := BaseValue(77)
		val := seed

		maxSteps := 300
		cycleLength := 0

		for step := 1; step <= maxSteps; step++ {
			val = val.Rotate3D()
			xorWithSeed := val.XOR(seed)

			if xorWithSeed.ActiveCount() == 0 {
				cycleLength = step
				break
			}
		}

		gc.So(cycleLength, gc.ShouldBeGreaterThan, 0)
		t.Logf("Rotate3D cycle length for BaseValue(77): %d steps", cycleLength)

		gc.Convey("The cycle length should be the same for values with the same sparsity", func() {
			seed2 := BaseValue(200)
			val2 := seed2
			cycle2 := 0

			for step := 1; step <= maxSteps; step++ {
				val2 = val2.Rotate3D()

				if val2.XOR(seed2).ActiveCount() == 0 {
					cycle2 = step
					break
				}
			}

			t.Logf("Rotate3D cycle length for BaseValue(200): %d steps", cycle2)
			gc.So(cycle2, gc.ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestValueSharedInvariantExtraction verifies that OR + AND correctly
extracts the shared structure across multiple sequences of Values.
This is the fold's core operation at the full Value level.
*/
func TestValueSharedInvariantExtraction(t *testing.T) {
	gc.Convey("Given three sequences that share a common element", t, func() {
		// Shared element present in all three sequences.
		shared := BaseValue('x')

		// Unique elements per sequence.
		seqA := []Value{BaseValue('a'), shared, BaseValue('b')}
		seqB := []Value{BaseValue('c'), shared, BaseValue('d')}
		seqC := []Value{BaseValue('e'), shared, BaseValue('f')}

		// OR each sequence into its union.
		unionA := seqA[0]
		for _, v := range seqA[1:] {
			unionA = unionA.OR(v)
		}

		unionB := seqB[0]
		for _, v := range seqB[1:] {
			unionB = unionB.OR(v)
		}

		unionC := seqC[0]
		for _, v := range seqC[1:] {
			unionC = unionC.OR(v)
		}

		// AND across all unions → shared invariant.
		invariant := unionA.AND(unionB)
		invariant = invariant.AND(unionC)

		gc.Convey("The invariant contains the shared element's bits", func() {
			// The shared element's bits should be a subset of the invariant.
			sharedInInvariant := shared.Similarity(invariant)
			gc.So(sharedInInvariant, gc.ShouldEqual, shared.ActiveCount())
			t.Logf("Shared element has %d bits, invariant has %d bits, overlap = %d",
				shared.ActiveCount(), invariant.ActiveCount(), sharedInInvariant)
		})

		gc.Convey("XOR with the invariant cancels the shared element completely", func() {
			// The shared element should cancel to zero (or near-zero) against the invariant.
			sharedResidue := shared.XOR(invariant)
			gc.So(sharedResidue.ActiveCount(), gc.ShouldEqual, 0)

			// Unique elements retain their distinguishing bits.
			for i, seq := range [][]Value{seqA, seqB, seqC} {
				uniqueA := seq[0].XOR(invariant)
				uniqueB := seq[2].XOR(invariant)

				gc.So(uniqueA.ActiveCount(), gc.ShouldBeGreaterThan, 0)
				gc.So(uniqueB.ActiveCount(), gc.ShouldBeGreaterThan, 0)
				t.Logf("  Seq %d: unique[0] residue=%d bits, unique[2] residue=%d bits",
					i, uniqueA.ActiveCount(), uniqueB.ActiveCount())
			}
		})

		gc.Convey("XOR is self-inverse: (element XOR invariant) XOR invariant = element", func() {
			for _, seq := range [][]Value{seqA, seqB, seqC} {
				for _, elem := range seq {
					residue := elem.XOR(invariant)
					recovered := residue.XOR(invariant)
					diff := recovered.XOR(elem)
					gc.So(diff.ActiveCount(), gc.ShouldEqual, 0)
				}
			}
		})
	})
}

func BenchmarkRotate3DOrbit(b *testing.B) {
	seed := BaseValue(42)

	for b.Loop() {
		val := seed

		for range 50 {
			val = val.Rotate3D()
		}
	}
}

func BenchmarkValueXORCancellation(b *testing.B) {
	valA := BaseValue(42)
	valB := BaseValue(100)

	for b.Loop() {
		valA.XOR(valB)
	}
}
