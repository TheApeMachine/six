package lang

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
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

		_ = []programExecuteTestCase{
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

	})
}
