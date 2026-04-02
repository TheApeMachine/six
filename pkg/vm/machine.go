package vm

import (
	"context"
	"errors"
	"io"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/cluster"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Machine provides a unified stream processing pipeline. All machine options are
applied during construction so tests and callers can instantiate isolated
machines without relying on package-level state.
*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	backend      *compute.Backend
	sources      io.Reader
	feedback     io.Writer
	destinations io.Writer
	output       io.ReadWriter
	controlplane *cluster.ControlPlane
	tokenizer    *Tokenizer
}

type machineOption func(*Machine)

const (
	promptSettleDeadline = 50 * time.Millisecond
	promptSettlePoll     = 500 * time.Microsecond
	promptStableSamples  = 2
)

/*
NewMachine constructs a new Machine with the provided options.
It requires a context for lifecycle management and will return
an error if the context is invalid or if the underlying stream
fails to start. The machine can be configured with various options,
such as custom datasets, stream adapters, and region counts.
*/
func NewMachine(
	ctx context.Context, opts ...machineOption,
) (machine *Machine, err error) {
	ctx, cancel := context.WithCancel(ctx)

	machine = &Machine{
		ctx:          ctx,
		cancel:       cancel,
		backend:      compute.NewBackend(ctx),
		controlplane: cluster.NewControlPlane(ctx),
	}

	tokenizer, err := NewTokenizer(
		ctx,
		TokenizerWithStore(machine.controlplane),
	)

	if err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, err),
		)
	}

	machine.sources = tokenizer
	machine.tokenizer = tokenizer
	machine.destinations = tokenizer

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":          machine.ctx,
		"cancel":       machine.cancel,
		"sources":      machine.sources,
		"destinations": machine.destinations,
		"backend":      machine.backend,
		"tokenizer":    machine.tokenizer,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, machine.err),
		)
	}

	machine.start()

	return machine, nil
}

/*
start the machine as a single in-band loop:
  - sources are serialized in machine.sources
  - each read queues the frame, then writes it through machine.Write

io.Copy uses a 32KiB buffer by default; Tokenizer.Read and primitive.Value
serialization require a buffer of at least core.Cfg.Value.Bytes. When
value.bytes is larger than that, the copy loop must use io.CopyBuffer or
Read returns io.ErrShortBuffer.
*/
func (machine *Machine) start() (err error) {
	frameBytes := core.Cfg.Value.Bytes
	copyBufSize := 32 * 1024
	if frameBytes > copyBufSize {
		copyBufSize = frameBytes
	}

	copyBuf := make([]byte, copyBufSize)

	go func() {
		for {
			select {
			case <-machine.ctx.Done():
				return
			default:
				_, machine.err = io.CopyBuffer(machine, machine, copyBuf)

				if machine.err == nil || errors.Is(machine.err, io.EOF) {
					machine.err = nil
					continue
				}

				if machine.ctx.Err() != nil {
					machine.err = nil
					return
				}

				machine.err = errnie.Error(
					NewMachineError(ErrStreamFailed, machine.err),
				)
				return
			}
		}
	}()

	return nil
}

/*
Read implements io.Reader so the machine acts as a wiring mechanism between
the tokenizer stream and queue machinery. Frames emitted by prompts can be
optionally copied to machine.feedback for an explicit loopback path.
*/
func (machine *Machine) Read(p []byte) (n int, err error) {
	select {
	case <-machine.ctx.Done():
		return 0, machine.ctx.Err()
	default:
		if n, err = machine.sources.Read(p); err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}

			return n, errnie.Error(err)
		}

		if n == 0 {
			return n, err
		}

		value := primitive.BytesToValue(p[:n])
		if err = machine.backend.Queue(unsafe.Pointer(value)); err != nil {
			return 0, errnie.Error(err)
		}

		isPrompt := value.IsPrompt()
		if isPrompt {
			machine.waitForPrompt(value)
		}

		n, err = value.Read(p)

		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}

		// Value.Read returns io.EOF after copying a full wire frame; keep the
		// caller surface aligned with typical io.Reader success (n, nil).
		if errors.Is(err, io.EOF) {
			err = nil
		}

		if isPrompt {
			tokenIDs := machine.controlplane.LookupKeysByValue(value)

			if len(tokenIDs) == 0 {
				valueID := value.GetWord(core.Cfg.Value.Region.ID.Start)
				tokenIDs = machine.controlplane.LookupKeysByValueID(valueID)
			}

			if len(tokenIDs) == 0 {
				return n, errnie.Error(errors.New("no token IDs found for prompt"))
			}

			output := primitive.DecodeTokenIDs(tokenIDs)

			if machine.output != nil {
				if _, writeErr := machine.output.Write(output); writeErr != nil {
					return n, errnie.Error(writeErr)
				}
			}

			if machine.feedback != nil {
				if _, writeErr := machine.feedback.Write(p[:n]); writeErr != nil {
					return n, errnie.Error(writeErr)
				}
			}
		}
	}

	return n, err
}

func (machine *Machine) waitForPrompt(value *primitive.Value) {
	if machine == nil || value == nil {
		return
	}

	deadline := time.Now().Add(promptSettleDeadline)
	snapshot := *value
	sawChange := false
	stable := 0

	for time.Now().Before(deadline) {
		if machine.ctx.Err() != nil {
			return
		}

		current := *value
		if current == snapshot {
			stable++
			if sawChange && stable >= promptStableSamples {
				return
			}
		} else {
			snapshot = current
			sawChange = true
			stable = 0
		}

		time.Sleep(promptSettlePoll)
	}
}

/*
Write implements io.Writer so the machine acts as a wiring mechanism between
sources and destinations. Destinations can still aggregate outputs via
io.MultiWriter for fan-out.
*/
func (machine *Machine) Write(p []byte) (n int, err error) {
	if machine.destinations == nil {
		return len(p), nil
	}

	return machine.destinations.Write(p)
}

/*
Close implements io.Closer so the machine can be closed, which will cancel
the context, and if everything is wired up correctly, this should trigger
a full system-wide shutdown. This means that the system's context must be
the ultimate root context for the system.
*/
func (machine *Machine) Close() (err error) {
	if machine.cancel != nil {
		machine.cancel()
	}

	return err
}

/*
WithSources configures the machine with one or more sources, which act as
the ingress points for data.
*/
func WithSources(readers ...io.Reader) machineOption {
	return func(machine *Machine) {
		machine.sources = io.MultiReader(
			append([]io.Reader{machine.sources}, readers...)...,
		)
	}
}

/*
WithFeedback configures an additional prompt-output sink.
Prompt Values can be written to this output to build an explicit feedback
ingress path.
*/
func WithFeedback(writers ...io.Writer) machineOption {
	return func(machine *Machine) {
		if len(writers) == 0 {
			return
		}

		machine.feedback = io.MultiWriter(
			append([]io.Writer{}, writers...)...,
		)
	}
}

/*
WithDestinations configures the machine with one or more destinations,
which act as the egress points for data.
*/
func WithDestinations(writers ...io.Writer) machineOption {
	return func(machine *Machine) {
		machine.destinations = io.MultiWriter(
			append([]io.Writer{machine.destinations}, writers...)...,
		)
	}
}

/*
WithOutput configures the machine with an output reader, which is used to
capture the output of the machine.
*/
func WithOutput(rw io.ReadWriter) machineOption {
	return func(machine *Machine) {
		machine.output = rw
	}
}
