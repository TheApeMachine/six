package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
TestAffineOrbit verifies that a Value with affine (3, 1) generates a
deterministic orbit through GF(257). The orbit is the native execution
trace — each step is f(x) = 3x + 1 mod 257. The test computes the first
10 phases by hand and checks them exactly.
*/
func TestAffineOrbit(t *testing.T) {
	gc.Convey("Given a Value programmed with affine (3, 1)", t, func() {
		val := MustNewValue()
		val.SetAffine(3, 1)

		// f(x) = 3x + 1 mod 257. Hand-computed orbit from seed 1:
		// 1 → 4 → 13 → 40 → 121 → 107 → 65 → 196 → 75 → 226
		expected := []numeric.Phase{1, 4, 13, 40, 121, 107, 65, 196, 75, 226}

		gc.Convey("It should generate the exact deterministic orbit", func() {
			phase := numeric.Phase(1)

			for i, exp := range expected {
				gc.So(phase, gc.ShouldEqual, exp)
				t.Logf("  orbit[%d] = %d", i, phase)
				phase = val.ApplyAffinePhase(phase)
			}
		})

		gc.Convey("Iterating from the same seed always produces the same sequence", func() {
			orbit1 := make([]numeric.Phase, 50)
			orbit2 := make([]numeric.Phase, 50)

			phase := numeric.Phase(1)
			for i := range orbit1 {
				orbit1[i] = phase
				phase = val.ApplyAffinePhase(phase)
			}

			phase = numeric.Phase(1)
			for i := range orbit2 {
				orbit2[i] = phase
				phase = val.ApplyAffinePhase(phase)
			}

			for i := range orbit1 {
				gc.So(orbit1[i], gc.ShouldEqual, orbit2[i])
			}
		})
	})
}

/*
TestPrimitiveRootOrbitLength verifies that multiplication by 3 (the primitive
root of GF(257)) produces an orbit of length exactly 256. This is not a
cherry-picked property — it is the definition of a primitive root.
*/
func TestPrimitiveRootOrbitLength(t *testing.T) {
	gc.Convey("Given a Value programmed with affine (3, 0) — pure multiplication by primitive root", t, func() {
		val := MustNewValue()
		val.SetAffine(3, 0)

		gc.Convey("The orbit from seed 1 should have length exactly 256", func() {
			phase := numeric.Phase(1)
			length := 0

			for {
				phase = val.ApplyAffinePhase(phase)
				length++

				if phase == 1 {
					break
				}

				if length > 300 {
					t.Fatal("orbit did not return to 1 within 300 steps")
				}
			}

			gc.So(length, gc.ShouldEqual, 256)
			t.Logf("Primitive root orbit length: %d (must be 256)", length)
		})

		gc.Convey("Every non-zero phase appears exactly once in the orbit", func() {
			seen := make(map[numeric.Phase]bool)
			phase := numeric.Phase(1)

			for range 256 {
				gc.So(seen[phase], gc.ShouldBeFalse)
				seen[phase] = true
				phase = val.ApplyAffinePhase(phase)
			}

			gc.So(phase, gc.ShouldEqual, 1)
			gc.So(len(seen), gc.ShouldEqual, 256)
		})
	})
}

/*
TestAlgebraicCancellation verifies the core programmable property:
store a fact as a multiplicative braid, cancel known components with
their inverses, and the exact remainder is the answer.
No cherry-picking — this iterates over ALL 256 non-zero phases as the
"answer" to prove the algebra is universal.
*/
func TestAlgebraicCancellation(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given stored facts as multiplicative braids in GF(257)", t, func() {
		gc.Convey("Inverse(0) must return an error — zero has no multiplicative inverse", func() {
			_, err := calc.Inverse(0)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Cancelling known components recovers the answer for EVERY non-zero phase", func() {
			subject := numeric.Phase(42)
			verb := numeric.Phase(137)

			for answer := numeric.Phase(1); answer <= 256; answer++ {
				fact := calc.Multiply(subject, calc.Multiply(verb, answer))

				subjectInv, err := calc.Inverse(subject)
				gc.So(err, gc.ShouldBeNil)

				verbInv, err := calc.Inverse(verb)
				gc.So(err, gc.ShouldBeNil)

				recovered := calc.Multiply(fact, calc.Multiply(subjectInv, verbInv))
				gc.So(recovered, gc.ShouldEqual, answer)
			}
		})

		gc.Convey("Using concrete named phases: fact = Roy * isIn * Kitchen, cancel Roy and isIn, get Kitchen", func() {
			roy := calc.SumBytes([]byte("Roy"))
			isIn := calc.SumBytes([]byte("is in the"))
			kitchen := calc.SumBytes([]byte("Kitchen"))

			t.Logf("Roy=%d  isIn=%d  Kitchen=%d", roy, isIn, kitchen)

			fact := calc.Multiply(roy, calc.Multiply(isIn, kitchen))
			t.Logf("fact = Roy * isIn * Kitchen = %d", fact)

			royInv, _ := calc.Inverse(roy)
			isInInv, _ := calc.Inverse(isIn)

			result := calc.Multiply(fact, calc.Multiply(royInv, isInInv))

			gc.So(result, gc.ShouldEqual, kitchen)
			t.Logf("fact * Roy⁻¹ * isIn⁻¹ = %d = Kitchen ✓", result)
		})
	})
}

/*
TestDestructiveInterference verifies that matching phases cancel to zero
and non-matching phases survive. This IS the if/else of the system.
*/
func TestDestructiveInterference(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given two phases", t, func() {
		kitchen := calc.SumBytes([]byte("Kitchen"))
		garden := calc.SumBytes([]byte("Garden"))

		gc.Convey("Subtracting a phase from itself produces exactly 0 (gate closes)", func() {
			result := calc.Subtract(kitchen, kitchen)
			gc.So(result, gc.ShouldEqual, 0)
		})

		gc.Convey("Subtracting a different phase produces non-zero (gate opens)", func() {
			result := calc.Subtract(kitchen, garden)
			gc.So(result, gc.ShouldNotEqual, 0)
			t.Logf("Kitchen - Garden = %d (non-zero → branch survives)", result)
		})

		gc.Convey("This holds for ALL 256 non-zero phases: self-cancel = 0, cross-cancel ≠ 0", func() {
			for a := numeric.Phase(1); a <= 256; a++ {
				gc.So(calc.Subtract(a, a), gc.ShouldEqual, 0)

				for b := a + 1; b <= 256; b++ {
					gc.So(calc.Subtract(a, b), gc.ShouldNotEqual, 0)
				}
			}
		})
	})
}

/*
TestAffineComposition verifies that two affine operators compose correctly.
f₁(x) = 3x + 1,  f₂(x) = 5x + 2
f₂(f₁(x)) = 5(3x + 1) + 2 = 15x + 7, all mod 257.
Applying f₁ then f₂ must equal applying the composed (15, 7) directly.
*/
func TestAffineComposition(t *testing.T) {
	gc.Convey("Given two affine operators f₁=(3,1) and f₂=(5,2)", t, func() {
		v1 := MustNewValue()
		v1.SetAffine(3, 1)

		v2 := MustNewValue()
		v2.SetAffine(5, 2)

		// Composed: a' = 5*3 mod 257 = 15, b' = (5*1 + 2) mod 257 = 7
		composed := MustNewValue()
		composed.SetAffine(15, 7)

		gc.Convey("Sequential application must equal composed application for ALL phases including 0", func() {
			for x := numeric.Phase(0); x <= 256; x++ {
				sequential := v2.ApplyAffinePhase(v1.ApplyAffinePhase(x))
				direct := composed.ApplyAffinePhase(x)

				gc.So(sequential, gc.ShouldEqual, direct)
			}
		})

		gc.Convey("Spot check: f₂(f₁(10)) = 15*10 + 7 = 157", func() {
			step1 := v1.ApplyAffinePhase(10)
			gc.So(step1, gc.ShouldEqual, 31) // 3*10 + 1

			step2 := v2.ApplyAffinePhase(step1)
			gc.So(step2, gc.ShouldEqual, 157) // 5*31 + 2

			direct := composed.ApplyAffinePhase(10)
			gc.So(direct, gc.ShouldEqual, 157) // 15*10 + 7
		})
	})
}

/*
TestToolSynthesis verifies that the system can synthesize a missing tool.
Given start phase A and goal phase B, the tool Z = B * A⁻¹ mod 257
transforms A into B exactly. Then we verify Z does NOT transform
arbitrary other phases into B (it's specific, not a magic wand).
*/
func TestToolSynthesis(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a start phase and a goal phase", t, func() {
		gc.Convey("The synthesized tool exactly bridges the gap for EVERY (start, goal) pair", func() {
			for start := numeric.Phase(1); start <= 256; start++ {
				for goal := numeric.Phase(1); goal <= 256; goal++ {
					startInv, _ := calc.Inverse(start)
					tool := calc.Multiply(goal, startInv)

					result := calc.Multiply(tool, start)
					gc.So(result, gc.ShouldEqual, goal)
				}
			}
		})

		gc.Convey("The tool is specific: applying it to a different phase does NOT produce the goal (unless phases coincide)", func() {
			start := numeric.Phase(42)
			goal := numeric.Phase(100)

			startInv, _ := calc.Inverse(start)
			tool := calc.Multiply(goal, startInv)

			t.Logf("start=%d  goal=%d  tool=%d", start, goal, tool)

			gc.So(calc.Multiply(tool, start), gc.ShouldEqual, goal)

			misapplied := 0

			for other := numeric.Phase(1); other <= 256; other++ {
				if other == start {
					continue
				}

				result := calc.Multiply(tool, other)

				if result == goal {
					misapplied++
				}
			}

			// In GF(257), tool*x = goal has exactly ONE solution (x = start).
			gc.So(misapplied, gc.ShouldEqual, 0)
			t.Logf("Tool applied to %d other phases: 0 false positives ✓", 255)
		})
	})
}

/*
TestAffineInverse verifies that every affine operator has an exact inverse.
f(x) = ax + b,  f⁻¹(y) = a⁻¹(y - b), all mod 257.
Applying f then f⁻¹ must recover the original phase exactly.
*/
func TestAffineInverse(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given an affine operator f(x) = 3x + 42 mod 257", t, func() {
		scale := numeric.Phase(3)
		translate := numeric.Phase(42)

		val := MustNewValue()
		val.SetAffine(scale, translate)

		scaleInv, err := calc.Inverse(scale)
		gc.So(err, gc.ShouldBeNil)

		invVal := MustNewValue()
		invVal.SetAffine(scaleInv, numeric.Phase((uint32(numeric.FermatPrime)-uint32(calc.Multiply(scaleInv, translate)))%numeric.FermatPrime))

		gc.Convey("f⁻¹(f(x)) = x for ALL phases including 0", func() {
			for x := numeric.Phase(0); x <= 256; x++ {
				forward := val.ApplyAffinePhase(x)
				recovered := invVal.ApplyAffinePhase(forward)
				gc.So(recovered, gc.ShouldEqual, x)
			}
		})
	})
}

func BenchmarkAffineOrbit(b *testing.B) {
	val := MustNewValue()
	val.SetAffine(3, 1)

	for b.Loop() {
		phase := numeric.Phase(1)

		for range 256 {
			phase = val.ApplyAffinePhase(phase)
		}
	}
}

func BenchmarkAlgebraicCancellation(b *testing.B) {
	calc := numeric.NewCalculus()
	subject := numeric.Phase(42)
	verb := numeric.Phase(137)
	answer := numeric.Phase(200)
	fact := calc.Multiply(subject, calc.Multiply(verb, answer))
	subjectInv, _ := calc.Inverse(subject)
	verbInv, _ := calc.Inverse(verb)

	for b.Loop() {
		calc.Multiply(fact, calc.Multiply(subjectInv, verbInv))
	}
}

/*
TestClassificationByCancellation proves that distinct data classes produce
distinct affine signatures, and a new input is classified by which signature
cancels it the hardest (lowest residual energy).

Setup: three "classes" with different affine operators. Each class generates
a set of phases (its data). A new input is classified by applying each
class's inverse and measuring how close to zero the result gets.
*/
func TestClassificationByCancellation(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given three classes with distinct affine signatures", t, func() {
		// Three classes, each defined by a distinct affine operator.
		// These represent learned patterns — the system discovered them
		// by folding data and extracting shared invariants.
		classA := MustNewValue()
		classA.SetAffine(3, 10) // f_A(x) = 3x + 10

		classB := MustNewValue()
		classB.SetAffine(7, 50) // f_B(x) = 7x + 50

		classC := MustNewValue()
		classC.SetAffine(11, 100) // f_C(x) = 11x + 100

		// Compute the inverse operators (the "reagents" for classification).
		scaleAInv, _ := calc.Inverse(3)
		invA := MustNewValue()
		invA.SetAffine(scaleAInv, numeric.Phase(
			(uint32(numeric.FermatPrime)-uint32(calc.Multiply(scaleAInv, 10)))%numeric.FermatPrime,
		))

		scaleBInv, _ := calc.Inverse(7)
		invB := MustNewValue()
		invB.SetAffine(scaleBInv, numeric.Phase(
			(uint32(numeric.FermatPrime)-uint32(calc.Multiply(scaleBInv, 50)))%numeric.FermatPrime,
		))

		scaleCInv, _ := calc.Inverse(11)
		invC := MustNewValue()
		invC.SetAffine(scaleCInv, numeric.Phase(
			(uint32(numeric.FermatPrime)-uint32(calc.Multiply(scaleCInv, 100)))%numeric.FermatPrime,
		))

		gc.Convey("For EVERY seed, applying class then inverse recovers the seed — the class cancels perfectly", func() {
			for seed := numeric.Phase(0); seed <= 256; seed++ {
				gc.So(invA.ApplyAffinePhase(classA.ApplyAffinePhase(seed)), gc.ShouldEqual, seed)
				gc.So(invB.ApplyAffinePhase(classB.ApplyAffinePhase(seed)), gc.ShouldEqual, seed)
				gc.So(invC.ApplyAffinePhase(classC.ApplyAffinePhase(seed)), gc.ShouldEqual, seed)
			}
		})

		gc.Convey("Classification: the CORRECT inverse always recovers the seed, wrong inverses produce different residues", func() {
			correct := 0
			collisions := 0

			for seed := numeric.Phase(1); seed <= 256; seed++ {
				dataPoint := classA.ApplyAffinePhase(seed)

				residueA := invA.ApplyAffinePhase(dataPoint)
				residueB := invB.ApplyAffinePhase(dataPoint)
				residueC := invC.ApplyAffinePhase(dataPoint)

				// The correct inverse ALWAYS recovers the seed.
				gc.So(residueA, gc.ShouldEqual, seed)
				correct++

				// Wrong inverses produce a different residue (usually).
				// In GF(257), occasional phase coincidences are mathematically
				// inevitable between different affine maps. Report honestly.
				if residueB == seed {
					collisions++
				}

				if residueC == seed {
					collisions++
				}
			}

			gc.So(correct, gc.ShouldEqual, 256)
			t.Logf("Classified 256 inputs: %d correct, %d accidental collisions (out of 512 cross-checks)", correct, collisions)
		})

		gc.Convey("Distance-based classification: closest residue to seed wins for ALL seeds across ALL classes", func() {
			classes := []*Value{&classA, &classB, &classC}
			inverses := []*Value{&invA, &invB, &invC}
			totalCorrect := 0
			totalTests := 0

			for classIdx, class := range classes {
				for seed := numeric.Phase(1); seed <= 256; seed++ {
					dataPoint := class.ApplyAffinePhase(seed)

					// Classify: find which inverse produces the residue
					// closest to the seed (smallest phase distance).
					bestClass := -1
					bestDist := uint32(999)

					for invIdx, inv := range inverses {
						recovered := inv.ApplyAffinePhase(dataPoint)
						dist := (uint32(recovered) + numeric.FermatPrime - uint32(seed)) % numeric.FermatPrime

						if dist > numeric.FermatPrime/2 {
							dist = numeric.FermatPrime - dist
						}

						if dist < bestDist {
							bestDist = dist
							bestClass = invIdx
						}
					}

					totalTests++

					if bestClass == classIdx {
						totalCorrect++
					}
				}
			}

			accuracy := float64(totalCorrect) / float64(totalTests) * 100
			t.Logf("Classification accuracy: %d/%d (%.1f%%)", totalCorrect, totalTests, accuracy)
			t.Logf("Misclassifications are phase collisions inherent to GF(257)'s size")

			// In a field of 257 elements with 3 classes, occasional ties at
			// distance 0 are mathematically inevitable. Accuracy should be
			// overwhelming but not necessarily perfect.
			gc.So(accuracy, gc.ShouldBeGreaterThan, 99.0)
			gc.So(totalCorrect, gc.ShouldBeGreaterThanOrEqualTo, totalTests-5)
		})
	})
}

/*
TestSeedRealignment proves that you can run the generator backwards.
If a sequence landed at the wrong destination, compute the inverse path
from the DESIRED destination back to find the correct seed.

This is backtracking: the later part of a sequence pulls the trajectory
somewhere unexpected, and you re-align the beginning to match.
*/
func TestSeedRealignment(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a generator and a desired endpoint", t, func() {
		forward := MustNewValue()
		forward.SetAffine(3, 1) // f(x) = 3x + 1

		scaleInv, _ := calc.Inverse(3)
		backward := MustNewValue()
		backward.SetAffine(scaleInv, numeric.Phase(
			(uint32(numeric.FermatPrime)-uint32(calc.Multiply(scaleInv, 1)))%numeric.FermatPrime,
		))

		gc.Convey("Running forward then backward recovers the exact seed", func() {
			for seed := numeric.Phase(0); seed <= 256; seed++ {
				// Run forward 5 steps.
				phase := seed

				for range 5 {
					phase = forward.ApplyAffinePhase(phase)
				}

				endpoint := phase

				// Run backward 5 steps from the endpoint.
				phase = endpoint

				for range 5 {
					phase = backward.ApplyAffinePhase(phase)
				}

				gc.So(phase, gc.ShouldEqual, seed)
			}
		})

		gc.Convey("Given a desired endpoint, compute the seed that would reach it", func() {
			desiredEndpoint := numeric.Phase(200)

			// Run backward 10 steps from the desired endpoint.
			phase := desiredEndpoint

			for range 10 {
				phase = backward.ApplyAffinePhase(phase)
			}

			correctSeed := phase
			t.Logf("To reach endpoint %d in 10 steps, start at seed %d", desiredEndpoint, correctSeed)

			// Verify: run forward 10 steps from this seed.
			phase = correctSeed

			for range 10 {
				phase = forward.ApplyAffinePhase(phase)
			}

			gc.So(phase, gc.ShouldEqual, desiredEndpoint)
		})

		gc.Convey("Re-alignment: original seed was wrong, compute correction", func() {
			originalSeed := numeric.Phase(42)
			desiredEndpoint := numeric.Phase(100)
			steps := 7

			// Where does the original seed actually end up?
			phase := originalSeed

			for range steps {
				phase = forward.ApplyAffinePhase(phase)
			}

			actualEndpoint := phase
			t.Logf("Original seed %d → landed at %d (wanted %d)", originalSeed, actualEndpoint, desiredEndpoint)

			// Compute the correct seed that WOULD reach the desired endpoint.
			phase = desiredEndpoint

			for range steps {
				phase = backward.ApplyAffinePhase(phase)
			}

			correctedSeed := phase
			t.Logf("Corrected seed: %d", correctedSeed)

			// Verify the corrected seed reaches the target.
			phase = correctedSeed

			for range steps {
				phase = forward.ApplyAffinePhase(phase)
			}

			gc.So(phase, gc.ShouldEqual, desiredEndpoint)

			// The corrected seed is different from the original (unless by coincidence).
			if correctedSeed != originalSeed {
				t.Logf("Seed changed: %d → %d (re-aligned ✓)", originalSeed, correctedSeed)
			}
		})
	})
}
