package processor

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Device is the address space for the RouteHint register in C7.
Each pipeline component checks whether a Value's RouteHint matches its own
device address. Non-matching Values pass through untouched.
*/
type Device uint8

const (
	DeviceNone    Device = 0
	DeviceCompute Device = 1
	DeviceGraph   Device = 2
	DeviceStore   Device = 3
	DeviceSynth   Device = 4
)

/*
InterpreterServer is a register-machine interpreter that executes programs
encoded as sequences of primitive.Value. Each Value is simultaneously an
instruction and an operand: C5 carries the opcode (control flow), C7 carries
the device address and affine operator, and C0–C3+C4:0 carry the 257-bit
GF(257) core state.

Execution walks the program array following the program counter (pc). At each
step the node's affine operator transforms the running phase, the RouteHint
selects a device dispatch, and the Opcode controls the pc advance:

	OpcodeNext    → pc++
	OpcodeJump    → pc += Jump()
	OpcodeBranch  → fork: evaluate all branches, pick lowest-residue winner
	OpcodeHalt    → stop, emit accumulated state as output
	OpcodeReset   → reset phase to identity, pc++

The accumulated output buffer collects terminal residues and branch winners.
Reading it back yields the execution result as a Value sequence.
*/
type InterpreterServer struct {
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	state   *errnie.State
	program []primitive.Value
	output  []primitive.Value
	loader  Loader
}

type interpreterOpts func(*InterpreterServer)

var state = errnie.NewState("vm/processor/interpreter")

/*
New allocates a new loader.
*/
func New() (Loader, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))

	if err != nil {
		return Loader{}, errnie.Error(
			NewInterpreterError(InterpreterErrorTypeAllocationFailed),
		)
	}

	loader, err := NewLoader(seg)

	if err != nil {
		return Loader{}, errnie.Error(
			NewInterpreterError(InterpreterErrorTypeAllocationFailed),
		)
	}

	return loader, nil
}

/*
NewInterpreterServer creates the interpreter processor.
*/
func NewInterpreterServer(opts ...interpreterOpts) *InterpreterServer {

	server := &InterpreterServer{
		state:   state,
		program: make([]primitive.Value, 0, 64),
		output:  make([]primitive.Value, 0, 16),
		loader: errnie.Guard(state, func() (Loader, error) {
			return New()
		}),
	}

	for _, opt := range opts {
		opt(server)
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":    server.ctx,
			"cancel": server.cancel,
		})
	})

	return server
}

func (loader Loader) Read(p []byte) (n int, err error) {
	buffer := bytes.NewBuffer(p)
	encoder := capnp.NewEncoder(buffer)

	errnie.GuardVoid(state, func() error {
		return encoder.Encode(loader.Message())
	})

	return buffer.Len(), nil
}

func (loader Loader) Write(p []byte) (n int, err error) {
	decoder := capnp.NewDecoder(bytes.NewBuffer(p))

	msg := errnie.Guard(state, func() (*capnp.Message, error) {
		return decoder.Decode()
	})

	root := errnie.Guard(state, func() (Loader, error) {
		return ReadRootLoader(msg)
	})

	values, err := root.Values()
	if err != nil {
		return 0, err
	}

	out, err := primitive.BuildValue(p)

	if err != nil {
		return 0, err
	}

	values.Set(values.Len(), out)

	return 0, nil
}

func (loader Loader) Close() error {
	return nil
}

/*
Client returns a Cap'n Proto client for this InterpreterServer.
*/
func (server *InterpreterServer) Client(_ string) capnp.Client {
	server.mu.Lock()
	defer server.mu.Unlock()

	return capnp.Client(Interpreter_ServerToClient(server))
}

/*
Read implements Interpreter_Server. Executes the accumulated program, then
streams the output Values back via the Callback capability.
*/
func (server *InterpreterServer) Read(
	ctx context.Context, call primitive.Service_read,
) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	callback := errnie.Guard(server.state, func() (
		primitive.Service_Callback, error,
	) {
		return primitive.Service_Callback(call.Args().Callback()), nil
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	server.execute()

	for range server.output {
		if err := callback.Send(ctx, func(
			params primitive.Service_Callback_send_Params,
		) error {
			return nil
		}); err != nil {
			return err
		}
	}

	_, release := callback.Done(ctx, nil)
	defer release()

	return nil
}

/*
Write implements Interpreter_Server. Appends one streamed Value to the
program buffer. Execution is deferred until Read or Close.
*/
func (server *InterpreterServer) Write(
	ctx context.Context, call primitive.Service_write,
) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	value := errnie.Guard(server.state, func() (primitive.Value, error) {
		return call.Args().Value()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	server.program = append(server.program, value)

	return nil
}

/*
Close implements Interpreter_Server. Executes the program if it hasn't been
read yet, then cancels the context.
*/
func (server *InterpreterServer) Close(
	ctx context.Context, call primitive.Service_close,
) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if len(server.output) == 0 && len(server.program) > 0 {
		server.execute()
	}

	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
execute runs the register machine over the program buffer. The running state
is a single phase value in GF(257). Each instruction node transforms the
phase through its affine operator, and the opcode controls the program
counter. The output buffer collects emitted Values.
*/
func (server *InterpreterServer) execute() {
	server.output = server.output[:0]

	if len(server.program) == 0 {
		return
	}

	phase := numeric.Phase(1)
	pc := 0

	for pc < len(server.program) {
		node := server.program[pc]
		opcode := primitive.Opcode(node.Opcode())

		phase = node.ApplyAffinePhase(phase)

		if from, to, ok := node.Trajectory(); ok {
			server.emitTrajectoryStep(node, phase, from, to)
		}

		switch opcode {
		case primitive.OpcodeHalt:
			server.emit(node, phase)
			return

		case primitive.OpcodeJump:
			jump := int(node.Jump())

			if jump == 0 {
				jump = 1
			}

			pc += jump

		case primitive.OpcodeBranch:
			winner := server.evaluateBranches(pc, node, phase)
			server.emit(winner, phase)
			pc++

		case primitive.OpcodeReset:
			phase = 1
			pc++

		default:
			if node.Terminal() {
				server.emit(node, phase)
			}

			pc++
		}

		if node.HasGuard() {
			if pc > 0 && pc-1 < len(server.program) {
				magnitude, err := node.TransitionMagnitude(server.program[pc-1])

				if err == nil && magnitude > numeric.Phase(node.GuardRadius()) {
					server.emit(node, phase)
					return
				}
			}
		}
	}
}

/*
emit snapshots the current execution state into an output Value. The running
phase is stamped into the shell so downstream consumers see the computed
result without re-executing.
*/
func (server *InterpreterServer) emit(node primitive.Value, phase numeric.Phase) {
	result, err := primitive.New()

	if err != nil {
		return
	}

	result.CopyFrom(node)
	result.SetStatePhase(phase)
	server.output = append(server.output, result)
}

/*
emitTrajectoryStep checks whether the current phase is advancing toward the
trajectory target. If the phase matches the target exactly, the node is
emitted as a trajectory completion.
*/
func (server *InterpreterServer) emitTrajectoryStep(
	node primitive.Value, phase numeric.Phase,
	from numeric.Phase, to numeric.Phase,
) {
	if phase == to {
		server.emit(node, phase)
	}
}

/*
evaluateBranches treats the N values following the current pc as candidate
branches. Each candidate is scored against the current node using the GF(257)
match evaluation. The branch with the highest fitness wins.
*/
func (server *InterpreterServer) evaluateBranches(
	pc int, node primitive.Value, phase numeric.Phase,
) primitive.Value {
	branchCount := int(node.Branches())

	if branchCount == 0 {
		branchCount = 1
	}

	end := pc + 1 + branchCount

	if end > len(server.program) {
		end = len(server.program)
	}

	candidates := server.program[pc+1 : end]

	if len(candidates) == 0 {
		return node
	}

	queryMask := primitive.BuildQueryMask(node)
	matches := primitive.BatchEvaluate(queryMask, candidates)

	bestIndex := 0
	bestFitness := matches[0].FitnessScore

	for idx := 1; idx < len(matches); idx++ {
		if matches[idx].FitnessScore > bestFitness {
			bestIndex = idx
			bestFitness = matches[idx].FitnessScore
		}
	}

	return candidates[bestIndex]
}

/*
Output returns the execution result buffer. Intended for direct Go callers
that bypass the RPC path.
*/
func (server *InterpreterServer) Output() []primitive.Value {
	server.mu.RLock()
	defer server.mu.RUnlock()

	return server.output
}

/*
Execute runs the program and returns the output for direct Go callers.
*/
func (server *InterpreterServer) Execute(program []primitive.Value) []primitive.Value {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.program = program
	server.execute()

	return server.output
}

/*
InterpreterWithContext sets a cancellable context.
*/
func InterpreterWithContext(ctx context.Context) interpreterOpts {
	return func(server *InterpreterServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
InterpreterErrorType enumerates typed interpreter failure modes.
*/
type InterpreterErrorType string

const (
	InterpreterErrorTypeProgramEmpty     InterpreterErrorType = "program buffer is empty"
	InterpreterErrorTypeDispatchFailed   InterpreterErrorType = "device dispatch failed"
	InterpreterErrorTypeGuardViolation   InterpreterErrorType = "guard radius exceeded"
	InterpreterErrorTypeAllocationFailed InterpreterErrorType = "allocation failed"
)

/*
InterpreterError carries a stable typed failure reason.
*/
type InterpreterError struct {
	Message string
	Err     InterpreterErrorType
}

/*
NewInterpreterError constructs a typed interpreter error.
*/
func NewInterpreterError(err InterpreterErrorType) *InterpreterError {
	return &InterpreterError{Message: string(err), Err: err}
}

/*
Error implements error for InterpreterError.
*/
func (err InterpreterError) Error() string {
	return fmt.Sprintf("interpreter error: %s: %s", err.Message, err.Err)
}
