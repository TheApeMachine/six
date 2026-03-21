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
/*
ExecutionStep records one instruction's effect during execution. The trace
is the raw material for reification: a successful trace between two
boundary states can be condensed into a single affine macro operator.
*/
type ExecutionStep struct {
	PC          int
	Opcode      primitive.Opcode
	PhaseBefore numeric.Phase
	PhaseAfter  numeric.Phase
	Value       primitive.Value
}

type InterpreterServer struct {
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	state   *errnie.State
	program []primitive.Value
	output  []primitive.Value
	trace   []ExecutionStep
	loader  *interpreterLoader
}

/*
interpreterLoader frames Cap'n Proto Loader roots with dedicated errnie state
so concurrent Loader io does not share a package-level error sink.
*/
type interpreterLoader struct {
	state *errnie.State
	root  Loader
}

type interpreterOpts func(*InterpreterServer)

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
func NewInterpreterServer(opts ...interpreterOpts) (*InterpreterServer, error) {
	loaderState := errnie.NewState("vm/processor/interpreter.loader")

	root, err := New()

	if err != nil {
		return nil, err
	}

	server := &InterpreterServer{
		state:   errnie.NewState("vm/processor/interpreter"),
		program: make([]primitive.Value, 0, 64),
		output:  make([]primitive.Value, 0, 16),
		trace:   make([]ExecutionStep, 0, 64),
		loader: &interpreterLoader{
			state: loaderState,
			root:  root,
		},
	}

	for _, opt := range opts {
		opt(server)
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":    server.ctx,
			"cancel": server.cancel,
			"loader": server.loader,
		})
	})

	if server.state.Failed() {
		return nil, server.state.Err()
	}

	return server, nil
}

func (il *interpreterLoader) Read(p []byte) (n int, err error) {
	var buf bytes.Buffer
	encoder := capnp.NewEncoder(&buf)

	errnie.GuardVoid(il.state, func() error {
		return encoder.Encode(il.root.Message())
	})

	if il.state.Failed() {
		return 0, il.state.Err()
	}

	return copy(p, buf.Bytes()), nil
}

func (il *interpreterLoader) Write(p []byte) (n int, err error) {
	decoder := capnp.NewDecoder(bytes.NewReader(p))

	msg := errnie.Guard(il.state, func() (*capnp.Message, error) {
		return decoder.Decode()
	})

	if il.state.Failed() {
		return 0, il.state.Err()
	}

	incoming := errnie.Guard(il.state, func() (Loader, error) {
		return ReadRootLoader(msg)
	})

	if il.state.Failed() {
		return 0, il.state.Err()
	}

	incList, err := incoming.Values()

	if err != nil {
		return 0, err
	}

	incLen := incList.Len()

	if incLen == 0 {
		return len(p), nil
	}

	prevLen := 0

	var curList primitive.Value_List

	if il.root.HasValues() {
		curList, err = il.root.Values()

		if err != nil {
			return 0, err
		}

		prevLen = curList.Len()
	}

	total := prevLen + incLen

	merged, err := primitive.NewValue_List(il.root.Segment(), int32(total))

	if err != nil {
		return 0, err
	}

	for idx := 0; idx < prevLen; idx++ {
		dst := merged.At(idx)
		dst.CopyFrom(curList.At(idx))
	}

	for idx := 0; idx < incLen; idx++ {
		dst := merged.At(prevLen + idx)
		dst.CopyFrom(incList.At(idx))
	}

	if err := il.root.SetValues(merged); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (il *interpreterLoader) Close() error {
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

	if err := server.execute(); err != nil {
		return err
	}

	for _, outValue := range server.output {
		sendValue := outValue

		if err := callback.Send(ctx, func(
			params primitive.Service_Callback_send_Params,
		) error {
			destination, err := params.NewValue()

			if err != nil {
				return err
			}

			destination.CopyFrom(sendValue)

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
		if err := server.execute(); err != nil {
			return err
		}
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
func (server *InterpreterServer) execute() error {
	server.output = server.output[:0]
	server.trace = server.trace[:0]

	if len(server.program) == 0 {
		return nil
	}

	phase := numeric.Phase(1)
	pc := 0

	for pc < len(server.program) {
		node := server.program[pc]
		opcode := primitive.Opcode(node.Opcode())

		phaseBefore := phase
		phase = node.ApplyAffinePhase(phase)

		server.trace = append(server.trace, ExecutionStep{
			PC:          pc,
			Opcode:      opcode,
			PhaseBefore: phaseBefore,
			PhaseAfter:  phase,
			Value:       node,
		})

		if from, to, ok := node.Trajectory(); ok {
			if err := server.emitTrajectoryStep(node, phase, from, to); err != nil {
				return err
			}
		}

		switch opcode {
		case primitive.OpcodeHalt:
			if err := server.emit(node, phase); err != nil {
				return err
			}

			return nil

		case primitive.OpcodeJump:
			jump := int(node.Jump())

			if jump == 0 {
				return fmt.Errorf(
					"interpreter: jump offset is zero (pc=%d)", pc)
			}

			pc += jump

		case primitive.OpcodeBranch:
			winner, err := server.evaluateBranches(pc, node, phase)

			if err != nil {
				return err
			}

			if err := server.emit(winner, phase); err != nil {
				return err
			}

			pc++

		case primitive.OpcodeReset:
			phase = 1
			pc++

		case primitive.OpcodeMacro:
			macroScale, macroTranslate, ok := node.MacroAffine()
			if !ok {
				return fmt.Errorf(
					"interpreter: macro opcode without affine data (pc=%d)", pc)
			}

			phase = numeric.Phase(numeric.MersenneReduce(
				uint32(macroScale)*uint32(phase) + uint32(macroTranslate),
			))

			if node.Terminal() {
				if err := server.emit(node, phase); err != nil {
					return err
				}
			}

			pc++

		default:
			if node.Terminal() {
				if err := server.emit(node, phase); err != nil {
					return err
				}
			}

			pc++
		}

		if node.HasGuard() {
			if pc > 0 && pc-1 < len(server.program) {
				magnitude, err := node.TransitionMagnitude(server.program[pc-1])

				if err == nil && magnitude > numeric.Phase(node.GuardRadius()) {
					if emitErr := server.emit(node, phase); emitErr != nil {
						return emitErr
					}

					return nil
				}
			}
		}
	}

	return nil
}

/*
emit snapshots the current execution state into an output Value. The running
phase is stamped into the shell so downstream consumers see the computed
result without re-executing.
*/
func (server *InterpreterServer) emit(node primitive.Value, phase numeric.Phase) error {
	result, err := primitive.New()

	if err != nil {
		return errnie.Error(err)
	}

	result.CopyFrom(node)
	result.SetStatePhase(phase)
	server.output = append(server.output, result)

	return nil
}

/*
emitTrajectoryStep checks whether the current phase is advancing toward the
trajectory target. If the phase matches the target exactly, the node is
emitted as a trajectory completion.
*/
func (server *InterpreterServer) emitTrajectoryStep(
	node primitive.Value, phase numeric.Phase,
	from numeric.Phase, to numeric.Phase,
) error {
	_ = from

	if phase == to {
		return server.emit(node, phase)
	}

	return nil
}

/*
evaluateBranches treats the N values following the current pc as candidate
branches. Each candidate is scored against the current node using the GF(257)
match evaluation. The branch with the highest fitness wins.
*/
func (server *InterpreterServer) evaluateBranches(
	pc int, node primitive.Value, phase numeric.Phase,
) (primitive.Value, error) {
	_ = phase

	branchCount := int(node.Branches())

	if branchCount == 0 {
		return primitive.Value{}, fmt.Errorf(
			"interpreter: branch count is zero (pc=%d)", pc)
	}

	end := pc + 1 + branchCount

	if end > len(server.program) {
		end = len(server.program)
	}

	candidates := server.program[pc+1 : end]

	if len(candidates) == 0 {
		return primitive.Value{}, fmt.Errorf(
			"interpreter: branch has no candidate slots (pc=%d want=%d have=%d)",
			pc, branchCount, len(server.program)-pc-1)
	}

	queryMask, err := primitive.New()
	if err != nil {
		return primitive.Value{}, err
	}

	if err := primitive.BuildQueryMaskInto(&queryMask, node); err != nil {
		return primitive.Value{}, err
	}

	matchScratch := make([]primitive.MatchResult, len(candidates))

	for index := range matchScratch {
		residue, err := primitive.New()
		if err != nil {
			return primitive.Value{}, err
		}

		matchScratch[index].Residue = residue
	}

	matches, err := primitive.BatchEvaluateInto(queryMask, candidates, matchScratch)
	if err != nil {
		return primitive.Value{}, err
	}

	if len(matches) == 0 {
		return primitive.Value{}, fmt.Errorf(
			"interpreter: batch evaluate returned no matches (pc=%d candidates=%d)",
			pc, len(candidates))
	}

	if len(matches) != len(candidates) {
		return primitive.Value{}, fmt.Errorf(
			"interpreter: batch evaluate length mismatch (pc=%d matches=%d candidates=%d)",
			pc, len(matches), len(candidates))
	}

	bestIndex := 0
	bestFitness := matches[0].FitnessScore

	for idx := 1; idx < len(matches); idx++ {
		if matches[idx].FitnessScore > bestFitness {
			bestIndex = idx
			bestFitness = matches[idx].FitnessScore
		}
	}

	return candidates[bestIndex], nil
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
func (server *InterpreterServer) Execute(
	program []primitive.Value,
) ([]primitive.Value, error) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.program = program

	if err := server.execute(); err != nil {
		return nil, err
	}

	return server.output, nil
}

/*
Trace returns the execution trace from the most recent run. Each step
records the pc, opcode, phase-before, and phase-after so callers can
analyze the execution path and derive a reified operator.
*/
func (server *InterpreterServer) Trace() []ExecutionStep {
	server.mu.RLock()
	defer server.mu.RUnlock()

	return server.trace
}

/*
ReifyTrace condenses the most recent execution trace into a single macro
operator Value. The composite affine transform is computed by chaining
each step's phase transformation: the start phase is the first step's
PhaseBefore and the end phase is the last step's PhaseAfter. The resulting
Value carries OpcodeMacro and the composed affine operator.

Returns an empty Value and false if the trace is empty or all steps are
identity transforms.
*/
func (server *InterpreterServer) ReifyTrace() (primitive.Value, bool) {
	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.trace) == 0 {
		return primitive.Value{}, false
	}

	startPhase := server.trace[0].PhaseBefore
	endPhase := server.trace[len(server.trace)-1].PhaseAfter

	if startPhase == endPhase {
		return primitive.Value{}, false
	}

	scale := numeric.Phase(1)
	translate := numeric.Phase(0)

	for _, step := range server.trace {
		stepScale, stepTranslate := step.Value.Affine()

		composedScale := numeric.MersenneReduce(
			uint32(scale) * uint32(stepScale),
		)

		composedTranslate := numeric.MersenneReduce(
			uint32(scale)*uint32(stepTranslate) + uint32(translate),
		)

		scale = numeric.Phase(composedScale)
		translate = numeric.Phase(composedTranslate)
	}

	keyBlocks := make([]uint64, 0)

	for _, step := range server.trace {
		for blockIdx := 0; blockIdx < len(keyBlocks) || blockIdx == 0; blockIdx++ {
			keyBlocks = append(keyBlocks, step.Value.Block(blockIdx))
		}
	}

	value, err := primitive.EncodeMacroOperator(scale, translate, keyBlocks)
	if err != nil {
		return primitive.Value{}, false
	}

	return value, true
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
	return &InterpreterError{Err: err}
}

/*
Error implements error for InterpreterError.
*/
func (err InterpreterError) Error() string {
	if err.Message != "" && err.Message != string(err.Err) {
		return fmt.Sprintf("interpreter error: %s (%s)", err.Err, err.Message)
	}

	return fmt.Sprintf("interpreter error: %s", err.Err)
}
