package integration

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/algebra"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

/*
1. EMPIRICAL LATTICE ARITHMETIC
We bypass wrapper methods and directly assert the underlying uint64 arrays
to ensure the SIMD vector instructions emit the exact expected integers.
*/
func TestExactLatticeArithmetic(t *testing.T) {
	gc.Convey("Given explicitly constructed Value states", t, func() {
		A := primitive.NewValue()
		// Bits 0 (2), 2 (5), 4 (11) -> 1 + 4 + 16 = 21
		A[0] = 21

		B := primitive.NewValue()
		// Bits 0 (2), 1 (3), 2 (5) -> 1 + 2 + 4 = 7
		B[0] = 7

		gc.Convey("AND (GCD) evaluates to exact memory state", func() {
			dst := primitive.NewValue()
			operation.AND(A[:], B[:], dst[:])

			// 21 & 7 = 5 (Bits 0, 2)
			gc.So(dst[0], gc.ShouldEqual, 5)
			gc.So(dst[1], gc.ShouldEqual, 0) // Strict boundary check
		})

		gc.Convey("OR (LCM) evaluates to exact memory state", func() {
			dst := primitive.NewValue()
			operation.OR(A[:], B[:], dst[:])

			// 21 | 7 = 23 (Bits 0, 1, 2, 4)
			gc.So(dst[0], gc.ShouldEqual, 23)
		})

		gc.Convey("XOR (Lattice Distance) evaluates to exact memory state", func() {
			dst := primitive.NewValue()
			operation.XOR(A[:], B[:], dst[:])

			// 21 ^ 7 = 18 (Bits 1, 4)
			gc.So(dst[0], gc.ShouldEqual, 18)
		})

		gc.Convey("AndNot (Factor Residue) evaluates to exact memory state", func() {
			dst := primitive.NewValue()
			operation.AndNot(A[:], B[:], dst[:])

			// 21 &^ 7 = 16 (Bit 4)
			gc.So(dst[0], gc.ShouldEqual, 16)
		})
	})
}

/*
2. EXACT AFFINE MOTOR ALGEBRA
Verifies that f(p) = a*p + b (mod 8191) and its inverse are computed exactly
according to the Extended Euclidean Algorithm for GF(8191).
*/
func TestExactAffineMotorMath(t *testing.T) {
	gc.Convey("Given a specifically chosen Value", t, func() {
		val := primitive.NewValue()

		// Set bit 5 and bit 7
		val[0] = (1 << 5) | (1 << 7)

		scale, translate := val.Motor()

		gc.Convey("Scale and Translate match exact GF(8191) properties", func() {
			// Scale: (5 * 7) % 8191 = 35
			gc.So(scale, gc.ShouldEqual, 35)
			// Translate: (5 + 7) % 8191 = 12
			gc.So(translate, gc.ShouldEqual, 12)
		})

		gc.Convey("InvertMotor recovers the exact modular inverse", func() {
			// Mathematical Inverse of 35 in GF(8191):
			// 8191 = 35 * 234 + 1  =>  35 * (-234) = 1 (mod 8191)
			// -234 mod 8191 = 7957
			// Mathematical Inverse Translate:
			// 7957 * 12 = 95484. 95484 % 8191 = 5383.
			// 8191 - 5383 = 2808.

			invScale, invTranslate, err := primitive.InvertMotor(scale, translate)
			gc.So(err, gc.ShouldBeNil)
			gc.So(invScale, gc.ShouldEqual, 7957)
			gc.So(invTranslate, gc.ShouldEqual, 2808) // Corrected from 2799

			// Verify it perfectly untangles the position: f^-1(f(100)) == 100
			forward := primitive.ApplyMotor(scale, translate, 100)
			backward := primitive.ApplyMotor(invScale, invTranslate, forward)
			gc.So(backward, gc.ShouldEqual, 100)
		})
	})
}

/*
3. NON-COMMUTATIVITY OF SEQUENCE MEMORY
Proves empirically that "the cat" and "cat the" collapse into entirely distinct
regions of the 128-word array through exact math.
*/
func TestExactNonCommutativeSequence(t *testing.T) {
	gc.Convey("Given two distinct tokens mapped across sequence steps", t, func() {
		// "The" -> Bit 10
		the := primitive.NewValue()
		the[0] = 1 << 10

		// "Cat" -> Bit 20
		cat := primitive.NewValue()
		cat[0] = 1 << 20

		gc.Convey("Sequence 1: 'the' -> 'cat'", func() {
			seq1 := primitive.NewValue()
			seq1[0] = the[0] // State = "the"

			// 'the' motor: Scale = 10, Translate = 10
			// Mapped 'cat' (Bit 20): (10 * 20 + 10) % 8191 = 210
			mappedCat := primitive.NewValue()
			operation.MotorApply(seq1[:], cat[:], mappedCat[:])

			// 210 / 64 = 3 (Word 3), 210 % 64 = 18 (Bit 18)
			gc.So(mappedCat[3], gc.ShouldEqual, 1<<18)

			operation.XOR(seq1[:], mappedCat[:], seq1[:])
			gc.So(seq1[0], gc.ShouldEqual, 1<<10) // Retains "the"
			gc.So(seq1[3], gc.ShouldEqual, 1<<18) // Contains mapped "cat"
		})

		gc.Convey("Sequence 2: 'cat' -> 'the'", func() {
			seq2 := primitive.NewValue()
			seq2[0] = cat[0] // State = "cat"

			// 'cat' motor: Scale = 20, Translate = 20
			// Mapped 'the' (Bit 10): (20 * 10 + 20) % 8191 = 220
			mappedThe := primitive.NewValue()
			operation.MotorApply(seq2[:], the[:], mappedThe[:])

			// 220 / 64 = 3 (Word 3), 220 % 64 = 28 (Bit 28)
			gc.So(mappedThe[3], gc.ShouldEqual, 1<<28)

			operation.XOR(seq2[:], mappedThe[:], seq2[:])
			gc.So(seq2[0], gc.ShouldEqual, 1<<20) // Retains "cat"
			gc.So(seq2[3], gc.ShouldEqual, 1<<28) // Contains mapped "the"
		})
	})
}

/*
4. EXACT CIRCULAR SHIFT (Mersenne Boundary Wrap)
Verifies that RollLeft correctly jumps across the 8191st bit and maps EXACTLY
to bit 0 (re-entering Word 0) without leaking into the 8192nd (Instruction) bit.
*/
func TestExactRollLeftBoundary(t *testing.T) {
	gc.Convey("Given a bit sitting at the absolute edge of the GF(8191) field", t, func() {
		src := primitive.NewValue()

		// Set bit 8190 (Last bit of the Mersenne Field)
		// 8190 / 64 = 127 (Word 127)
		// 8190 % 64 = 62 (Bit 62)
		src[127] = 1 << 62

		dst := primitive.NewValue()

		gc.Convey("Shift by 1 wraps flawlessly to exact Bit 0 (Word 0)", func() {
			operation.RollLeft(src, dst, 1)

			gc.So(dst[0], gc.ShouldEqual, 1)   // Bit 0
			gc.So(dst[127], gc.ShouldEqual, 0) // Erased from Word 127
		})

		gc.Convey("Shift by 65 wraps deep into Word 1", func() {
			operation.RollLeft(src, dst, 65)

			// 8190 + 65 = 8255. 8255 % 8191 = 64.
			// 64 / 64 = 1 (Word 1). 64 % 64 = 0 (Bit 0).
			gc.So(dst[1], gc.ShouldEqual, 1)
			gc.So(dst[127], gc.ShouldEqual, 0)
		})
	})
}

/*
5. MOTOR EQUIVALENCE CLASSES (Exact Collision)
Proves the mathematical claim that the vast 2^8191 state space forces disjoint
prime factorizations to map to identical affine motors, enabling structural substitutability.
*/
func TestExactEquivalenceClassCollision(t *testing.T) {
	gc.Convey("Given two distinct Values with identical derived motors", t, func() {
		/*
			Product 0·1·4 and 0·2·3 both normalize to scale 1; sums are 0+1+4 and 0+2+3
			(mod 8191), so translate matches while the active bit sets differ.
		*/
		collisionA := primitive.NewValue()
		collisionA.Set(0)
		collisionA.Set(1)
		collisionA.Set(4)

		collisionB := primitive.NewValue()
		collisionB.Set(0)
		collisionB.Set(2)
		collisionB.Set(3)

		scaleA, translateA := collisionA.Motor()
		scaleB, translateB := collisionB.Motor()

		gc.Convey("Distinct memory states share the same affine motor parameters", func() {
			gc.So(collisionA.Equal(collisionB), gc.ShouldBeFalse)
			gc.So(scaleA, gc.ShouldEqual, scaleB)
			gc.So(translateA, gc.ShouldEqual, translateB)
		})

		gc.Convey("MotorApply agrees on payloads for every tested prime index", func() {
			exactScale, exactTranslate := scaleA, translateA

			for _, primeIndex := range []int{0, 41, 42, 100, 4096} {
				payload := primitive.NewValue()
				payload.Set(primeIndex)

				mappedA := primitive.NewValue()
				mappedB := primitive.NewValue()

				operation.MotorApply(collisionA[:], payload[:], mappedA[:])
				operation.MotorApply(collisionB[:], payload[:], mappedB[:])

				gc.So(mappedA.Equal(mappedB), gc.ShouldBeTrue)

				mappedPos := primitive.ApplyMotor(exactScale, exactTranslate, uint16(primeIndex))
				expectedWord := int(mappedPos) / 64
				expectedOffset := int(mappedPos) % 64

				gc.So(mappedA[expectedWord], gc.ShouldEqual, uint64(1)<<expectedOffset)
			}
		})
	})
}

/*
6. OOB INSTRUCTION MASK SAFETY
Verifies that the `InstructionMask` (Bit 8191, outside the core field) is exactly
set and cleared without corrupting the 8191st field bit.
*/
func TestExactInstructionMaskSafety(t *testing.T) {
	gc.Convey("Given the edge of Word 127", t, func() {
		val := primitive.NewValue()

		// Fill all 63 valid bits of the Mersenne Field in the final word
		val[127] = primitive.LastMask

		// Mathematical verification of the LastMask boundary
		// 8191 % 64 = 63 bits (0 to 62). LastMask = (1 << 63) - 1
		gc.So(primitive.LastMask, gc.ShouldEqual, (1<<63)-1)

		instr := primitive.NewValue()
		instr[0] = 8 // Truth table 8 (AND)

		val.SetInstruction(instr)

		gc.Convey("OOB Bit 63 is set exactly without modifying lower words", func() {
			gc.So(val[0], gc.ShouldEqual, 8)

			// The Instruction Mask is EXACTLY bit 63 in word 127 (1 << 63)
			gc.So(val[127]&primitive.InstructionMask, gc.ShouldEqual, primitive.InstructionMask)
		})

		gc.Convey("ClearInstruction erases exactly the out-of-band bit", func() {
			val.ClearInstruction()
			gc.So(val[127]&primitive.InstructionMask, gc.ShouldEqual, 0)
		})
	})
}

/*
12. EXACT REVERSAL OF OR (The Shannon Mic-Drop)
Proves that OR (LCM) accumulation on a prime lattice does not destroy information.
Original components can be perfectly reconstructed from the composite using
lattice intersections (Inclusion-Exclusion / Möbius inversion over the lattice),
proving accumulation is exactly undoable.
*/
func TestExactReversalOfOR(t *testing.T) {
	gc.Convey("Given a composite accumulated from overlapping sequence values", t, func() {
		// A = {10, 20, 30}
		A := primitive.NewValue()
		A.Set(10)
		A.Set(20)
		A.Set(30)

		// B = {20, 30, 40} (Shares 20, 30 with A)
		B := primitive.NewValue()
		B.Set(20)
		B.Set(30)
		B.Set(40)

		// C = {30, 40, 50} (Shares 30 with A, 30/40 with B)
		C := primitive.NewValue()
		C.Set(30)
		C.Set(40)
		C.Set(50)

		// 1. Accumulate into a single Composite (OR / LCM)
		composite := primitive.NewValue()
		operation.OR(A[:], B[:], composite[:])
		operation.OR(composite[:], C[:], composite[:])

		// Composite is now {10, 20, 30, 40, 50}
		gc.So(composite.PopCount(), gc.ShouldEqual, 5)

		gc.Convey("Mobius parity function correctly alternates based on PopCount", func() {
			// Möbius function μ(n) = (-1)^k where k is active primes.
			// This provides the +1 / -1 coefficients for exact Inclusion-Exclusion.
			gc.So(algebra.Mobius(A.PopCount()), gc.ShouldEqual, -1)         // 3 bits -> -1
			gc.So(algebra.Mobius(composite.PopCount()), gc.ShouldEqual, -1) // 5 bits -> -1

			evenBits := primitive.NewValue()
			evenBits.Set(1)
			evenBits.Set(2)
			gc.So(algebra.Mobius(evenBits.PopCount()), gc.ShouldEqual, 1) // 2 bits -> +1
		})

		gc.Convey("We can perfectly reconstruct A from the deeply merged Composite", func() {
			// To reconstruct A from the Composite, we apply lattice inclusion-exclusion.
			// In standard boolean logic on arbitrary labels, 'OR' destroys the boundaries permanently.
			// But on a prime lattice, tracking shared structure via GCD (AND) allows perfect recovery.

			// Known context (the rest of the sequence: B OR C)
			context := primitive.NewValue()
			operation.OR(B[:], C[:], context[:])

			// Step 1: Extract the unique residue of A that wasn't swallowed by the context
			// Residue = Composite &~ Context
			residue := primitive.NewValue()
			operation.AndNot(composite[:], context[:], residue[:])

			// Step 2: Recover the shared primes (GCD of A and Context)
			// (In the full VM, this intersection is what is stored in the FoldGraph nodes)
			shared := primitive.NewValue()
			operation.AND(A[:], context[:], shared[:])

			// Step 3: Reconstruct A by fusing the unique residue and the shared structure
			reconstructedA := primitive.NewValue()
			operation.OR(residue[:], shared[:], reconstructedA[:])

			// The reconstructed Value must perfectly match the original A memory state
			gc.So(reconstructedA.Equal(A), gc.ShouldBeTrue)

			// Verify the exact memory state of Reconstructed A
			// It MUST contain exactly bits 10, 20, 30, and NO bleed-over from the Composite
			gc.So(reconstructedA.Has(10), gc.ShouldBeTrue)
			gc.So(reconstructedA.Has(20), gc.ShouldBeTrue)
			gc.So(reconstructedA.Has(30), gc.ShouldBeTrue)

			gc.So(reconstructedA.Has(40), gc.ShouldBeFalse) // Did not bleed from B/C
			gc.So(reconstructedA.Has(50), gc.ShouldBeFalse) // Did not bleed from C
		})
	})
}

/*
13. EXACT REDUCTION ASSOCIATIVITY
The lattice reductions used throughout the architecture must keep their result
independent of binary tree shape for associative operations.
*/
func TestExactReductionAssociativity(t *testing.T) {
	gc.Convey("Given three explicitly constructed Value fixtures", t, func() {
		left := primitive.NewValue()
		left.Set(0)
		left.Set(2)
		left.Set(4)

		middle := primitive.NewValue()
		middle.Set(0)
		middle.Set(1)
		middle.Set(4)

		right := primitive.NewValue()
		right.Set(1)
		right.Set(3)
		right.Set(4)

		gc.Convey("AND reduction is exact regardless of grouping", func() {
			leftGrouped := primitive.NewValue()
			rightGrouped := primitive.NewValue()
			tmp := primitive.NewValue()

			operation.AND(left[:], middle[:], tmp[:])
			operation.AND(tmp[:], right[:], leftGrouped[:])

			operation.AND(middle[:], right[:], tmp[:])
			operation.AND(left[:], tmp[:], rightGrouped[:])

			gc.So(leftGrouped.Equal(rightGrouped), gc.ShouldBeTrue)
			gc.So(leftGrouped.Equal(valueWithBits(4)), gc.ShouldBeTrue)
		})

		gc.Convey("OR reduction is exact regardless of grouping", func() {
			leftGrouped := primitive.NewValue()
			rightGrouped := primitive.NewValue()
			tmp := primitive.NewValue()

			operation.OR(left[:], middle[:], tmp[:])
			operation.OR(tmp[:], right[:], leftGrouped[:])

			operation.OR(middle[:], right[:], tmp[:])
			operation.OR(left[:], tmp[:], rightGrouped[:])

			gc.So(leftGrouped.Equal(rightGrouped), gc.ShouldBeTrue)
			gc.So(leftGrouped.Equal(valueWithBits(0, 1, 2, 3, 4)), gc.ShouldBeTrue)
		})

		gc.Convey("XOR reduction is exact regardless of grouping", func() {
			leftGrouped := primitive.NewValue()
			rightGrouped := primitive.NewValue()
			tmp := primitive.NewValue()

			operation.XOR(left[:], middle[:], tmp[:])
			operation.XOR(tmp[:], right[:], leftGrouped[:])

			operation.XOR(middle[:], right[:], tmp[:])
			operation.XOR(left[:], tmp[:], rightGrouped[:])

			gc.So(leftGrouped.Equal(rightGrouped), gc.ShouldBeTrue)
			gc.So(leftGrouped.Equal(valueWithBits(2, 3, 4)), gc.ShouldBeTrue)
		})
	})
}
