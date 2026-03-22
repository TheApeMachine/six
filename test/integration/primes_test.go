package integration

import (
	"fmt"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
	"github.com/theapemachine/six/pkg/transport"
)

/*
frameSource emits exact 1024-byte Value frames for pipeline integration tests.
*/
type frameSource struct {
	frames [][]byte
	index  int
}

/*
Read emits one full frame per call to preserve Stream framing semantics.
*/
func (source *frameSource) Read(p []byte) (int, error) {
	if source.index >= len(source.frames) {
		return 0, io.EOF
	}

	frame := source.frames[source.index]
	source.index++

	return copy(p, frame), nil
}

/*
Write satisfies io.ReadWriter for transport.NewPipeline source stage.
*/
func (source *frameSource) Write(p []byte) (int, error) {
	return len(p), nil
}

/*
valueBytes serializes a Value into a fixed-size byte slice.
*/
func valueBytes(value *primitive.Value) []byte {
	buffer := make([]byte, primitive.ByteSize)
	_, _ = value.Read(buffer)
	return buffer
}

/*
runInBandOp executes one in-band binary operation through transport pipeline elements.
*/
func runInBandOp(instr, left, right *primitive.Value) (*primitive.Value, error) {
	stream := transport.NewStream()
	source := &frameSource{
		frames: [][]byte{
			valueBytes(instr),
			valueBytes(left),
			valueBytes(right),
		},
	}
	pipeline := transport.NewPipeline(source, stream)
	resultBuffer := make([]byte, primitive.ByteSize)

	n, err := pipeline.Read(resultBuffer)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n != primitive.ByteSize {
		return nil, io.ErrUnexpectedEOF
	}

	result := primitive.NewValue()

	if _, err := result.Write(resultBuffer); err != nil {
		return nil, err
	}

	return result, nil
}

func TestInBandGCDThroughStream(t *testing.T) {
	Convey("Given a Stream receiving an AND instruction followed by two operand Values", t, func() {
		a := primitive.BaseValue(30)
		b := primitive.BaseValue(70)
		instrA := primitive.NewValue()
		instrA.SetInstruction(primitive.InstrAND)

		out, err := runInBandOp(instrA, a, b)
		So(err, ShouldBeNil)
		So(out.Equal(primitive.BaseValue(10)), ShouldBeTrue)
	})
}

func TestPrimeLatticeClaimsThroughTransportPipeline(t *testing.T) {
	Convey("Given in-band instructions running through transport pipeline", t, func() {
		type operationCase struct {
			name     string
			op       *primitive.Value
			left     byte
			right    byte
			expected byte
		}

		cases := []operationCase{
			{
				name:     "AND should match GCD for 30 and 70",
				op:       primitive.InstrAND,
				left:     30,
				right:    70,
				expected: 10,
			},
			{
				name:     "OR should match LCM for 30 and 70",
				op:       primitive.InstrOR,
				left:     30,
				right:    70,
				expected: 210,
			},
			{
				name:     "XOR should match LCM/GCD for 30 and 70",
				op:       primitive.InstrXOR,
				left:     30,
				right:    70,
				expected: 21,
			},
			{
				name:     "AndNot should match multiplicative residue for 110 and 30",
				op:       primitive.InstrAndNot,
				left:     110,
				right:    30,
				expected: 11,
			},
			{
				name:     "Clear should keep the left operand",
				op:       primitive.InstrClear,
				left:     30,
				right:    70,
				expected: 30,
			},
		}

		for _, testCase := range cases {
			testCase := testCase

			Convey(testCase.name, func() {
				instruction := primitive.NewValue()
				instruction.SetInstruction(testCase.op)

				out, err := runInBandOp(
					instruction,
					primitive.BaseValue(testCase.left),
					primitive.BaseValue(testCase.right),
				)
				So(err, ShouldBeNil)
				So(out.Equal(primitive.BaseValue(testCase.expected)), ShouldBeTrue)
			})
		}

		Convey("Divisibility should satisfy (A AND B) == A", func() {
			instruction := primitive.NewValue()
			instruction.SetInstruction(primitive.InstrAND)
			divisor := primitive.BaseValue(10)
			multiple := primitive.BaseValue(210)

			out, err := runInBandOp(instruction, divisor, multiple)
			So(err, ShouldBeNil)
			So(out.Equal(divisor), ShouldBeTrue)
		})

		Convey("Coprime values should satisfy (A AND B) == 0", func() {
			instruction := primitive.NewValue()
			instruction.SetInstruction(primitive.InstrAND)

			out, err := runInBandOp(
				instruction,
				primitive.BaseValue(14),
				primitive.BaseValue(15),
			)
			So(err, ShouldBeNil)
			So(out.IsZero(), ShouldBeTrue)
		})
	})
}

/*
instructionValueAt builds an in-band instruction frame from a truth-table row.
*/
func instructionValueAt(bit int) *primitive.Value {
	instruction := primitive.NewValue()
	instruction.Set(bit)

	flagged := primitive.NewValue()
	flagged.SetInstruction(instruction)

	return flagged
}

/*
valueWithBits constructs an exact Value fixture from active core bit positions.
*/
func valueWithBits(bits ...int) *primitive.Value {
	value := primitive.NewValue()

	for _, bit := range bits {
		value.Set(bit)
	}

	return value
}

/*
fullCoreValue constructs the all-ones Value over the 8191-bit core field.
*/
func fullCoreValue() *primitive.Value {
	value := primitive.NewValue()

	for bit := range primitive.CoreBits {
		value.Set(bit)
	}

	return value
}

/*
fullCoreValueExcept constructs the all-ones core Value minus the excluded bits.
*/
func fullCoreValueExcept(excluded ...int) *primitive.Value {
	value := fullCoreValue()

	for _, bit := range excluded {
		value[bit/64] &^= 1 << (bit % 64)
	}

	value.Clamp()

	return value
}

/*
CLAIM: THE VALUE ISA COVERS THE BOOLEAN TRUTH TABLE
The in-band transport path should honor the same truth-table row semantics
documented in NEXTEST.md rather than exposing only a hand-picked subset.
*/
func TestBooleanTruthTableRowsThroughTransportPipeline(t *testing.T) {
	Convey("Given overlapping operand Values and truth-table row instructions", t, func() {
		left := valueWithBits(0, 1)
		right := valueWithBits(1, 2)

		cases := []struct {
			name        string
			instruction int
			expected    *primitive.Value
		}{
			{
				name:        "Contradiction should clear every core bit",
				instruction: 0,
				expected:    primitive.NewValue(),
			},
			{
				name:        "NOR should invert the union",
				instruction: 1,
				expected:    fullCoreValueExcept(0, 1, 2),
			},
			{
				name:        "Converse nonimplication should produce B and not A",
				instruction: 2,
				expected:    valueWithBits(2),
			},
			{
				name:        "NOT should invert the first operand",
				instruction: 3,
				expected:    fullCoreValueExcept(0, 1),
			},
			{
				name:        "Material nonimplication should produce A and not B",
				instruction: 4,
				expected:    valueWithBits(0),
			},
			{
				name:        "Not-second should invert the second operand",
				instruction: 5,
				expected:    fullCoreValueExcept(1, 2),
			},
			{
				name:        "XOR should keep only the non-shared structure",
				instruction: 6,
				expected:    valueWithBits(0, 2),
			},
			{
				name:        "NAND should invert the intersection",
				instruction: 7,
				expected:    fullCoreValueExcept(1),
			},
			{
				name:        "AND should keep only the shared factor",
				instruction: 8,
				expected:    valueWithBits(1),
			},
			{
				name:        "XNOR should invert the symmetric difference",
				instruction: 9,
				expected:    fullCoreValueExcept(0, 2),
			},
			{
				name:        "Identity B should return the second operand",
				instruction: 10,
				expected:    valueWithBits(1, 2),
			},
			{
				name:        "Material conditional should produce not A or B",
				instruction: 11,
				expected:    fullCoreValueExcept(0),
			},
			{
				name:        "Identity A should return the first operand",
				instruction: 12,
				expected:    valueWithBits(0, 1),
			},
			{
				name:        "Converse implication should produce A or not B",
				instruction: 13,
				expected:    fullCoreValueExcept(2),
			},
			{
				name:        "OR should produce the union",
				instruction: 14,
				expected:    valueWithBits(0, 1, 2),
			},
			{
				name:        "Tautology should set every core bit",
				instruction: 15,
				expected:    fullCoreValue(),
			},
		}

		for _, testCase := range cases {
			testCase := testCase

			Convey(testCase.name, func() {
				out, err := runInBandOp(
					instructionValueAt(testCase.instruction),
					left,
					right,
				)
				So(err, ShouldBeNil)
				So(out.Equal(testCase.expected), ShouldBeTrue)
			})
		}
	})
}

func TestInstructionBoundaryIntegrityThroughTransport(t *testing.T) {
	Convey("Given an in-band instruction through transport", t, func() {
		instruction := primitive.NewValue()
		instruction.SetInstruction(primitive.InstrAND)

		So(instruction.IsInstruction(), ShouldBeTrue)

		out, err := runInBandOp(
			instruction,
			primitive.BaseValue(30),
			primitive.BaseValue(70),
		)
		So(err, ShouldBeNil)
		So(out.IsInstruction(), ShouldBeFalse)
		So(out.Equal(primitive.BaseValue(10)), ShouldBeTrue)
	})
}

func TestCoreBitBoundaryClamp(t *testing.T) {
	Convey("Given a raw 1024-byte payload with every bit set", t, func() {
		raw := make([]byte, primitive.ByteSize)

		for i := range raw {
			raw[i] = 0xFF
		}

		value := primitive.NewValue()
		_, err := value.Write(raw)
		So(err, ShouldBeNil)

		So(value.IsInstruction(), ShouldBeFalse)
		So(value.Has(primitive.CoreBits-1), ShouldBeTrue)
		So(value.PopCount(), ShouldEqual, primitive.CoreBits)
	})
}

/*
7. EXACT PRIME FACTORIZATION INGESTION
Proves that standard byte values project natively into the prime lattice
without tokenization, and that their prime constituents are mathematically correct.
*/
func TestExactPrimeFactoringIngestion(t *testing.T) {
	gc.Convey("Given raw bytes ingested into the system", t, func() {

		gc.Convey("Byte 10 (2 * 5) projects exactly to prime indices 0 and 2", func() {
			val := primitive.BaseValue(10)

			// Primes[0] = 2, Primes[1] = 3, Primes[2] = 5
			gc.So(val.Has(0), gc.ShouldBeTrue)  // Factor 2
			gc.So(val.Has(2), gc.ShouldBeTrue)  // Factor 5
			gc.So(val.Has(1), gc.ShouldBeFalse) // Not divisible by 3

			// Exactly two bits should be set
			gc.So(val.PopCount(), gc.ShouldEqual, 2)
		})

		gc.Convey("Byte 42 (2 * 3 * 7) projects exactly to prime indices 0, 1, and 3", func() {
			val := primitive.BaseValue(42)

			// Primes[0] = 2, Primes[1] = 3, Primes[2] = 5, Primes[3] = 7
			gc.So(val.Has(0), gc.ShouldBeTrue)
			gc.So(val.Has(1), gc.ShouldBeTrue)
			gc.So(val.Has(3), gc.ShouldBeTrue)
			gc.So(val.Has(2), gc.ShouldBeFalse) // Not divisible by 5
		})

		gc.Convey("Square-free projection ignores exponents: Byte 12 (2^2 * 3) -> 2 * 3", func() {
			val := primitive.BaseValue(12)

			// Should only flag presence of 2 and 3
			gc.So(val.Has(0), gc.ShouldBeTrue)
			gc.So(val.Has(1), gc.ShouldBeTrue)
			gc.So(val.PopCount(), gc.ShouldEqual, 2) // No redundant bits
		})
	})
}

/*
independentPrimeBits projects a byte into prime bit positions without using BaseValue.
*/
func independentPrimeBits(b byte) []int {
	n := uint32(b)

	if n < 2 {
		return nil
	}

	var bits []int

	for divisor := uint32(2); divisor*divisor <= n; divisor++ {
		if n%divisor != 0 {
			continue
		}

		if position, ok := primitive.PrimeIndex[divisor]; ok {
			bits = append(bits, position)
		}

		for n%divisor == 0 {
			n /= divisor
		}
	}

	if n > 1 {
		if position, ok := primitive.PrimeIndex[n]; ok {
			bits = append(bits, position)
		}
	}

	return bits
}

/*
valueFromBits builds a Value from a bit-position slice.
*/
func valueFromBits(bits []int) *primitive.Value {
	value := primitive.NewValue()

	for _, bit := range bits {
		value.Set(bit)
	}

	return value
}

/*
bitSet returns a compact lookup table for a bit-position slice.
*/
func bitSet(bits []int) map[int]struct{} {
	out := make(map[int]struct{}, len(bits))

	for _, bit := range bits {
		out[bit] = struct{}{}
	}

	return out
}

/*
CLAIM: BYTE INGESTION IS EXACT FOR THE FULL BYTE FAMILY
Every byte should project to the exact square-free set of its prime divisors.
*/
func TestExactPrimeProjectionAcrossFullByteFamily(t *testing.T) {
	gc.Convey("Given every byte value from 0 through 255", t, func() {
		for byteValue := range 256 {
			byteValue := byteValue

			gc.Convey(
				fmt.Sprintf("byte %d should project to its exact prime divisor set", byteValue),
				func() {
					expected := valueFromBits(independentPrimeBits(byte(byteValue)))
					actual := primitive.BaseValue(byte(byteValue))

					gc.So(actual.Equal(expected), gc.ShouldBeTrue)
				},
			)
		}
	})
}

/*
CLAIM: LATTICE IDENTITIES HOLD ACROSS THE BYTE PROJECTION SURFACE
For the full byte family, AND/OR/XOR/AndNot should match exact set arithmetic
over the independently factored prime bit positions.
*/
func TestExactLatticeIdentitiesAcrossByteFamilyPairs(t *testing.T) {
	gc.Convey("Given independent prime-factor projections for all byte pairs", t, func() {
		for leftByte := range 256 {
			leftBits := independentPrimeBits(byte(leftByte))
			leftSet := bitSet(leftBits)
			leftValue := primitive.BaseValue(byte(leftByte))

			for rightByte := range 256 {
				rightBits := independentPrimeBits(byte(rightByte))
				rightSet := bitSet(rightBits)
				rightValue := primitive.BaseValue(byte(rightByte))

				expectedAnd := primitive.NewValue()
				expectedOr := primitive.NewValue()
				expectedXor := primitive.NewValue()
				expectedAndNot := primitive.NewValue()

				for _, bit := range leftBits {
					if _, ok := rightSet[bit]; ok {
						expectedAnd.Set(bit)
					}

					expectedOr.Set(bit)

					if _, ok := rightSet[bit]; !ok {
						expectedXor.Set(bit)
						expectedAndNot.Set(bit)
					}
				}

				for _, bit := range rightBits {
					expectedOr.Set(bit)

					if _, ok := leftSet[bit]; !ok {
						expectedXor.Set(bit)
					}
				}

				actualAnd := primitive.NewValue()
				actualOr := primitive.NewValue()
				actualXor := primitive.NewValue()
				actualAndNot := primitive.NewValue()

				operation.AND(leftValue[:], rightValue[:], actualAnd[:])
				operation.OR(leftValue[:], rightValue[:], actualOr[:])
				operation.XOR(leftValue[:], rightValue[:], actualXor[:])
				operation.AndNot(leftValue[:], rightValue[:], actualAndNot[:])

				gc.So(actualAnd.Equal(expectedAnd), gc.ShouldBeTrue)
				gc.So(actualOr.Equal(expectedOr), gc.ShouldBeTrue)
				gc.So(actualXor.Equal(expectedXor), gc.ShouldBeTrue)
				gc.So(actualAndNot.Equal(expectedAndNot), gc.ShouldBeTrue)
			}
		}
	})
}

/*
8. EXACT LATTICE DIVISIBILITY & COPRIMALITY
Proves the claim that "Divisibility is subset" and "Coprimality is disjointness".
*/
func TestExactLatticeNumberTheory(t *testing.T) {
	gc.Convey("Given Values mapping to the divisibility lattice", t, func() {
		// A = 6 (2 * 3) -> Bits 0, 1
		A := primitive.BaseValue(6)

		// B = 30 (2 * 3 * 5) -> Bits 0, 1, 2
		B := primitive.BaseValue(30)

		// C = 77 (7 * 11) -> Bits 3, 4
		C := primitive.BaseValue(77)

		gc.Convey("Divisibility strictly equates to Subset (A AND B == A)", func() {
			intersect := primitive.NewValue()
			operation.AND(A[:], B[:], intersect[:])

			// If A divides B, A & B must be mathematically identical to A
			gc.So(intersect.Equal(A), gc.ShouldBeTrue)
		})

		gc.Convey("Coprimality strictly equates to Disjointness (A AND C == 0)", func() {
			intersect := primitive.NewValue()
			operation.AND(A[:], C[:], intersect[:])

			gc.So(intersect.IsZero(), gc.ShouldBeTrue)

			// In coprime relationships, LCM (OR) and Distance (XOR) are identical
			lcm := primitive.NewValue()
			dist := primitive.NewValue()
			operation.OR(A[:], C[:], lcm[:])
			operation.XOR(A[:], C[:], dist[:])

			gc.So(lcm.Equal(dist), gc.ShouldBeTrue)
		})
	})
}

/*
9. EXACT AFFINE COMPOSITION LAW
Proves that composing motors analytically yields the exact same target as
applying them sequentially. f2(f1(p)) == f_composed(p)
*/
func TestExactAffineCompositionLaw(t *testing.T) {
	gc.Convey("Given two affine transformations", t, func() {
		// Motor 1: f1(p) = 5p + 10
		s1, t1 := uint16(5), uint16(10)

		// Motor 2: f2(p) = 3p + 7
		s2, t2 := uint16(3), uint16(7)

		// Composed: f2(f1(p)) = 3*(5p + 10) + 7 = 15p + 37
		sComp, tComp := primitive.ComposeMotor(s1, t1, s2, t2)

		gc.Convey("Mathematical composition matches exact formula", func() {
			gc.So(sComp, gc.ShouldEqual, 15)
			gc.So(tComp, gc.ShouldEqual, 37)
		})

		gc.Convey("Sequential application matches composed operator for all test positions", func() {
			positions := []uint16{0, 1, 42, 1024, 4095, 8190}

			for _, position := range positions {
				intermediate := primitive.ApplyMotor(s1, t1, position)
				sequential := primitive.ApplyMotor(s2, t2, intermediate)
				composed := primitive.ApplyMotor(sComp, tComp, position)

				gc.So(sequential, gc.ShouldEqual, composed)
			}
		})
	})
}

/*
10. EXACT BVP CANTILEVER / RESIDUAL TRACKING
Proves that Material Nonimplication isolating the "unresolved request" physically
forces a brand new rotational motor to steer navigation.
*/
func TestExactCantileverResidualTracking(t *testing.T) {
	gc.Convey("Given a prompt asking for two distinct structural components", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(10) // e.g. "Sort"
		prompt.Set(20) // e.g. "Write"

		// Initial Target Vector
		initScale, initTranslate := prompt.Motor()

		// Output matches the first component ("Sort")
		output := primitive.NewValue()
		output.Set(10)

		gc.Convey("The residual naturally shifts the navigation target", func() {
			residual := primitive.NewValue()
			operation.AndNot(prompt[:], output[:], residual[:])

			// The residual must EXACTLY contain only the missing component
			gc.So(residual.Has(20), gc.ShouldBeTrue)
			gc.So(residual.Has(10), gc.ShouldBeFalse)

			// The motor MUST shift, breaking away from the original trajectory
			resScale, resTranslate := residual.Motor()
			gc.So(resScale, gc.ShouldNotEqual, initScale)
			gc.So(resTranslate, gc.ShouldNotEqual, initTranslate)

			// Verify it shifted exactly to the "Write" component's native frequency
			expectedTarget := primitive.NewValue()
			expectedTarget.Set(20)
			expS, expT := expectedTarget.Motor()

			gc.So(resScale, gc.ShouldEqual, expS)
			gc.So(resTranslate, gc.ShouldEqual, expT)
		})
	})
}

/*
11. EXACT IN-BAND DATAWAVE SPLICING
Proves that the transport layer reads in-band control frames to hot-swap
the pipeline's behavior in real-time, fulfilling the "Data is Operation" claim.
*/
func TestInBandControlFrameStreamReconfiguration(t *testing.T) {
	gc.Convey("Given a generic asynchronous data stream", t, func() {
		stream := transport.NewStream()
		defer stream.Close()

		gc.Convey("An out-of-band Instruction Mask rewires the Stream pipeline inline", func() {
			// Instruction frame: OR on the lattice, flagged for transport (last byte 0x80).
			instrPacket := primitive.NewValue()
			instrPacket.SetInstruction(primitive.InstrOR)
			packet := valueBytes(instrPacket)

			// Send it into the pipeline
			n, err := stream.Write(packet)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)

			// If the architecture holds, the stream should have consumed the packet
			// without buffering it as readable data, and internally transitioned
			// its operation loop to an active Bitwise Accumulator.

			// Because a Bitwise accumulator requires 3 writes (Instruction + 2 Operands)
			// before it yields a read, reading right now must block with 0 bytes (err nil).
			readBuf := make([]byte, primitive.ByteSize)
			rn, rErr := stream.Read(readBuf)

			gc.So(rn, gc.ShouldEqual, 0)
			gc.So(rErr, gc.ShouldBeNil)

			// Now we provide the two data operands to fulfill the accumulator.
			operand1 := make([]byte, primitive.ByteSize)
			operand2 := make([]byte, primitive.ByteSize)

			// Set deterministic bits to verify the operation triggers.
			// Operand 1 gets 5, Operand 2 gets 10.
			operand1[0] = 5
			operand2[0] = 10

			_, err1 := stream.Write(operand1)
			gc.So(err1, gc.ShouldBeNil)

			_, err2 := stream.Write(operand2)
			gc.So(err2, gc.ShouldBeNil)

			// The 3rd write completed the Bitwise ring. It should now yield a computed Read.
			rn, rErr = stream.Read(readBuf)

			// Based on `stream.go`, receiving an instruction defaults it to operation.OR.
			// 5 | 10 = 15. The exact math must execute transparently inside the stream.
			gc.So(rErr, gc.ShouldBeNil)
			gc.So(rn, gc.ShouldEqual, primitive.ByteSize)
			gc.So(readBuf[0], gc.ShouldEqual, 15)
		})
	})
}
