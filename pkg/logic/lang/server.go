package lang

import (
	"context"
	"sync"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
ProgramServer represents an active execution trace seeking a
specific target state. It applies hyperdimensional query
masks against a pool of unstructured candidates.
*/
type ProgramServer struct {
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	state      *errnie.State
	router     *cluster.Router
	workerPool *pool.Pool
	sink       *telemetry.Sink
	start      primitive.Value
	target     primitive.Value
	buffer     []primitive.Value
	maxSteps   int
}

type programServerOpts func(*ProgramServer)

/*
NewProgramServer creates a new ProgramServer.
*/
func NewProgramServer(opts ...programServerOpts) *ProgramServer {
	server := &ProgramServer{
		state:    errnie.NewState("logic/lang/programServer"),
		sink:     telemetry.NewSink(),
		maxSteps: int(numeric.FermatPrime),
	}

	for _, opt := range opts {
		opt(server)
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":    server.ctx,
			"cancel": server.cancel,
			"sink":   server.sink,
		})
	})

	if server.state.Failed() {
		return server
	}

	return server
}

/*
Read streams buffered Values back to the caller via the Callback capability.
Satisfies Service_Server.
*/
func (server *ProgramServer) Read(ctx context.Context, call primitive.Service_read) error {
	server.mu.RLock()
	defer server.mu.RUnlock()

	server.state.Reset()

	callback := errnie.Guard(server.state, func() (primitive.Service_Callback, error) {
		return primitive.Service_Callback(call.Args().Callback()), nil
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	for _, value := range server.buffer {
		if err := callback.Send(ctx, func(params primitive.Service_Callback_send_Params) error {
			return nil
		}); err != nil {
			return err
		}

		_ = value
	}

	_, release := callback.Done(ctx, nil)
	defer release()

	return nil
}

/*
Write appends a single streamed Value into the server buffer.
Satisfies Service_Server.
*/
func (server *ProgramServer) Write(ctx context.Context, call primitive.Service_write) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	value := errnie.Guard(server.state, func() (primitive.Value, error) {
		return call.Args().Value()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	server.buffer = append(server.buffer, value)

	return nil
}

/*
Close cancels the server context.
Satisfies Service_Server.
*/
func (server *ProgramServer) Close(ctx context.Context, call primitive.Service_close) error {
	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Execute drops the query mask into the candidate pool and follows the path
of lowest geometric resistance until it achieves phase-lock with the Target.
*/
func (prog *ProgramServer) Execute(candidates []primitive.Value) (*Output, error) {
	prog.state.Reset()

	if prog.start.ActiveCount() == 0 || prog.target.ActiveCount() == 0 {
		return nil, errnie.Error(
			NewProgramError(ProgramErrorTypeStartAndTargetEmpty),
		)
	}

	if len(candidates) == 0 {
		return nil, errnie.Error(
			NewProgramError(ProgramErrorTypeCandidatePoolEmpty),
		)
	}

	currentState := errnie.Guard(prog.state, func() (primitive.Value, error) {
		return primitive.New()
	})
	currentState.CopyFrom(prog.start)

	residueScratch := errnie.Guard(prog.state, func() (primitive.Value, error) {
		return primitive.New()
	})

	if prog.state.Failed() {
		return nil, prog.state.Err()
	}

	errnie.GuardVoid(prog.state, func() error {
		return currentState.XORInto(prog.target, &residueScratch)
	})

	preResidue := residueScratch.CoreActiveCount()

	if prog.state.Failed() {
		return nil, prog.state.Err()
	}

	prog.sink.Emit(telemetry.Event{
		Component: "Program",
		Action:    "Execute",
		Data: telemetry.EventData{
			Stage:          "start",
			PreResidue:     preResidue,
			CandidateCount: len(candidates),
			MaxSteps:       prog.maxSteps,
		},
	})

	for step := 0; step < prog.maxSteps; step++ {
		queryMask := primitive.BuildQueryMask(currentState)
		matchResults := primitive.BatchEvaluate(queryMask, candidates)

		bestIndex := -1
		var bestRecovered primitive.Value
		bestResidue := int(numeric.FermatPrime)
		bestFitness := -1

		for idx, match := range matchResults {
			if match.PhaseQuotient == 0 {
				continue
			}

			errnie.GuardVoid(prog.state, func() error {
				return match.Residue.XORInto(prog.target, &residueScratch)
			})

			postResidue := residueScratch.CoreActiveCount()

			if prog.state.Failed() {
				return nil, prog.state.Err()
			}

			if bestIndex == -1 ||
				postResidue < bestResidue ||
				(postResidue == bestResidue && match.FitnessScore > bestFitness) {
				bestIndex = idx
				bestRecovered = match.Residue
				bestResidue = postResidue
				bestFitness = match.FitnessScore
			}
		}

		if bestIndex == -1 {
			return nil, errnie.Error(
				NewProgramError(ProgramErrorTypeProgramStalled),
				"residue", preResidue,
				"step", step+1,
			)
		}

		advanced := bestResidue < preResidue
		stable := bestResidue == 0

		prog.sink.Emit(telemetry.Event{
			Component: "Program",
			Action:    "Execute",
			Data: telemetry.EventData{
				Stage:          "step",
				Step:           step + 1,
				MaxSteps:       prog.maxSteps,
				PreResidue:     preResidue,
				PostResidue:    bestResidue,
				BestIndex:      bestIndex,
				CandidateCount: len(candidates),
				Advanced:       advanced,
				Stable:         stable,
			},
		})

		outcome := &Output{
			QueryMask:      queryMask,
			Matches:        matchResults,
			WinnerIndex:    bestIndex,
			RecoveredState: bestRecovered,
			PostResidue:    bestResidue,
			Steps:          step + 1,
		}

		if console.IsTraceEnabled() {
			console.Trace(
				"OUTCOME",
				"mask", queryMask,
				"matches", matchResults,
				"winner", bestIndex,
				"recovered", bestRecovered,
				"postResidue", bestResidue,
				"steps", step+1,
			)
		}

		if stable {
			prog.sink.Emit(telemetry.Event{
				Component: "Program",
				Action:    "Execute",
				Data: telemetry.EventData{
					Stage:       "complete",
					Outcome:     "stable",
					Step:        step + 1,
					PostResidue: 0,
				},
			})

			return outcome, nil
		}

		if !advanced {
			prog.sink.Emit(telemetry.Event{
				Component: "Program",
				Action:    "Execute",
				Data: telemetry.EventData{
					Stage:       "complete",
					Outcome:     "stalled",
					Step:        step + 1,
					PostResidue: bestResidue,
				},
			})

			return outcome, errnie.Error(
				NewProgramError(ProgramErrorTypeExecutionStalled),
				"residue", bestResidue,
				"step", step+1,
			)
		}

		currentState.CopyFrom(bestRecovered)
		preResidue = bestResidue
	}

	prog.sink.Emit(telemetry.Event{
		Component: "Program",
		Action:    "Execute",
		Data: telemetry.EventData{
			Stage:       "complete",
			Outcome:     "exhausted",
			Step:        prog.maxSteps,
			PostResidue: preResidue,
		},
	})

	return nil, errnie.Error(
		NewProgramError(ProgramErrorTypeProgramExhausted),
		"steps", prog.maxSteps,
	)
}

/*
boundaryScratch returns one reusable mutable Value for start/target boundaries.
*/
func (server *ProgramServer) boundaryScratch(value primitive.Value) (primitive.Value, error) {
	if value.ActiveCount() > 0 {
		return value, nil
	}

	return primitive.New()
}

/*
Seed sets the execution boundaries used by Execute.
*/
func (server *ProgramServer) Seed(
	startValue primitive.Value,
	targetValue primitive.Value,
) error {
	server.state.Reset()

	if startValue.ActiveCount() == 0 || targetValue.ActiveCount() == 0 {
		return errnie.Error(
			NewProgramError(ProgramErrorTypeStartAndTargetEmpty),
		)
	}

	server.start = errnie.Guard(server.state, func() (primitive.Value, error) {
		scratch, err := server.boundaryScratch(server.start)
		if err != nil {
			return primitive.Value{}, err
		}

		scratch.CopyFrom(startValue)
		return scratch, nil
	})

	server.target = errnie.Guard(server.state, func() (primitive.Value, error) {
		scratch, err := server.boundaryScratch(server.target)
		if err != nil {
			return primitive.Value{}, err
		}

		scratch.CopyFrom(targetValue)
		return scratch, nil
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	return nil
}

/*
ProgramServerWithContext sets a cancellable context.
*/
func ProgramServerWithContext(ctx context.Context) programServerOpts {
	return func(server *ProgramServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
ProgramServerWithRouter sets the cluster router.
*/
func ProgramServerWithRouter(router *cluster.Router) programServerOpts {
	return func(server *ProgramServer) {
		server.router = router
	}
}

/*
ProgramServerWithWorkerPool sets the shared worker pool.
*/
func ProgramServerWithWorkerPool(pool *pool.Pool) programServerOpts {
	return func(server *ProgramServer) {
		server.workerPool = pool
	}
}

/*
ProgramServerWithSink sets the telemetry sink.
*/
func ProgramServerWithSink(sink *telemetry.Sink) programServerOpts {
	return func(server *ProgramServer) {
		server.sink = sink
	}
}

/*
ProgramServerWithMaxSteps sets the maximum execution steps.
*/
func ProgramServerWithMaxSteps(maxSteps int) programServerOpts {
	return func(server *ProgramServer) {
		server.maxSteps = max(maxSteps, 1)
	}
}
