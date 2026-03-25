package vm

import (
	"context"
	"errors"
	"io"
	"runtime"

	"github.com/panjf2000/ants/v2"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/workflow"
)

/*
Machine is purely a convenience wrapper around a workflow,
which in itself if just a convenience wrapper around Value types.
The idea is that Values pass through Values, which activates the
second property of the Value type: behavior.
Machine reduces boilerplate, but is not an essential part of the
system's operational mechanics.
*/
type Machine struct {
	ctx      context.Context
	cancel   context.CancelFunc
	pool     *ants.Pool
	dataset  io.ReadCloser
	backend  kernel.Substrate
	prompt   io.ReadWriteCloser
	source   *workflow.MergeSource
	pipeline io.ReadWriter
	pump     *workflow.Pump
}

/*
machineOption is a function that can be used to configure a Machine.
*/
type machineOption func(*Machine)

/*
NewMachine creates a new Machine with the given options.
*/
func NewMachine(opts ...machineOption) (machine *Machine, err error) {
	prompt := primitive.NewValue()
	pump := workflow.NewPump()

	pool, err := ants.NewPool(runtime.NumCPU() - 1)

	if err != nil {
		return nil, errnie.Wrap(err, "vm.machine.NewMachine")
	}

	machine = &Machine{
		pool:    pool,
		backend: compute.NewBackend(),
		prompt:  prompt,
		source:  workflow.NewMergeSource(nil, pump.Loop()),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if err = validate.Require(map[string]any{
		"backend":  machine.backend,
		"prompt":   machine.prompt,
		"source":   machine.source,
		"pipeline": machine.pipeline,
		"pump":     machine.pump,
	}); err != nil {
		return nil, errnie.Wrap(err, "vm.machine.NewMachine")
	}

	// Feedback tees backend output into prompt (for Prompt()) and into the pump
	// ring (MergeSource loop) so each tick can consume the previous output.
	inner := workflow.NewPipeline(
		machine.source,
		primitive.NewValue(),
		workflow.NewFeedback(machine.backend, io.MultiWriter(
			&valueFeedbackWriter{dst: prompt},
			workflow.DrainWriter{W: pump.Sink()},
		)),
	)
	pump.Attach(inner)
	machine.pump = pump
	machine.pipeline = pump

	return machine
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	n, err = machine.pipeline.Read(p)
	errnie.Trace("vm.machine.Read", "n", n, "err", err)
	return n, err
}

// Write injects host bytes into the pipeline head (consumed before dataset
// and loop reads). Use this for prompts and steering input.
func (machine *Machine) Write(p []byte) (n int, err error) {
	n, err = machine.source.Write(p)
	errnie.Trace("vm.machine.Write", "n", n, "err", err)
	return n, err
}

// Prompt returns the feedback-fed loop substrate (same Value teed by Feedback).
// Prefer Machine.Write for injection; this is for direct Read/Write access.
func (machine *Machine) Prompt() io.ReadWriter {
	return machine.prompt
}

func (machine *Machine) Close() error {
	machine.cancel()
	machine.pool.Release()

	var errs error
	if machine.pump != nil {
		if err := machine.pump.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if machine.backend != nil {
		if err := machine.backend.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if machine.dataset != nil {
		if err := machine.dataset.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func WithContext(ctx context.Context) machineOption {
	return func(m *Machine) {
		m.ctx, m.cancel = context.WithCancel(ctx)
	}
}

func WithBackend(backend kernel.Substrate) machineOption {
	return func(m *Machine) {
		errnie.Info("vm.machine.WithBackend", "msg", "overriding backend")
		m.backend = backend
	}
}

// Backend returns the underlying kernel.Substrate for direct dispatch of
// vectorized operations (UniversalBitwise, MotorCompose, etc.) without
// going through the streaming pipeline.
func (machine *Machine) Backend() kernel.Substrate {
	return machine.backend
}

func WithDataset(dataset io.ReadCloser) machineOption {
	return func(m *Machine) {
		m.dataset = dataset
		m.source.SetDataset(dataset)
	}
}

// maxValueFeedbackBuf limits how much deframing buffer valueFeedbackWriter may
// hold (partial tail + queued raw bytes). It should exceed any plausible burst
// of full frames from the backend; tune if a backend streams longer runs.
const maxValueFeedbackBuf = 256 * primitive.ByteSize

// ErrValueFeedbackBufferFull is returned when appending to the feedback deframer
// would exceed maxValueFeedbackBuf.
var ErrValueFeedbackBufferFull = errors.New("vm: value feedback buffer cap exceeded")

// valueFeedbackWriter buffers bytes from Feedback's tee until full wire frames
// are available, then applies each via ApplyWireFrame. Backend output is
// serialized Value frames, not a raw byte stream for Value.Write (which would
// tokenize frame bytes and can hit divergence or chunk boundaries and report
// partial counts to io.MultiWriter).
type valueFeedbackWriter struct {
	dst *primitive.Value
	buf []byte
}

func (w *valueFeedbackWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(w.buf)+len(p) > maxValueFeedbackBuf {
		return 0, ErrValueFeedbackBufferFull
	}
	w.buf = append(w.buf, p...)
	for len(w.buf) >= primitive.ByteSize {
		frame := w.buf[:primitive.ByteSize]
		w.buf = w.buf[primitive.ByteSize:]
		if err := w.dst.ApplyWireFrame(frame); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}
