package lang

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
valueBlocksMatch verifies that two Values carry identical raw state.
*/
func valueBlocksMatch(left primitive.Value, right primitive.Value) bool {
	for index := range 8 {
		if left.Block(index) != right.Block(index) {
			return false
		}
	}

	return true
}

/*
seedValue returns one deterministic lexical seed converted to primitive.Value.
*/
func seedValue(symbol byte) primitive.Value {
	return primitive.Value(primitive.BaseValue(symbol))
}

/*
newSeedList builds the list shape expected by Evaluator.Write.
*/
func newSeedList(start, target primitive.Value) []primitive.Value {
	return []primitive.Value{
		start,
		target,
		primitive.NeutralValue(),
	}
}

/*
newProgramServerForTests constructs ProgramServer with deterministic execution controls.
*/
func newProgramServerForTests() *ProgramServer {
	server := NewProgramServer(
		ProgramServerWithContext(context.Background()),
	)

	server.maxSteps = 4
	server.macroIndex = macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(context.Background()),
	)

	return server
}

/*
TestProgramServerWrite verifies Write stores start and target seeds exactly.
*/
func TestProgramServerWrite(t *testing.T) {
	gc.Convey("Given a ProgramServer and streamed primitive seeds", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		client := server.Client("logic/lang/server_test")
		start := seedValue('A')
		target := seedValue('B')
		seeds := newSeedList(start, target)

		gc.Convey("Write should retain exact start and target after Done", func() {
			err := client.Write(context.Background(), func(params Evaluator_write_Params) error {
				list, err := primitive.ValueSliceToList(seeds)
				if err != nil {
					return err
				}

				return params.SetSeed(list)
			})

			gc.So(err, gc.ShouldBeNil)

			future, release := client.Done(context.Background(), nil)
			defer release()

			_, err = future.Struct()
			gc.So(err, gc.ShouldBeNil)
			gc.So(valueBlocksMatch(server.start, start), gc.ShouldBeTrue)
			gc.So(valueBlocksMatch(server.target, target), gc.ShouldBeTrue)
		})

	})

	gc.Convey("Given a ProgramServer and an invalid one-seed write", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		client := server.Client("logic/lang/server_test/invalid-seed-count")
		start := seedValue('A')

		gc.Convey("Write should leave start and target empty", func() {
			err := client.Write(context.Background(), func(params Evaluator_write_Params) error {
				list, listErr := primitive.ValueSliceToList([]primitive.Value{start})
				if listErr != nil {
					return listErr
				}

				return params.SetSeed(list)
			})

			gc.So(err, gc.ShouldBeNil)
			gc.So(server.start.ActiveCount(), gc.ShouldEqual, 0)
			gc.So(server.target.ActiveCount(), gc.ShouldEqual, 0)
		})
	})
}

/*
TestProgramServerExecute verifies success and explicit failure modes.
*/
func TestProgramServerExecute(t *testing.T) {
	gc.Convey("Given a ProgramServer configured for deterministic Execute behavior", t, func() {
		/*
			programExecuteTestCase defines one Execute scenario with setup and expectations.
		*/
		type programExecuteTestCase struct {
			name                 string
			setup                func(*ProgramServer) ([]primitive.Value, error)
			expectErrorContains  string
			expectOutcomeOnError bool
			expectOutcomeNil     bool
			expectSteps          int
			expectWinnerIndex    int
			expectPostResidue    int
			expectCandidate      bool
			expectAdvanced       bool
			expectStable         bool
		}

		testCases := []programExecuteTestCase{
			{
				name: "Execute should return candidate metadata when first transition does not reduce residue",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.start = seedValue('A')

					target, err := primitive.New()
					if err != nil {
						return nil, err
					}

					target.SetStatePhase(2)
					server.target = target

					candidate, err := primitive.New()
					if err != nil {
						return nil, err
					}

					candidate.SetStatePhase(2)

					return []primitive.Value{candidate}, nil
				},
				expectErrorContains:  string(ProgramErrorTypeExecutionStalled),
				expectOutcomeOnError: true,
				expectOutcomeNil:     false,
				expectSteps:          1,
				expectWinnerIndex:    0,
				expectPostResidue:    7,
				expectCandidate:      true,
				expectAdvanced:       false,
				expectStable:         false,
			},
			{
				name: "Execute should expose candidate metadata when execution stalls",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.start = seedValue('A')

					target, err := primitive.New()
					if err != nil {
						return nil, err
					}

					target.SetStatePhase(2)
					server.target = target

					nonWinning, err := primitive.New()
					if err != nil {
						return nil, err
					}

					nonWinning.SetStatePhase(3)

					winning, err := primitive.New()
					if err != nil {
						return nil, err
					}

					winning.SetStatePhase(2)

					return []primitive.Value{nonWinning, winning}, nil
				},
				expectErrorContains:  string(ProgramErrorTypeExecutionStalled),
				expectOutcomeOnError: true,
				expectOutcomeNil:     false,
				expectSteps:          1,
				expectWinnerIndex:    0,
				expectPostResidue:    7,
				expectCandidate:      true,
				expectAdvanced:       false,
				expectStable:         false,
			},
			{
				name: "Execute should fail fast when start and target are empty",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.start = primitive.Value{}
					server.target = primitive.Value{}

					return []primitive.Value{primitive.NeutralValue()}, nil
				},
				expectErrorContains: string(ProgramErrorTypeStartAndTargetEmpty),
				expectOutcomeNil:    true,
			},
			{
				name: "Execute should fail fast on empty candidate pool",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.start = seedValue('A')
					server.target = seedValue('B')

					return nil, nil
				},
				expectErrorContains: string(ProgramErrorTypeCandidatePoolEmpty),
				expectOutcomeNil:    true,
			},
			{
				name: "Execute should report program stalled when no candidate has phase quotient",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.start = seedValue('A')

					target, err := primitive.New()
					if err != nil {
						return nil, err
					}

					target.SetStatePhase(7)
					server.target = target

					candidate, err := primitive.New()
					if err != nil {
						return nil, err
					}

					candidate.Set(9)

					return []primitive.Value{candidate}, nil
				},
				expectErrorContains: string(ProgramErrorTypeProgramStalled),
				expectOutcomeNil:    true,
			},
			{
				name: "Execute should report execution stalled when no candidate can reduce residue",
				setup: func(server *ProgramServer) ([]primitive.Value, error) {
					server.maxSteps = 1
					server.start = seedValue('A')

					target, err := primitive.New()
					if err != nil {
						return nil, err
					}

					target.SetStatePhase(5)
					target.Set(17)
					server.target = target

					candidate, err := primitive.New()
					if err != nil {
						return nil, err
					}

					candidate.SetStatePhase(5)

					return []primitive.Value{candidate}, nil
				},
				expectErrorContains:  string(ProgramErrorTypeExecutionStalled),
				expectOutcomeOnError: true,
				expectOutcomeNil:     false,
				expectSteps:          1,
				expectWinnerIndex:    0,
				expectPostResidue:    7,
				expectCandidate:      true,
				expectAdvanced:       false,
				expectStable:         false,
			},
		}

		for _, testCase := range testCases {
			testCase := testCase

			gc.Convey(testCase.name, func() {
				server := newProgramServerForTests()
				defer server.macroIndex.Close()
				defer server.Close()

				candidates, setupErr := testCase.setup(server)
				gc.So(setupErr, gc.ShouldBeNil)

				if setupErr != nil {
					return
				}

				outcome, executeErr := server.Execute(candidates)

				if testCase.expectErrorContains != "" {
					gc.So(executeErr, gc.ShouldNotBeNil)
					gc.So(
						executeErr.Error(),
						gc.ShouldContainSubstring,
						testCase.expectErrorContains,
					)

					if testCase.expectOutcomeOnError {
						gc.So(outcome, gc.ShouldNotBeNil)
						gc.So(outcome.Steps, gc.ShouldEqual, testCase.expectSteps)
						gc.So(outcome.WinnerIndex, gc.ShouldEqual, testCase.expectWinnerIndex)
						gc.So(outcome.PostResidue, gc.ShouldEqual, testCase.expectPostResidue)

						if testCase.expectCandidate {
							gc.So(outcome.Candidate, gc.ShouldNotBeNil)
							gc.So(outcome.Candidate.Advanced, gc.ShouldEqual, testCase.expectAdvanced)
							gc.So(outcome.Candidate.Stable, gc.ShouldEqual, testCase.expectStable)
						}

						return
					}

					gc.So(outcome, gc.ShouldBeNil)
					return
				}

				if testCase.expectOutcomeNil {
					gc.So(outcome, gc.ShouldBeNil)
					gc.So(executeErr, gc.ShouldBeNil)
					return
				}

				gc.So(executeErr, gc.ShouldBeNil)
				gc.So(outcome, gc.ShouldNotBeNil)
				gc.So(outcome.Steps, gc.ShouldEqual, testCase.expectSteps)
				gc.So(outcome.WinnerIndex, gc.ShouldEqual, testCase.expectWinnerIndex)
				gc.So(outcome.PostResidue, gc.ShouldEqual, testCase.expectPostResidue)

				if testCase.expectCandidate {
					gc.So(outcome.Candidate, gc.ShouldNotBeNil)
					gc.So(outcome.Candidate.Advanced, gc.ShouldEqual, testCase.expectAdvanced)
					gc.So(outcome.Candidate.Stable, gc.ShouldEqual, testCase.expectStable)
				}
			})
		}
	})
}

/*
TestProgramServerExecuteUsesSeededStart verifies Execute starts from the seeded boundary state.
*/
func TestProgramServerExecuteUsesSeededStart(t *testing.T) {
	gc.Convey("Given a ProgramServer with seeded start and target", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		start, startErr := primitive.New()
		gc.So(startErr, gc.ShouldBeNil)
		start.SetStatePhase(11)

		target, targetErr := primitive.New()
		gc.So(targetErr, gc.ShouldBeNil)
		target.SetStatePhase(13)

		server.start = start
		server.target = target

		candidate, candidateErr := primitive.New()
		gc.So(candidateErr, gc.ShouldBeNil)
		candidate.SetStatePhase(13)

		outcome, err := server.Execute([]primitive.Value{candidate})

		gc.So(outcome, gc.ShouldNotBeNil)
		gc.So(err, gc.ShouldNotBeNil)
		gc.So(err.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeExecutionStalled))
		gc.So(outcome.QueryMask.ResidualCarry(), gc.ShouldEqual, primitive.BuildQueryMask(start).ResidualCarry())
	})
}

/*
TestProgramServerExecuteRejectsNilMacroIndex verifies Execute fails cleanly when macro index is missing.
*/
func TestProgramServerExecuteRejectsNilMacroIndex(t *testing.T) {
	gc.Convey("Given a ProgramServer without a macro index", t, func() {
		server := newProgramServerForTests()
		defer server.Close()

		server.macroIndex.Close()
		server.macroIndex = nil
		server.start = seedValue('A')
		server.target = seedValue('B')
		candidate := seedValue('C')

		var (
			outcome *Output
			err     error
		)

		gc.So(func() {
			outcome, err = server.Execute([]primitive.Value{candidate})
		}, gc.ShouldNotPanic)
		gc.So(outcome, gc.ShouldBeNil)
		gc.So(err, gc.ShouldNotBeNil)
		gc.So(err.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeMacroIndexRequired))
	})
}

/*
TestProgramServerExecuteResidueProfile quantifies residue behavior across candidate pools.
*/
func TestProgramServerExecuteResidueProfile(t *testing.T) {
	gc.Convey("Given a ProgramServer with a fixed start-target gap", t, func() {
		/*
			residueProfileTestCase captures one candidate pool and expected residue dynamics.
		*/
		type residueProfileTestCase struct {
			name               string
			buildCandidates    func() ([]primitive.Value, error)
			expectWinnerIndex  int
			expectErrorSubstr  string
			expectAdvancedFlag bool
		}

		testCases := []residueProfileTestCase{
			{
				name: "Single affine candidate should stall when it cannot reduce the gap",
				buildCandidates: func() ([]primitive.Value, error) {
					candidate, err := primitive.New()
					if err != nil {
						return nil, err
					}

					candidate.SetStatePhase(2)
					return []primitive.Value{candidate}, nil
				},
				expectWinnerIndex:  0,
				expectErrorSubstr:  string(ProgramErrorTypeExecutionStalled),
				expectAdvancedFlag: false,
			},
			{
				name: "Best-fitness candidate can still stall when post-residue is unchanged",
				buildCandidates: func() ([]primitive.Value, error) {
					first, firstErr := primitive.New()
					if firstErr != nil {
						return nil, firstErr
					}
					first.SetStatePhase(3)

					second, secondErr := primitive.New()
					if secondErr != nil {
						return nil, secondErr
					}
					second.SetStatePhase(2)

					return []primitive.Value{first, second}, nil
				},
				expectWinnerIndex:  0,
				expectErrorSubstr:  string(ProgramErrorTypeExecutionStalled),
				expectAdvancedFlag: false,
			},
		}

		for _, testCase := range testCases {
			testCase := testCase

			gc.Convey(testCase.name, func() {
				server := newProgramServerForTests()
				defer server.macroIndex.Close()
				defer server.Close()

				server.start = seedValue('A')

				target, targetErr := primitive.New()
				gc.So(targetErr, gc.ShouldBeNil)
				target.SetStatePhase(2)
				server.target = target

				candidates, buildErr := testCase.buildCandidates()
				gc.So(buildErr, gc.ShouldBeNil)

				if buildErr != nil {
					return
				}

				outcome, executeErr := server.Execute(candidates)

				gc.So(executeErr, gc.ShouldNotBeNil)
				gc.So(executeErr.Error(), gc.ShouldContainSubstring, testCase.expectErrorSubstr)
				gc.So(outcome, gc.ShouldNotBeNil)
				gc.So(outcome.Candidate, gc.ShouldNotBeNil)
				gc.So(outcome.WinnerIndex, gc.ShouldEqual, testCase.expectWinnerIndex)
				gc.So(outcome.Candidate.PostResidue, gc.ShouldBeGreaterThanOrEqualTo, outcome.Candidate.PreResidue)
				gc.So(outcome.Candidate.Advanced, gc.ShouldEqual, testCase.expectAdvancedFlag)
			})
		}
	})
}

/*
TestProgramServerExecutePrioritizesResidueOverFitness verifies winner selection favors lower post-residue.
*/
func TestProgramServerExecutePrioritizesResidueOverFitness(t *testing.T) {
	gc.Convey("Given candidate pairs with competing fitness and residue", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		server.start = seedValue('A')

		target, targetErr := primitive.New()
		gc.So(targetErr, gc.ShouldBeNil)
		target.SetStatePhase(2)
		server.target = target

		queryMask := primitive.BuildQueryMask(server.start)
		type candidateMetric struct {
			value        primitive.Value
			fitnessScore int
			postResidue  int
			phaseQ       numeric.Phase
		}

		metrics := make([]candidateMetric, 0, 64)

		for phase := numeric.Phase(1); phase <= 64; phase++ {
			candidate, candidateErr := primitive.New()
			gc.So(candidateErr, gc.ShouldBeNil)

			if candidateErr != nil {
				return
			}

			candidate.SetStatePhase(phase)
			match := queryMask.EvaluateMatch(candidate)

			if match.PhaseQuotient == 0 {
				continue
			}

			startClone, startCloneErr := primitive.New()
			gc.So(startCloneErr, gc.ShouldBeNil)

			if startCloneErr != nil {
				return
			}

			startClone.CopyFrom(server.start)
			recovered := startClone.ApplyAffineValue(match.PhaseQuotient, 0)
			postResidue, residueErr := recovered.XOR(server.target)
			gc.So(residueErr, gc.ShouldBeNil)

			if residueErr != nil {
				return
			}

			metrics = append(metrics, candidateMetric{
				value:        candidate,
				fitnessScore: match.FitnessScore,
				postResidue:  postResidue.CoreActiveCount(),
				phaseQ:       match.PhaseQuotient,
			})
		}

		gc.So(len(metrics), gc.ShouldBeGreaterThan, 1)

		worseResidueHigherFitnessIndex := -1
		betterResidueLowerFitnessIndex := -1

		for leftIndex := range metrics {
			for rightIndex := range metrics {
				left := metrics[leftIndex]
				right := metrics[rightIndex]

				if left.postResidue > right.postResidue && left.fitnessScore > right.fitnessScore {
					worseResidueHigherFitnessIndex = leftIndex
					betterResidueLowerFitnessIndex = rightIndex
					break
				}
			}

			if worseResidueHigherFitnessIndex != -1 {
				break
			}
		}

		gc.So(worseResidueHigherFitnessIndex, gc.ShouldBeGreaterThanOrEqualTo, 0)
		gc.So(betterResidueLowerFitnessIndex, gc.ShouldBeGreaterThanOrEqualTo, 0)

		worseResidueHigherFitness := metrics[worseResidueHigherFitnessIndex]
		betterResidueLowerFitness := metrics[betterResidueLowerFitnessIndex]

		gc.So(worseResidueHigherFitness.postResidue, gc.ShouldBeGreaterThan, betterResidueLowerFitness.postResidue)
		gc.So(worseResidueHigherFitness.fitnessScore, gc.ShouldBeGreaterThan, betterResidueLowerFitness.fitnessScore)

		pairCandidates := []primitive.Value{
			worseResidueHigherFitness.value,
			betterResidueLowerFitness.value,
		}

		pairMatches := primitive.BatchEvaluate(queryMask, pairCandidates)

		firstStartClone, firstStartCloneErr := primitive.New()
		gc.So(firstStartCloneErr, gc.ShouldBeNil)
		firstStartClone.CopyFrom(server.start)
		firstRecovered := firstStartClone.ApplyAffineValue(pairMatches[0].PhaseQuotient, 0)
		firstResidueValue, firstResidueErr := firstRecovered.XOR(server.target)
		gc.So(firstResidueErr, gc.ShouldBeNil)

		secondStartClone, secondStartCloneErr := primitive.New()
		gc.So(secondStartCloneErr, gc.ShouldBeNil)
		secondStartClone.CopyFrom(server.start)
		secondRecovered := secondStartClone.ApplyAffineValue(pairMatches[1].PhaseQuotient, 0)
		secondResidueValue, secondResidueErr := secondRecovered.XOR(server.target)
		gc.So(secondResidueErr, gc.ShouldBeNil)

		firstResidue := firstResidueValue.CoreActiveCount()
		secondResidue := secondResidueValue.CoreActiveCount()

		gc.So(firstResidue, gc.ShouldBeGreaterThan, secondResidue)

		outcome, executeErr := server.Execute(pairCandidates)

		gc.So(executeErr, gc.ShouldNotBeNil)
		gc.So(executeErr.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeExecutionStalled))
		gc.So(outcome, gc.ShouldNotBeNil)
		gc.So(outcome.WinnerIndex, gc.ShouldEqual, 1)
		gc.So(outcome.PostResidue, gc.ShouldEqual, betterResidueLowerFitness.postResidue)
		gc.So(outcome.PostResidue, gc.ShouldBeLessThan, worseResidueHigherFitness.postResidue)
		gc.So(betterResidueLowerFitness.fitnessScore, gc.ShouldBeLessThan, worseResidueHigherFitness.fitnessScore)
	})
}

/*
TestProgramServerLifecycle verifies local RPC client wiring and Close teardown.
*/
func TestProgramServerLifecycle(t *testing.T) {
	gc.Convey("Given a ProgramServer with local RPC clients", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()

		clientA := server.Client("logic/lang/lifecycle/a")
		clientB := server.Client("logic/lang/lifecycle/b")

		gc.Convey("Client should be valid and Close should release all pipe resources", func() {
			gc.So(clientA.IsValid(), gc.ShouldBeTrue)
			gc.So(clientB.IsValid(), gc.ShouldBeTrue)
			gc.So(server.serverConn, gc.ShouldNotBeNil)
			gc.So(server.clientConns["logic/lang/lifecycle/a"], gc.ShouldNotBeNil)
			gc.So(server.clientConns["logic/lang/lifecycle/b"], gc.ShouldNotBeNil)

			err := server.Close()

			gc.So(err, gc.ShouldBeNil)
			gc.So(server.serverConn, gc.ShouldBeNil)
			gc.So(server.serverSide, gc.ShouldBeNil)
			gc.So(server.clientSide, gc.ShouldBeNil)
			gc.So(len(server.clientConns), gc.ShouldEqual, 0)
		})
	})
}

/*
TestProgramServerWriteAllocationBudget verifies steady-state Write allocations stay bounded.
*/
func TestProgramServerWriteAllocationBudget(t *testing.T) {
	gc.Convey("Given a ProgramServer and a fixed seed list", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		client := server.Client("logic/lang/allocation/write")
		seeds := newSeedList(seedValue('A'), seedValue('B'))

		allocations := testing.AllocsPerRun(200, func() {
			err := client.Write(context.Background(), func(params Evaluator_write_Params) error {
				list, err := params.NewSeed(int32(len(seeds)))
				if err != nil {
					return err
				}

				for index, seed := range seeds {
					entry := list.At(index)
					entry.CopyFrom(seed)
				}

				return nil
			})

			if err != nil {
				t.Fatalf("write failed: %v", err)
			}
		})

		gc.So(allocations, gc.ShouldBeLessThanOrEqualTo, 33.0)
	})
}

/*
TestProgramServerExecuteStableAllocationBudget verifies one-step stable execution allocations stay bounded.
*/
func TestProgramServerExecuteStableAllocationBudget(t *testing.T) {
	gc.Convey("Given a ProgramServer in stable one-step configuration", t, func() {
		server := newProgramServerForTests()
		defer server.macroIndex.Close()
		defer server.Close()

		server.start = seedValue('A')

		target, targetErr := primitive.New()
		gc.So(targetErr, gc.ShouldBeNil)
		target.SetStatePhase(2)
		server.target = target

		candidate, candidateErr := primitive.New()
		gc.So(candidateErr, gc.ShouldBeNil)
		candidate.SetStatePhase(2)
		candidates := []primitive.Value{candidate}

		allocations := testing.AllocsPerRun(200, func() {
			outcome, execErr := server.Execute(candidates)
			if execErr != nil {
				t.Fatalf("execute failed: %v", execErr)
			}

			if outcome == nil || outcome.PostResidue != 0 {
				t.Fatalf("unexpected outcome: %+v", outcome)
			}
		})

		gc.So(allocations, gc.ShouldBeLessThanOrEqualTo, 32.0)
	})
}

/*
BenchmarkProgramServerWrite measures RPC seed streaming cost.
*/
func BenchmarkProgramServerWrite(b *testing.B) {
	server := newProgramServerForTests()
	defer server.macroIndex.Close()
	defer server.Close()

	client := server.Client("logic/lang/benchmark/write")
	seeds := newSeedList(seedValue('A'), seedValue('B'))

	b.ResetTimer()

	for b.Loop() {
		err := client.Write(context.Background(), func(params Evaluator_write_Params) error {
			list, err := params.NewSeed(int32(len(seeds)))
			if err != nil {
				return err
			}

			for index, seed := range seeds {
				entry := list.At(index)
				entry.CopyFrom(seed)
			}

			return nil
		})

		if err != nil {
			b.Fatalf("write failed: %v", err)
		}
	}
}

/*
BenchmarkProgramServerExecuteStable measures stable one-step execution throughput.
*/
func BenchmarkProgramServerExecuteStable(b *testing.B) {
	server := newProgramServerForTests()
	defer server.macroIndex.Close()
	defer server.Close()

	server.start = seedValue('A')

	target, err := primitive.New()
	if err != nil {
		b.Fatalf("target allocation failed: %v", err)
	}
	target.SetStatePhase(2)
	server.target = target

	candidate, err := primitive.New()
	if err != nil {
		b.Fatalf("candidate allocation failed: %v", err)
	}
	candidate.SetStatePhase(2)

	candidates := []primitive.Value{candidate}

	b.ResetTimer()

	for b.Loop() {
		outcome, execErr := server.Execute(candidates)
		if execErr != nil {
			b.Fatalf("execute failed: %v", execErr)
		}

		if outcome == nil || outcome.PostResidue != 0 {
			b.Fatalf("unexpected execute outcome: %+v", outcome)
		}
	}
}
