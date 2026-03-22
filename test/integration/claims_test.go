package integration

import (
	"fmt"
	"strconv"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

/*
CLAIM: XOR/XNOR INVERTIBILITY
XOR is the only binary boolean operation that is perfectly invertible:
XOR(XOR(A, B), B) == A. This makes it the correct accumulation operator
for sequence memory. AND and OR are provably NOT invertible.
*/
func TestXORInvertibility(t *testing.T) {
	gc.Convey("Given two real-data Values projected from bytes", t, func() {
		A := primitive.BaseValue(210)
		B := primitive.BaseValue(77)

		gc.Convey("XOR(A,B) XOR B recovers A exactly", func() {
			accumulated := primitive.NewValue()
			operation.XOR(A[:], B[:], accumulated[:])

			recovered := primitive.NewValue()
			operation.XOR(accumulated[:], B[:], recovered[:])

			gc.So(recovered.Equal(A), gc.ShouldBeTrue)
		})

		gc.Convey("XOR(A,B) XOR A recovers B exactly", func() {
			accumulated := primitive.NewValue()
			operation.XOR(A[:], B[:], accumulated[:])

			recovered := primitive.NewValue()
			operation.XOR(accumulated[:], A[:], recovered[:])

			gc.So(recovered.Equal(B), gc.ShouldBeTrue)
		})

		gc.Convey("XNOR(XNOR(A,B), B) recovers A exactly", func() {
			accumulated := primitive.NewValue()
			operation.XNOR(A[:], B[:], accumulated[:])
			accumulated.Clamp()

			recovered := primitive.NewValue()
			operation.XNOR(accumulated[:], B[:], recovered[:])
			recovered.Clamp()

			gc.So(recovered.Equal(A), gc.ShouldBeTrue)
		})

		gc.Convey("AND is NOT invertible: AND(A,B) AND B != A", func() {
			accumulated := primitive.NewValue()
			operation.AND(A[:], B[:], accumulated[:])

			roundTrip := primitive.NewValue()
			operation.AND(accumulated[:], B[:], roundTrip[:])

			gc.So(roundTrip.Equal(A), gc.ShouldBeFalse)
		})

		gc.Convey("OR is NOT invertible: OR(A,B) AND ~B != A when overlap exists", func() {
			C := primitive.BaseValue(30)
			D := primitive.BaseValue(70)

			merged := primitive.NewValue()
			operation.OR(C[:], D[:], merged[:])

			notD := primitive.NewValue()
			operation.NOT(D[:], nil, notD[:])
			notD.Clamp()

			attempt := primitive.NewValue()
			operation.AND(merged[:], notD[:], attempt[:])

			gc.So(attempt.Equal(C), gc.ShouldBeFalse)
		})
	})
}

/*
CLAIM: MULTI-STEP SEQUENCE ACCUMULATION DIVERGENCE
When tokens are consumed sequentially via XOR with motor-mapped values,
the accumulated state generally changes the motor; for many byte sequences
the motor updates every step. Different orderings of the same tokens produce
distinct final states in the cases exercised below.
*/
func TestMultiStepSequenceAccumulationDivergence(t *testing.T) {
	gc.Convey("Given a 4-token sequence accumulated via motor-mapped XOR", t, func() {
		tokens := []byte{42, 77, 210, 105}
		values := make([]*primitive.Value, len(tokens))

		for i, tok := range tokens {
			values[i] = primitive.BaseValue(tok)
		}

		gc.Convey("The motor changes at every accumulation step", func() {
			state := primitive.NewValue()
			*state = *values[0]
			prevScale, prevTranslate := state.Motor()

			motorChanged := 0

			for step := 1; step < len(values); step++ {
				mapped := primitive.NewValue()
				operation.MotorApply(state[:], values[step][:], mapped[:])

				newState := primitive.NewValue()
				operation.XOR(state[:], mapped[:], newState[:])
				state = newState

				scale, translate := state.Motor()

				if scale != prevScale || translate != prevTranslate {
					motorChanged++
				}

				prevScale = scale
				prevTranslate = translate
			}

			gc.So(motorChanged, gc.ShouldEqual, len(values)-1)
		})

		gc.Convey("Reversed token order produces a distinct final state", func() {
			forward := accumulateSequence(values)
			reversed := make([]*primitive.Value, len(values))

			for i, v := range values {
				reversed[len(values)-1-i] = v
			}

			backward := accumulateSequence(reversed)

			gc.So(forward.Equal(backward), gc.ShouldBeFalse)
		})

		gc.Convey("Even single-swap yields a distinct final state", func() {
			swapped := make([]*primitive.Value, len(values))
			copy(swapped, values)
			swapped[1], swapped[2] = swapped[2], swapped[1]

			original := accumulateSequence(values)
			modified := accumulateSequence(swapped)

			gc.So(original.Equal(modified), gc.ShouldBeFalse)
		})
	})
}

/*
accumulateSequence runs XOR(state, motorApply(state, next)) for each token.
*/
func accumulateSequence(values []*primitive.Value) *primitive.Value {
	state := primitive.NewValue()
	*state = *values[0]

	for step := 1; step < len(values); step++ {
		mapped := primitive.NewValue()
		operation.MotorApply(state[:], values[step][:], mapped[:])

		newState := primitive.NewValue()
		operation.XOR(state[:], mapped[:], newState[:])
		state = newState
	}

	return state
}

/*
gcdPopCount returns PopCount(context AND BaseValue(b)).
*/
func gcdPopCount(context *primitive.Value, b byte) int {
	candidate := primitive.BaseValue(b)
	gcd := primitive.NewValue()
	operation.AND(context[:], candidate[:], gcd[:])

	return gcd.PopCount()
}

/*
conversationContextFactors210 builds OR(BaseValue(30), BaseValue(70), BaseValue(210)).
*/
func conversationContextFactors210() *primitive.Value {
	context := primitive.NewValue()

	operation.OR(
		primitive.BaseValue(30)[:],
		primitive.BaseValue(70)[:],
		context[:],
	)
	operation.OR(context[:], primitive.BaseValue(210)[:], context[:])

	return context
}

/*
CLAIM: CONTEXTUAL BIAS VIA GCD
An accumulated conversation state biases selection of the next token by
having higher GCD (AND) overlap with structurally relevant candidates
than with irrelevant ones. The conversation acts as a filter.
*/
func TestContextualBiasViaGCD(t *testing.T) {
	gc.Convey("Given a conversation context about factors of 210", t, func() {
		context := conversationContextFactors210()

		relevant := primitive.BaseValue(42)
		irrelevant := primitive.BaseValue(77)

		gc.Convey("GCD with relevant candidate exceeds GCD with irrelevant", func() {
			gcdRelevant := primitive.NewValue()
			operation.AND(context[:], relevant[:], gcdRelevant[:])

			gcdIrrelevant := primitive.NewValue()
			operation.AND(context[:], irrelevant[:], gcdIrrelevant[:])

			gc.So(gcdRelevant.PopCount(), gc.ShouldBeGreaterThan, gcdIrrelevant.PopCount())
		})

		gc.Convey("Fully coprime candidate has zero GCD with context", func() {
			coprime := primitive.NewValue()
			coprime.Set(100)
			coprime.Set(200)

			gcd := primitive.NewValue()
			operation.AND(context[:], coprime[:], gcd[:])

			gc.So(gcd.IsZero(), gc.ShouldBeTrue)
		})

		gc.Convey("Ranking by PopCount(AND) picks first menu byte among those tied for max overlap", func() {
			candidates := []byte{42, 77, 105, 15, 6}
			menuMax := 0

			for _, b := range candidates {
				overlap := gcdPopCount(context, b)

				if overlap > menuMax {
					menuMax = overlap
				}
			}

			bestOverlap := 0
			bestByte := byte(0)

			for _, b := range candidates {
				overlap := gcdPopCount(context, b)

				if overlap > bestOverlap {
					bestOverlap = overlap
					bestByte = b
				}
			}

			gc.So(bestOverlap, gc.ShouldEqual, menuMax)
			gc.So(menuMax, gc.ShouldEqual, 3)

			var tied []byte

			for _, b := range candidates {
				if gcdPopCount(context, b) == menuMax {
					tied = append(tied, b)
				}
			}

			gc.So(tied, gc.ShouldResemble, []byte{42, 105})
			gc.So(bestByte, gc.ShouldEqual, tied[0])
		})

		gc.Convey("Full byte scan finds maximum overlap 4 at 210 for this context", func() {
			globalMax := 0

			for b := 2; b < 256; b++ {
				overlap := gcdPopCount(context, byte(b))

				if overlap > globalMax {
					globalMax = overlap
				}
			}

			gc.So(globalMax, gc.ShouldEqual, 4)
			gc.So(gcdPopCount(context, 210), gc.ShouldEqual, 4)
		})
	})
}

/*
TestXORInvertibilityAcrossBytePairs repeats XOR round-trip identities on several
independent byte projections (not a single cherry-picked pair).
*/
func TestXORInvertibilityAcrossBytePairs(t *testing.T) {
	gc.Convey("Given independent BaseValue pairs", t, func() {
		pairs := []struct {
			a byte
			b byte
		}{
			{210, 77},
			{30, 70},
			{255, 1},
			{143, 187},
			{42, 105},
			{11, 13},
		}

		for _, pair := range pairs {
			pair := pair

			gc.Convey(fmt.Sprintf("pair %d,%d XOR round-trip", pair.a, pair.b), func() {
				A := primitive.BaseValue(pair.a)
				B := primitive.BaseValue(pair.b)

				accumulated := primitive.NewValue()
				operation.XOR(A[:], B[:], accumulated[:])

				recoveredA := primitive.NewValue()
				operation.XOR(accumulated[:], B[:], recoveredA[:])

				recoveredB := primitive.NewValue()
				operation.XOR(accumulated[:], A[:], recoveredB[:])

				gc.So(recoveredA.Equal(A), gc.ShouldBeTrue)
				gc.So(recoveredB.Equal(B), gc.ShouldBeTrue)
			})
		}
	})
}

/*
TestMultiStepSequenceNonCommutativityTable repeats ordering and motor-step claims
across byte-token sequences where XOR accumulation actually perturbs Motor each step.
*/
func TestMultiStepSequenceNonCommutativityTable(t *testing.T) {
	gc.Convey("Given token sequences that change Motor at every XOR step", t, func() {
		sequences := [][]byte{
			{42, 77, 210, 105},
			{255, 254, 253, 252},
			{33, 66, 99, 132},
		}

		for _, tokens := range sequences {
			tokens := tokens

			gc.Convey(fmt.Sprintf("sequence %v", tokens), func() {
				values := make([]*primitive.Value, len(tokens))

				for i, tok := range tokens {
					values[i] = primitive.BaseValue(tok)
				}

				state := primitive.NewValue()
				*state = *values[0]
				prevScale, prevTranslate := state.Motor()

				motorChanged := 0

				for step := 1; step < len(values); step++ {
					mapped := primitive.NewValue()
					operation.MotorApply(state[:], values[step][:], mapped[:])

					newState := primitive.NewValue()
					operation.XOR(state[:], mapped[:], newState[:])
					state = newState

					scale, translate := state.Motor()

					if scale != prevScale || translate != prevTranslate {
						motorChanged++
					}

					prevScale = scale
					prevTranslate = translate
				}

				gc.So(motorChanged, gc.ShouldEqual, len(values)-1)

				forward := accumulateSequence(values)
				reversed := make([]*primitive.Value, len(values))

				for i, v := range values {
					reversed[len(values)-1-i] = v
				}

				backward := accumulateSequence(reversed)
				gc.So(forward.Equal(backward), gc.ShouldBeFalse)

				swapped := make([]*primitive.Value, len(values))
				copy(swapped, values)
				swapped[1], swapped[2] = swapped[2], swapped[1]

				gc.So(
					accumulateSequence(values).Equal(accumulateSequence(swapped)),
					gc.ShouldBeFalse,
				)
			})
		}
	})
}

/*
TestSequenceAccumulationMotorPlateau documents that some byte sequences keep the
derived Motor() pair fixed across XOR steps; non-commutativity of ordering is
asserted separately on sequences where the motor does move every step.
*/
func TestSequenceAccumulationMotorPlateau(t *testing.T) {
	gc.Convey("Given powers-of-two tokens", t, func() {
		tokens := []byte{1, 2, 4, 8}
		values := make([]*primitive.Value, len(tokens))

		for i, tok := range tokens {
			values[i] = primitive.BaseValue(tok)
		}

		state := primitive.NewValue()
		*state = *values[0]
		prevScale, prevTranslate := state.Motor()

		motorChanged := 0

		for step := 1; step < len(values); step++ {
			mapped := primitive.NewValue()
			operation.MotorApply(state[:], values[step][:], mapped[:])

			newState := primitive.NewValue()
			operation.XOR(state[:], mapped[:], newState[:])
			state = newState

			scale, translate := state.Motor()

			if scale != prevScale || translate != prevTranslate {
				motorChanged++
			}

			prevScale = scale
			prevTranslate = translate
		}

		gc.So(motorChanged, gc.ShouldEqual, 0)

		finalScale, finalTranslate := state.Motor()
		gc.So(finalScale, gc.ShouldEqual, uint16(1))
		gc.So(finalTranslate, gc.ShouldEqual, uint16(0))
	})
}

/*
CLAIM: MOTOR ORBIT PERIODICITY
For a motor f(p)=s*p+t (mod 8191):
  - If s != 1: the multiplicative order of s divides |GF(8191)*| = 8190
    (Lagrange's theorem). The orbit period equals that order.
  - If s == 1: the motor is a pure translation f(p) = p + t. Its additive
    period is 8191/gcd(t,8191) = 8191 (since 8191 is prime and t != 0).
*/
func TestMotorOrbitPeriodicity(t *testing.T) {
	gc.Convey("Given motors with scale != 1 (non-trivial multiplicative orbit)", t, func() {
		nonTrivialCases := []struct {
			bits []int
		}{
			{bits: []int{5, 7}},
			{bits: []int{3, 11}},
			{bits: []int{7, 13}},
			{bits: []int{1, 5, 9}},
		}

		for _, tc := range nonTrivialCases {
			tc := tc

			gc.Convey("Bits "+formatBits(tc.bits)+" orbit period divides 8190", func() {
				value := primitive.NewValue()

				for _, b := range tc.bits {
					value.Set(b)
				}

				scale, translate := value.Motor()
				gc.So(scale, gc.ShouldNotEqual, 1)

				startPos := uint16(1)
				current := primitive.ApplyMotor(scale, translate, startPos)
				period := 1

				for current != startPos && period <= 8190 {
					current = primitive.ApplyMotor(scale, translate, current)
					period++
				}

				gc.So(current, gc.ShouldEqual, startPos)
				gc.So(8190%period, gc.ShouldEqual, 0)
			})
		}
	})

	gc.Convey("Given motors with scale == 1 (pure translation)", t, func() {
		value := primitive.BaseValue(30)
		scale, translate := value.Motor()
		gc.So(scale, gc.ShouldEqual, 1)

		startPos := uint16(1)
		current := primitive.ApplyMotor(scale, translate, startPos)
		period := 1

		for current != startPos && period <= 8191 {
			current = primitive.ApplyMotor(scale, translate, current)
			period++
		}

		gc.So(current, gc.ShouldEqual, startPos)
		gc.So(period, gc.ShouldEqual, 8191)
	})
}

/*
formatBits produces a simple label for test case identification.
*/
func formatBits(bits []int) string {
	out := "{"

	for i, b := range bits {
		if i > 0 {
			out += ","
		}

		out += strconv.Itoa(b)
	}

	return out + "}"
}

/*
CLAIM: CRT INDEPENDENT DECOMPOSITION OF COPRIME VALUES
When a composite Value has coprime components, those components can be
independently processed and exactly recombined. Operations on one component
cannot corrupt the other because they share no prime factors (zero GCD).
*/
func TestCRTIndependentDecomposition(t *testing.T) {
	gc.Convey("Given a composite Value built from coprime components", t, func() {
		compA := primitive.BaseValue(14)
		compB := primitive.BaseValue(15)

		gcdCheck := primitive.NewValue()
		operation.AND(compA[:], compB[:], gcdCheck[:])
		gc.So(gcdCheck.IsZero(), gc.ShouldBeTrue)

		composite := primitive.NewValue()
		operation.OR(compA[:], compB[:], composite[:])

		gc.Convey("Components can be independently extracted via AND", func() {
			extractA := primitive.NewValue()
			operation.AND(composite[:], compA[:], extractA[:])

			extractB := primitive.NewValue()
			operation.AND(composite[:], compB[:], extractB[:])

			gc.So(extractA.Equal(compA), gc.ShouldBeTrue)
			gc.So(extractB.Equal(compB), gc.ShouldBeTrue)
		})

		gc.Convey("Motor transform on one component does not affect the other", func() {
			motorSource := primitive.BaseValue(42)

			transformedA := primitive.NewValue()
			operation.MotorApply(motorSource[:], compA[:], transformedA[:])

			recombined := primitive.NewValue()
			operation.OR(transformedA[:], compB[:], recombined[:])

			reExtractB := primitive.NewValue()
			operation.AND(recombined[:], compB[:], reExtractB[:])

			gc.So(reExtractB.Equal(compB), gc.ShouldBeTrue)
		})

		gc.Convey("Exact recombination after independent processing", func() {
			motorSrc1 := primitive.BaseValue(30)
			motorSrc2 := primitive.BaseValue(70)

			transformedA := primitive.NewValue()
			operation.MotorApply(motorSrc1[:], compA[:], transformedA[:])

			transformedB := primitive.NewValue()
			operation.MotorApply(motorSrc2[:], compB[:], transformedB[:])

			combined := primitive.NewValue()
			operation.OR(transformedA[:], transformedB[:], combined[:])

			invertedA := primitive.NewValue()
			operation.MotorInvert(motorSrc1[:], transformedA[:], invertedA[:])

			invertedB := primitive.NewValue()
			operation.MotorInvert(motorSrc2[:], transformedB[:], invertedB[:])

			recoveredComposite := primitive.NewValue()
			operation.OR(invertedA[:], invertedB[:], recoveredComposite[:])

			gc.So(recoveredComposite.Equal(composite), gc.ShouldBeTrue)
		})
	})
}

/*
CLAIM: SELF-ROUTING VIA AND WITH SUMMARY VALUES
A machine's summary Value (OR of its corpus) AND'd with an incoming query
determines routing relevance. If the AND is non-zero, the machine has
structural overlap with the query. If zero, the machine is guaranteed
to have nothing relevant.
*/
func TestSelfRoutingViaSummaryAND(t *testing.T) {
	gc.Convey("Given machines with disjoint summary Values", t, func() {
		machineSummaryA := primitive.NewValue()
		machineSummaryA.Set(10)
		machineSummaryA.Set(20)
		machineSummaryA.Set(30)
		machineSummaryA.Set(40)

		machineSummaryB := primitive.NewValue()
		machineSummaryB.Set(100)
		machineSummaryB.Set(200)
		machineSummaryB.Set(300)
		machineSummaryB.Set(400)

		gc.Convey("Query overlapping only machine A routes exclusively to A", func() {
			query := primitive.NewValue()
			query.Set(10)
			query.Set(20)
			query.Set(50)

			relevanceA := primitive.NewValue()
			operation.AND(machineSummaryA[:], query[:], relevanceA[:])

			relevanceB := primitive.NewValue()
			operation.AND(machineSummaryB[:], query[:], relevanceB[:])

			gc.So(relevanceA.PopCount(), gc.ShouldEqual, 2)
			gc.So(relevanceB.IsZero(), gc.ShouldBeTrue)
		})

		gc.Convey("Query overlapping both machines routes to both with correct weights", func() {
			query := primitive.NewValue()
			query.Set(10)
			query.Set(20)
			query.Set(30)
			query.Set(100)

			relevanceA := primitive.NewValue()
			operation.AND(machineSummaryA[:], query[:], relevanceA[:])

			relevanceB := primitive.NewValue()
			operation.AND(machineSummaryB[:], query[:], relevanceB[:])

			gc.So(relevanceA.PopCount(), gc.ShouldEqual, 3)
			gc.So(relevanceB.PopCount(), gc.ShouldEqual, 1)
			gc.So(relevanceA.PopCount(), gc.ShouldBeGreaterThan, relevanceB.PopCount())
		})

		gc.Convey("Query with no factor overlap routes away from all machines", func() {
			query := primitive.NewValue()
			query.Set(500)
			query.Set(600)

			relevanceA := primitive.NewValue()
			operation.AND(machineSummaryA[:], query[:], relevanceA[:])

			relevanceB := primitive.NewValue()
			operation.AND(machineSummaryB[:], query[:], relevanceB[:])

			gc.So(relevanceA.IsZero(), gc.ShouldBeTrue)
			gc.So(relevanceB.IsZero(), gc.ShouldBeTrue)
		})

		gc.Convey("Summary built from corpus via OR correctly aggregates all factors", func() {
			corpus := []byte{6, 10, 14, 15, 21}
			summary := primitive.NewValue()

			for _, b := range corpus {
				v := primitive.BaseValue(b)
				operation.OR(summary[:], v[:], summary[:])
			}

			query := primitive.BaseValue(42)
			relevance := primitive.NewValue()
			operation.AND(summary[:], query[:], relevance[:])

			gc.So(relevance.PopCount(), gc.ShouldBeGreaterThan, 0)

			for bit := 0; bit < primitive.CoreBits; bit++ {
				if relevance.Has(bit) {
					gc.So(summary.Has(bit), gc.ShouldBeTrue)
					gc.So(query.Has(bit), gc.ShouldBeTrue)
				}
			}
		})
	})
}

/*
CLAIM: BIDIRECTIONAL RESOLUTION FROM A MIDDLE FRAGMENT
Given a middle fragment of a motor-encoded sequence, applying forward and
inverse motors reconstructs both the suffix (forward) and prefix (backward).
The system navigates both directions from any position.
*/
func TestBidirectionalResolutionFromMiddle(t *testing.T) {
	gc.Convey("Given a 5-token sequence encoded via sequential motor application", t, func() {
		tokens := []byte{42, 77, 210, 105, 30}
		values := make([]*primitive.Value, len(tokens))

		for i, tok := range tokens {
			values[i] = primitive.BaseValue(tok)
		}

		encoded := make([]*primitive.Value, len(values))
		encoded[0] = primitive.NewValue()
		*encoded[0] = *values[0]

		for step := 1; step < len(values); step++ {
			mapped := primitive.NewValue()
			operation.MotorApply(encoded[step-1][:], values[step][:], mapped[:])
			encoded[step] = mapped
		}

		middleIdx := 2

		gc.Convey("Forward resolution from middle reconstructs suffix", func() {
			for step := middleIdx + 1; step < len(encoded); step++ {
				reconstructed := primitive.NewValue()
				operation.MotorApply(encoded[step-1][:], values[step][:], reconstructed[:])

				gc.So(reconstructed.Equal(encoded[step]), gc.ShouldBeTrue)
			}
		})

		gc.Convey("Inverse resolution from middle reconstructs prefix", func() {
			for step := middleIdx; step > 0; step-- {
				reconstructedPrev := primitive.NewValue()
				operation.MotorInvert(encoded[step-1][:], encoded[step][:], reconstructedPrev[:])

				gc.So(reconstructedPrev.Equal(values[step]), gc.ShouldBeTrue)
			}
		})

		gc.Convey("Full round-trip from middle recovers every original token", func() {
			for step := 1; step < len(values); step++ {
				recovered := primitive.NewValue()
				operation.MotorInvert(encoded[step-1][:], encoded[step][:], recovered[:])

				gc.So(recovered.Equal(values[step]), gc.ShouldBeTrue)
			}
		})
	})
}

/*
CLAIM: NOVELTY EXTRACTION WITH BYTE-PROJECTED VALUES
Given a context (accumulated knowledge) and new input, the novelty is
exactly what the new input contains that the context does not:
novelty = newInput & ~context. This is Material Nonimplication (AndNot).
*/
func TestNoveltyExtractionWithByteProjectedValues(t *testing.T) {
	gc.Convey("Given a context built from real byte-projected Values", t, func() {
		contextBytes := []byte{6, 10, 15, 30, 42}
		context := primitive.NewValue()

		for _, b := range contextBytes {
			v := primitive.BaseValue(b)
			operation.OR(context[:], v[:], context[:])
		}

		gc.Convey("New input sharing all factors with context yields zero novelty", func() {
			familiar := primitive.BaseValue(6)

			novelty := primitive.NewValue()
			operation.AndNot(familiar[:], context[:], novelty[:])

			gc.So(novelty.IsZero(), gc.ShouldBeTrue)
		})

		gc.Convey("New input with unique factors yields exactly those factors as novelty", func() {
			novel := primitive.BaseValue(77)

			novelty := primitive.NewValue()
			operation.AndNot(novel[:], context[:], novelty[:])

			gc.So(novelty.IsZero(), gc.ShouldBeFalse)

			for bit := 0; bit < primitive.CoreBits; bit++ {
				if novelty.Has(bit) {
					gc.So(novel.Has(bit), gc.ShouldBeTrue)
					gc.So(context.Has(bit), gc.ShouldBeFalse)
				}
			}
		})

		gc.Convey("Novelty plus shared factors reconstruct the original input", func() {
			input := primitive.BaseValue(105)

			novelty := primitive.NewValue()
			operation.AndNot(input[:], context[:], novelty[:])

			shared := primitive.NewValue()
			operation.AND(input[:], context[:], shared[:])

			reconstructed := primitive.NewValue()
			operation.OR(novelty[:], shared[:], reconstructed[:])

			gc.So(reconstructed.Equal(input), gc.ShouldBeTrue)
		})

		gc.Convey("Multiple novel inputs accumulate additive novelty against context", func() {
			inputs := []byte{77, 143, 187}
			totalNovelty := primitive.NewValue()

			for _, b := range inputs {
				v := primitive.BaseValue(b)
				nov := primitive.NewValue()
				operation.AndNot(v[:], context[:], nov[:])
				operation.OR(totalNovelty[:], nov[:], totalNovelty[:])
			}

			gc.So(totalNovelty.PopCount(), gc.ShouldBeGreaterThan, 0)

			verifyNovelty := primitive.NewValue()
			operation.AND(totalNovelty[:], context[:], verifyNovelty[:])
			gc.So(verifyNovelty.IsZero(), gc.ShouldBeTrue)
		})
	})
}

/*
CLAIM: RESIDUAL TRACKING IS MONOTONIC UNDER SATISFYING OUTPUT
When output covers additional prompt structure, prompt & ~output should only
lose factors or stay equal; it must never regain satisfied structure.
*/
func TestResidualTrackingMonotonicity(t *testing.T) {
	gc.Convey("Given a prompt with four distinct structural components", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(10)
		prompt.Set(20)
		prompt.Set(30)
		prompt.Set(40)

		outputs := []*primitive.Value{
			primitive.NewValue(),
			valueWithBits(10),
			valueWithBits(10, 30),
			valueWithBits(10, 20, 30, 40),
		}

		residualCounts := make([]int, 0, len(outputs))
		residuals := make([]*primitive.Value, 0, len(outputs))

		for _, output := range outputs {
			residual := primitive.NewValue()
			operation.AndNot(prompt[:], output[:], residual[:])
			residuals = append(residuals, residual)
			residualCounts = append(residualCounts, residual.PopCount())
		}

		gc.Convey("Residual popcount decreases as more prompt structure is satisfied", func() {
			gc.So(residualCounts, gc.ShouldResemble, []int{4, 3, 2, 0})
		})

		gc.Convey("Later residuals are subsets of earlier residuals", func() {
			for index := 1; index < len(residuals); index++ {
				overlap := primitive.NewValue()
				operation.AND(residuals[index][:], residuals[index-1][:], overlap[:])
				gc.So(overlap.Equal(residuals[index]), gc.ShouldBeTrue)
			}
		})
	})
}

/*
CLAIM: SUMMARY ROUTING RANKS MACHINES BY EXACT OVERLAP
Routing relevance should be the exact PopCount of the query-summary AND.
*/
func TestSummaryRoutingRanksByExactOverlap(t *testing.T) {
	gc.Convey("Given three machine summaries with different overlaps", t, func() {
		query := valueWithBits(10, 20, 30, 40)
		summaries := []*primitive.Value{
			valueWithBits(10, 20, 30, 50),
			valueWithBits(10, 20),
			valueWithBits(70, 80),
		}

		overlaps := make([]int, 0, len(summaries))

		for _, summary := range summaries {
			match := primitive.NewValue()
			operation.AND(query[:], summary[:], match[:])
			overlaps = append(overlaps, match.PopCount())
		}

		gc.Convey("Overlap counts preserve the exact routing order", func() {
			gc.So(overlaps, gc.ShouldResemble, []int{3, 2, 0})
		})
	})
}

var benchAccumulateResult *primitive.Value

func BenchmarkSequenceAccumulation(b *testing.B) {
	tokens := []byte{42, 77, 210, 105, 30, 70, 255, 15}
	values := make([]*primitive.Value, len(tokens))

	for i, tok := range tokens {
		values[i] = primitive.BaseValue(tok)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchAccumulateResult = accumulateSequence(values)
	}
}

func BenchmarkNoveltyExtraction(b *testing.B) {
	context := primitive.NewValue()

	for _, byt := range []byte{6, 10, 15, 30, 42} {
		v := primitive.BaseValue(byt)
		operation.OR(context[:], v[:], context[:])
	}

	input := primitive.BaseValue(105)
	novelty := primitive.NewValue()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		operation.AndNot(input[:], context[:], novelty[:])
	}
}

func BenchmarkGCDRouting(b *testing.B) {
	summary := primitive.NewValue()
	summary.Set(10)
	summary.Set(20)
	summary.Set(30)
	summary.Set(40)

	query := primitive.NewValue()
	query.Set(10)
	query.Set(20)
	query.Set(50)

	result := primitive.NewValue()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		operation.AND(summary[:], query[:], result[:])
	}
}
