package vm

import (
	"bufio"
	"context"
	"io"
	"unsafe"

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
	destinations io.Writer
	values       []*primitive.Value
}

type machineOption func(*Machine)

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
	if ctx == nil {
		return nil, errnie.Error(
			NewMachineError(ErrNoContext),
		)
	}

	machine = &Machine{}
	machine.ctx, machine.cancel = context.WithCancel(ctx)
	machine.backend = compute.NewBackend()

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":     machine.ctx,
		"cancel":  machine.cancel,
		"sources": machine.sources,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, machine.err),
		)
	}

	machine.start()

	return machine, nil
}

func (machine *Machine) start() (err error) {
	scanner := bufio.NewScanner(machine.sources)
	// Buffer sized to the token region capacity: TokenBits/8 bytes minus
	// 3 reserved words (ValueID, PrevID, NextID) × 8 bytes each.
	tokenBytes := int(core.Cfg.Value.Region.Tokens.Bits/8) - 3*8
	scanner.Buffer(make([]byte, tokenBytes), tokenBytes)

	for {
		select {
		case <-machine.ctx.Done():
			return nil
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return errnie.Error(
						NewMachineError(ErrDatasetNotClosed, err),
					)
				}

				return nil
			}

			line := scanner.Bytes()

			if len(line) == 0 {
				continue
			}

			var value *primitive.Value

			if value, err = primitive.NewValue(line); err != nil {
				_ = errnie.Error(err)
				continue
			}

			// README Signals: bitwise work runs on copies; cut points live in the
			// workspace after the kernel returns. Canonical Value is not mutated.
			var workSelf, workPartner primitive.Value
			primitive.CopyFrame(&workSelf, value)
			primitive.CopyFrame(&workPartner, value)
			workSelf.InstallLearnFirmware()

			if err = machine.backend.UniversalBitwise(
				unsafe.Pointer(&workSelf),
				unsafe.Pointer(&workPartner),
			); err != nil {
				_ = errnie.Error(err)
				continue
			}

			st := StructureFromWorkspace(StructureKindLearnCancel, value, &workSelf)
			st.RegisterDefaultLSM()
		}
	}
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.sources.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	if machine.destinations == nil {
		return len(p), nil
	}

	return machine.destinations.Write(p)
}

func (machine *Machine) Close() (err error) {
	if machine.cancel != nil {
		machine.cancel()
	}

	return err
}

func WithSources(readers io.ReadCloser) machineOption {
	return func(machine *Machine) {
		machine.sources = io.LimitReader(io.MultiReader(
			readers,
		), int64(core.Cfg.Value.Bytes))
	}
}

func WithDestinations(writers io.WriteCloser) machineOption {
	return func(machine *Machine) {
		machine.destinations = io.MultiWriter(
			writers,
		)
	}
}
