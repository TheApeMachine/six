package lang

import (
	"context"
	"fmt"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Program represents an active execution trace seeking a
specific target state. It applies hyperdimensional query
masks against a pool of unstructured candidates.
*/
type ProgramServer struct {
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	state       *errnie.State
	serverSide  net.Conn
	clientSide  net.Conn
	client      Evaluator
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	workerPool  *pool.Pool
	sink        *telemetry.Sink
	start       primitive.Value
	target      primitive.Value
	buffer      []primitive.Value
	maxSteps    int
	macroIndex  *macro.MacroIndexServer
}

type programServerOpts func(*ProgramServer)

/*
NewProgramServer creates a new ProgramServer.
*/
func NewProgramServer(opts ...programServerOpts) *ProgramServer {
	server := &ProgramServer{
		state:       errnie.NewState("logic/lang/programServer"),
		clientConns: map[string]*rpc.Conn{},
		sink:        telemetry.NewSink(),
		maxSteps:    int(numeric.FermatPrime),
	}

	for _, opt := range opts {
		opt(server)
	}

	if server.ctx == nil || server.cancel == nil {
		server.ctx, server.cancel = context.WithCancel(context.Background())
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":  server.ctx,
			"sink": server.sink,
		})
	})

	if server.state.Failed() {
		return server
	}

	if server.macroIndex == nil {
		server.macroIndex = macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(server.ctx),
		)
	}

	server.serverSide, server.clientSide = net.Pipe()
	server.client = Evaluator_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		server.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server
}

/*
Client returns a Cap'n Proto client connected to this ProgramServer.
*/
func (server *ProgramServer) Client(clientID string) Evaluator {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe.
*/
func (server *ProgramServer) Close() error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	if server.serverConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverConn.Close()
		})

		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			errnie.GuardVoid(server.state, func() error {
				return conn.Close()
			})
		}

		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverSide.Close()
		})

		server.serverSide = nil
	}

	if server.clientSide != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.clientSide.Close()
		})

		server.clientSide = nil
	}

	if server.cancel != nil {
		server.cancel()
	}

	return server.state.Err()
}

/*
Write appends streamed native program Values into the server buffer.
*/
func (server *ProgramServer) Write(ctx context.Context, call Evaluator_write) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	seedList := errnie.Guard(server.state, func() (primitive.Value_List, error) {
		return call.Args().Seed()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	if seedList.Len() < 2 {
		return errnie.Error(
			NewProgramError(ProgramErrorTypeSeedPairRequired),
			"seed_count", seedList.Len(),
		)
	}

	server.start = errnie.Guard(server.state, func() (primitive.Value, error) {
		scratch, err := server.boundaryScratch(server.start)
		if err != nil {
			return primitive.Value{}, err
		}

		scratch.CopyFrom(seedList.At(0))
		return scratch, nil
	})

	server.target = errnie.Guard(server.state, func() (primitive.Value, error) {
		scratch, err := server.boundaryScratch(server.target)
		if err != nil {
			return primitive.Value{}, err
		}

		scratch.CopyFrom(seedList.At(1))
		return scratch, nil
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	return nil
}

/*
Done finalizes the current streamed program boundary.
*/
func (server *ProgramServer) Done(ctx context.Context, call Evaluator_done) error {
	server.state.Reset()

	_, err := call.AllocResults()
	if err != nil {
		return err
	}

	return nil
}

/*
Execute drops the query mask into the candidate pool and follows the path
of lowest geometric resistance until it achieves phase-lock with the Target.
(This replaces ReagentLoop).
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

	if prog.macroIndex == nil {
		return nil, errnie.Error(
			NewProgramError(ProgramErrorTypeMacroIndexRequired),
		)
	}

	loopKey := macro.AffineKeyFromValues(prog.start, prog.target)

	currentState := errnie.Guard(prog.state, func() (primitive.Value, error) {
		return primitive.New()
	})
	currentState.CopyFrom(prog.start)

	if prog.state.Failed() {
		return nil, prog.state.Err()
	}

	preResidue := errnie.Guard(prog.state, func() (primitive.Value, error) {
		return currentState.XOR(prog.target)
	}).CoreActiveCount()

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

		// 2. Evaluate the Pool
		matchResults := primitive.BatchEvaluate(queryMask, candidates)

		bestIndex := -1
		var bestRecovered primitive.Value
		bestResidue := int(numeric.FermatPrime)
		bestFitness := -1

		// 3. Find the lowest energy path
		for idx, match := range matchResults {
			if match.PhaseQuotient == 0 {
				continue // No algebraic relation
			}

			// FIX 2 & 3: The MatchResult ALREADY contains the mathematically healed,
			// geometrically perfect residue. No Affine math, no cloning, no allocations!
			recovered := match.Residue

			// Measure raw physical distance to the ultimate target
			postResidue := errnie.Guard(prog.state, func() (int, error) {
				delta, err := recovered.XOR(prog.target)
				if err != nil {
					return 0, err
				}
				return delta.CoreActiveCount(), nil
			})

			if bestIndex == -1 ||
				postResidue < bestResidue ||
				(postResidue == bestResidue && match.FitnessScore > bestFitness) {
				bestIndex = idx
				bestRecovered = recovered
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
		candidateRecord := prog.macroIndex.RecordCandidateResult(
			loopKey, preResidue, bestResidue, advanced, stable,
		)

		outcome := &Output{
			QueryMask:      queryMask,
			Matches:        matchResults,
			WinnerIndex:    bestIndex,
			RecoveredState: bestRecovered,
			PostResidue:    bestResidue,
			Steps:          step + 1,
			Candidate:      candidateRecord,
		}

		console.Trace(
			"OUTCOME",
			"mask", queryMask,
			"matches", matchResults,
			"winner", bestIndex,
			"recovered", bestRecovered,
			"postResidue", bestResidue,
			"steps", step+1,
			"candidate", candidateRecord,
		)

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
func ProgramServerWithContext sets a cancellable context.
*/
func ProgramServerWithContext(ctx context.Context) programServerOpts {
	return func(server *ProgramServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
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

/*
ProgramServerWithMacroIndex sets the shared macro index.
*/
func ProgramServerWithMacroIndex(index *macro.MacroIndexServer) programServerOpts {
	return func(server *ProgramServer) {
		server.macroIndex = index
	}
}

type ProgramError struct {
	Message string
	Err     ProgramErrorType
}

type ProgramErrorType string

const (
	ProgramErrorTypeStartAndTargetEmpty ProgramErrorType = "start and target values cannot be empty"
	ProgramErrorTypeSeedPairRequired    ProgramErrorType = "write requires at least start and target seeds"
	ProgramErrorTypeCandidatePoolEmpty  ProgramErrorType = "candidate pool cannot be empty"
	ProgramErrorTypeMacroIndexRequired  ProgramErrorType = "macro index is required"
	ProgramErrorTypeProgramStalled      ProgramErrorType = "program stalled"
	ProgramErrorTypeExecutionStalled    ProgramErrorType = "execution stalled"
	ProgramErrorTypeProgramExhausted    ProgramErrorType = "program exhausted"
)

func NewProgramError(err ProgramErrorType) *ProgramError {
	return &ProgramError{Message: string(err), Err: err}
}

func (err ProgramError) Error() string {
	return fmt.Sprintf("program error: %s: %s", err.Message, err.Err)
}
