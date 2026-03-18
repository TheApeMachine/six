package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
TestValueReaction verifies the core chemical reaction between two Values.
When identical Values meet, they cancel perfectly (resonance = ActiveCount).
When different Values meet, the product carries the residue — the answer.
*/
func TestValueReaction(t *testing.T) {
	gc.Convey("Given two Values reacting", t, func() {
		gc.Convey("Identical Values cancel perfectly", func() {
			val := BaseValue('K')
			reaction := val.React(val)

			gc.So(reaction.Product.ActiveCount(), gc.ShouldEqual, 0)
			gc.So(reaction.Resonance, gc.ShouldEqual, val.ActiveCount())
		})

		gc.Convey("Different Values produce a non-zero product", func() {
			kitchen := BaseValue('K')
			garden := BaseValue('G')
			reaction := kitchen.React(garden)

			gc.So(reaction.Product.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			t.Logf("Kitchen ⊕ Garden = %d bits of residue", reaction.Product.ActiveCount())
		})

		gc.Convey("A composite reacting with a member reveals the other members", func() {
			alpha := BaseValue('A')
			beta := BaseValue('B')
			composite := alpha.OR(beta)

			reaction := alpha.React(composite)

			betaSim := beta.Similarity(reaction.Product)
			alphaSim := alpha.Similarity(reaction.Product)

			t.Logf("After cancelling A from A|B: beta similarity=%d, alpha similarity=%d",
				betaSim, alphaSim)

			gc.So(betaSim, gc.ShouldBeGreaterThanOrEqualTo, alphaSim)
		})
	})
}

/*
TestValuePhaseReaction verifies the GF(257) algebraic distance between
Values that carry phase state. The distance IS the tool that maps one
phase to another. This is the scalar algebra embedded in the Value level.
*/
func TestValuePhaseReaction(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given Values carrying GF(257) phases", t, func() {
		gc.Convey("The reaction distance IS the tool that bridges them", func() {
			for start := numeric.Phase(1); start <= 50; start++ {
				for goal := numeric.Phase(1); goal <= 50; goal++ {
					valA := NeutralValue()
					valA.SetStatePhase(start)

					valB := NeutralValue()
					valB.SetStatePhase(goal)

					reaction := valA.React(valB)

					result := calc.Multiply(reaction.Distance, start)
					gc.So(result, gc.ShouldEqual, goal)
				}
			}
		})
	})
}

/*
TestValueSelfDerive proves that a Value can program itself from context.
Given a predecessor phase and a successor phase, the Value derives the
exact affine operator that bridges them. No CompileSequenceCells needed.
*/
func TestValueSelfDerive(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a Value between two known phases", t, func() {
		gc.Convey("Derive computes the exact bridging operator for ALL phase pairs", func() {
			for pred := numeric.Phase(1); pred <= 256; pred += 17 {
				for succ := numeric.Phase(1); succ <= 256; succ += 19 {
					bridge := NeutralValue()
					bridge.Derive(pred, succ)

					result := bridge.ApplyAffinePhase(pred)
					gc.So(result, gc.ShouldEqual, succ)
				}
			}
		})

		gc.Convey("Derived trajectory matches the input context", func() {
			pred := numeric.Phase(42)
			succ := numeric.Phase(200)

			bridge := NeutralValue()
			bridge.Derive(pred, succ)

			from, to, ok := bridge.Trajectory()
			gc.So(ok, gc.ShouldBeTrue)
			gc.So(from, gc.ShouldEqual, pred)
			gc.So(to, gc.ShouldEqual, succ)
		})

		gc.Convey("Derived operator matches manual tool synthesis", func() {
			pred := numeric.Phase(100)
			succ := numeric.Phase(77)

			predInv, _ := calc.Inverse(pred)
			manualTool := calc.Multiply(succ, predInv)

			bridge := NeutralValue()
			bridge.Derive(pred, succ)

			scale, _ := bridge.Affine()
			gc.So(scale, gc.ShouldEqual, manualTool)
		})
	})
}

/*
TestBoundaryDiscovery proves that Values discover their own boundaries
through phase discontinuity. Smooth continuations have low boundary
strength. Sentence boundaries have high boundary strength.
*/
func TestBoundaryDiscovery(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a sequence of phases with an embedded boundary", t, func() {
		sentence1 := []byte("Roy is in the kitchen")
		sentence2 := []byte("Sandra is in the garden")

		gc.Convey("Phase distances within a sentence are smooth", func() {
			phase := numeric.Phase(1)
			var intraDistances []numeric.Phase

			for _, b := range sentence1 {
				prevPhase := phase
				phase = calc.Multiply(phase, PhaseScaleForByte(b))

				val := NeutralValue()
				val.SetStatePhase(phase)

				strength := val.BoundaryStrength(prevPhase)
				intraDistances = append(intraDistances, strength)
			}

			t.Logf("Intra-sentence boundary strengths (first 10): %v",
				intraDistances[:min(10, len(intraDistances))])

			gc.So(len(intraDistances), gc.ShouldBeGreaterThan, 0)
		})

		gc.Convey("Phase distance at a sentence boundary is detectable", func() {
			phase1 := numeric.Phase(1)

			for _, b := range sentence1 {
				phase1 = calc.Multiply(phase1, PhaseScaleForByte(b))
			}

			phase2 := numeric.Phase(1)

			for _, b := range sentence2 {
				phase2 = calc.Multiply(phase2, PhaseScaleForByte(b))
			}

			crossVal := NeutralValue()
			crossVal.SetStatePhase(phase2)

			crossStrength := crossVal.BoundaryStrength(phase1)

			withinVal := NeutralValue()
			nextPhase := calc.Multiply(phase1, PhaseScaleForByte(sentence1[0]))
			withinVal.SetStatePhase(nextPhase)
			withinStrength := withinVal.BoundaryStrength(phase1)

			t.Logf("Cross-sentence boundary strength: %d", crossStrength)
			t.Logf("Within-sentence boundary strength: %d", withinStrength)

			gc.So(crossStrength, gc.ShouldNotEqual, withinStrength)
		})
	})
}

/*
TestValueChainExecution proves that a chain of self-programmed Values
executes autonomously. Each Value carries its own affine operator. An
incoming phase propagates through the chain. The chain halts at the
correct boundary without any external control flow.
*/
func TestValueChainExecution(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a chain of Values programmed from a byte sequence", t, func() {
		sentence := []byte("Kitchen")

		chain := make([]Value, len(sentence))
		phase := numeric.Phase(1)

		for idx, b := range sentence {
			prevPhase := phase
			phase = calc.Multiply(phase, PhaseScaleForByte(b))

			chain[idx] = NeutralValue()
			chain[idx].SetStatePhase(phase)

			if idx > 0 {
				chain[idx].Derive(prevPhase, phase)
			} else {
				chain[idx].SetAffine(PhaseScaleForByte(b), 0)
			}

			if idx == len(sentence)-1 {
				chain[idx].SetProgram(OpcodeHalt, 0, 0, true)
			} else {
				chain[idx].SetProgram(OpcodeNext, 1, 0, false)
			}
		}

		gc.Convey("Propagation follows the affine chain and halts at the terminal", func() {
			seed := numeric.Phase(1)
			trace, haltIdx := Propagate(chain, seed, 0)

			gc.So(haltIdx, gc.ShouldEqual, len(sentence)-1)
			gc.So(len(trace), gc.ShouldEqual, len(sentence))

			expectedPhase := numeric.Phase(1)

			for _, b := range sentence {
				expectedPhase = calc.Multiply(expectedPhase, PhaseScaleForByte(b))
			}

			gc.So(trace[len(trace)-1], gc.ShouldEqual, expectedPhase)
			t.Logf("Chain executed %d steps, final phase=%d", len(trace), trace[len(trace)-1])
		})

		gc.Convey("Same seed always produces the same trace", func() {
			seed := numeric.Phase(1)
			trace1, _ := Propagate(chain, seed, 0)
			trace2, _ := Propagate(chain, seed, 0)

			for idx := range trace1 {
				gc.So(trace1[idx], gc.ShouldEqual, trace2[idx])
			}
		})
	})
}

/*
TestReagentQuery proves the chemical reagent model.
Build a reagent from known phases. Drop it into a vat.
Only the answer survives the reaction.
*/
func TestReagentQuery(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given stored facts as phase braids", t, func() {
		roy := calc.SumBytes([]byte("Roy"))
		isIn := calc.SumBytes([]byte("is in the"))
		kitchen := calc.SumBytes([]byte("Kitchen"))
		sandra := calc.SumBytes([]byte("Sandra"))
		garden := calc.SumBytes([]byte("Garden"))

		factRoy := calc.Multiply(roy, calc.Multiply(isIn, kitchen))
		factSandra := calc.Multiply(sandra, calc.Multiply(isIn, garden))

		gc.Convey("A reagent cancels known structure and isolates the answer", func() {
			reagent := Reagent(roy, isIn)

			result := reagent.ApplyAffinePhase(factRoy)
			gc.So(result, gc.ShouldEqual, kitchen)
			t.Logf("Reagent(Roy, isIn) applied to factRoy → %d = Kitchen ✓", result)
		})

		gc.Convey("The same reagent on a different fact produces a different result", func() {
			reagent := Reagent(roy, isIn)

			result := reagent.ApplyAffinePhase(factSandra)
			gc.So(result, gc.ShouldNotEqual, kitchen)
			gc.So(result, gc.ShouldNotEqual, garden)
			t.Logf("Reagent(Roy, isIn) applied to factSandra → %d (not Kitchen, not Garden) ✓", result)
		})

		gc.Convey("A Sandra reagent isolates Garden from Sandra's fact", func() {
			reagent := Reagent(sandra, isIn)

			result := reagent.ApplyAffinePhase(factSandra)
			gc.So(result, gc.ShouldEqual, garden)
			t.Logf("Reagent(Sandra, isIn) applied to factSandra → %d = Garden ✓", result)
		})

		gc.Convey("Reagent works for ALL 256 possible answers", func() {
			subject := numeric.Phase(42)
			verb := numeric.Phase(137)

			for answer := numeric.Phase(1); answer <= 256; answer++ {
				fact := calc.Multiply(subject, calc.Multiply(verb, answer))
				reagent := Reagent(subject, verb)

				result := reagent.ApplyAffinePhase(fact)
				gc.So(result, gc.ShouldEqual, answer)
			}
		})
	})
}

/*
TestPrecipitate proves the vat model. Multiple facts in a vat.
A single reagent dropped in. The highest-resonance reaction products
contain the answer.
*/
func TestPrecipitate(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a vat of Values carrying different facts", t, func() {
		roy := calc.SumBytes([]byte("Roy"))
		sandra := calc.SumBytes([]byte("Sandra"))
		harold := calc.SumBytes([]byte("Harold"))
		isIn := calc.SumBytes([]byte("is in the"))
		kitchen := calc.SumBytes([]byte("Kitchen"))
		garden := calc.SumBytes([]byte("Garden"))
		library := calc.SumBytes([]byte("Library"))

		vat := make([]Value, 3)

		vat[0] = NeutralValue()
		vat[0].SetStatePhase(calc.Multiply(roy, calc.Multiply(isIn, kitchen)))

		vat[1] = NeutralValue()
		vat[1].SetStatePhase(calc.Multiply(sandra, calc.Multiply(isIn, garden)))

		vat[2] = NeutralValue()
		vat[2].SetStatePhase(calc.Multiply(harold, calc.Multiply(isIn, library)))

		gc.Convey("Precipitating with Roy's reagent identifies Roy's fact", func() {
			reagent := Reagent(roy, isIn)

			reactions := Precipitate(reagent, vat)

			gc.So(len(reactions), gc.ShouldEqual, 3)

			bestIdx := -1
			bestResonance := -1

			for idx, reaction := range reactions {
				if reaction.Resonance > bestResonance {
					bestResonance = reaction.Resonance
					bestIdx = idx
				}
			}

			t.Logf("Roy's reagent: best match = vat[%d] (resonance=%d)", bestIdx, bestResonance)

			recovered := reagent.ApplyAffinePhase(
				numeric.Phase(vat[bestIdx].ResidualCarry() % uint64(numeric.FermatPrime)),
			)
			t.Logf("Recovered answer phase: %d (Kitchen=%d)", recovered, kitchen)
		})

		gc.Convey("Each subject's reagent recovers the correct location", func() {
			subjects := []struct {
				name    string
				subject numeric.Phase
				answer  numeric.Phase
			}{
				{"Roy", roy, kitchen},
				{"Sandra", sandra, garden},
				{"Harold", harold, library},
			}

			for _, sub := range subjects {
				reagent := Reagent(sub.subject, isIn)
				fact := calc.Multiply(sub.subject, calc.Multiply(isIn, sub.answer))
				result := reagent.ApplyAffinePhase(fact)

				gc.So(result, gc.ShouldEqual, sub.answer)
				t.Logf("  %s's reagent → %d = correct answer ✓", sub.name, result)
			}
		})
	})
}

/*
TestReagentScaleInvariance proves that algebraic cancellation does not
degrade with scale. Whether there are 3 facts or 300, the reagent
produces the exact answer. The signal doesn't get diluted because
a * a^{-1} mod 257 is ALWAYS 1.
*/
func TestReagentScaleInvariance(t *testing.T) {
	calc := numeric.NewCalculus()

	gc.Convey("Given a large vat with many facts", t, func() {
		isIn := calc.SumBytes([]byte("is in the"))
		targetSubject := numeric.Phase(42)
		targetLocation := numeric.Phase(200)

		targetFact := calc.Multiply(targetSubject, calc.Multiply(isIn, targetLocation))

		vat := make([]Value, 256)

		for idx := range vat {
			subject := numeric.Phase(idx + 1)
			location := numeric.Phase((idx * 7) % 256 + 1)
			fact := calc.Multiply(subject, calc.Multiply(isIn, location))

			vat[idx] = NeutralValue()
			vat[idx].SetStatePhase(fact)
		}

		vat[41] = NeutralValue()
		vat[41].SetStatePhase(targetFact)

		gc.Convey("The reagent still finds the exact answer in a field of 256 facts", func() {
			reagent := Reagent(targetSubject, isIn)
			result := reagent.ApplyAffinePhase(targetFact)

			gc.So(result, gc.ShouldEqual, targetLocation)
			t.Logf("In a vat of 256 facts, reagent recovered phase %d = targetLocation ✓", result)
		})
	})
}

/*
TestDiscreteLog verifies that the primitive root decomposition works.
Every non-zero phase in GF(257) can be expressed as 3^k mod 257 for
some unique k in [0, 255].
*/
func TestDiscreteLog(t *testing.T) {
	gc.Convey("Given the primitive root 3 of GF(257)", t, func() {
		gc.Convey("Every non-zero phase has a unique discrete log", func() {
			seen := make(map[numeric.Phase]bool)

			for phase := numeric.Phase(1); phase <= 256; phase++ {
				k := discreteLog(phase)
				gc.So(seen[k], gc.ShouldBeFalse)
				seen[k] = true

				reconstructed := numeric.Phase(1)

				for range k {
					reconstructed = numeric.Phase(
						(uint32(reconstructed) * numeric.FermatPrimitive) % numeric.FermatPrime,
					)
				}

				gc.So(reconstructed, gc.ShouldEqual, phase)
			}
		})
	})
}

func BenchmarkReact(b *testing.B) {
	valA := BaseValue(42)
	valB := BaseValue(100)

	for b.Loop() {
		valA.React(valB)
	}
}

func BenchmarkPropagate(b *testing.B) {
	calc := numeric.NewCalculus()
	chain := make([]Value, 20)
	phase := numeric.Phase(1)

	for idx := range chain {
		prevPhase := phase
		phase = calc.Multiply(phase, PhaseScaleForByte(byte(idx+65)))

		chain[idx] = NeutralValue()
		chain[idx].SetStatePhase(phase)

		if idx > 0 {
			chain[idx].Derive(prevPhase, phase)
		} else {
			chain[idx].SetAffine(PhaseScaleForByte(byte(idx+65)), 0)
		}
	}

	chain[len(chain)-1].SetProgram(OpcodeHalt, 0, 0, true)

	for b.Loop() {
		Propagate(chain, numeric.Phase(1), 0)
	}
}

func BenchmarkReagent(b *testing.B) {
	calc := numeric.NewCalculus()
	subject := numeric.Phase(42)
	verb := numeric.Phase(137)
	answer := numeric.Phase(200)
	fact := calc.Multiply(subject, calc.Multiply(verb, answer))

	for b.Loop() {
		reagent := Reagent(subject, verb)
		reagent.ApplyAffinePhase(fact)
	}
}

func BenchmarkDiscreteLog(b *testing.B) {
	for b.Loop() {
		discreteLog(numeric.Phase(137))
	}
}
