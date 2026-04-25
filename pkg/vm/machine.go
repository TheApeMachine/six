package vm

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Machine is a central orchestrator that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	host      *network.Host
	tokenizer *Tokenizer
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	community []*primitive.Value
	ephemeral map[uint64]struct{}
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		telemetry: bridge,
		backend:   compute.NewBackend(ctx),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"tokenizer": machine.tokenizer,
		"backend":   machine.backend,
	})
}

/*
Close the machine.
*/
func (machine *Machine) Close() error {
	var errs []error

	machine.cancel()

	if machine.telemetry != nil {
		if err := machine.telemetry.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.host != nil {
		if err := machine.host.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.tokenizer != nil {
		if err := machine.tokenizer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.backend != nil {
		if err := machine.backend.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

/*
Error implements the error interface.
*/
func (machine *Machine) Error() string {
	return machine.err.Error()
}

/*
Cycle executes one observed community tick through the backend.
Callers repeat it until continuations settle or a resolved Value emerges.
*/
func (machine *Machine) Cycle() (resolved []*primitive.Value, err error) {
	select {
	case <-machine.ctx.Done():
		return nil, machine.ctx.Err()
	default:
	}

	if machine.backend == nil || len(machine.community) == 0 {
		return nil, nil
	}

	machine.markEphemeral(machine.community)

	if err = machine.stageEncounters(); err != nil {
		return nil, err
	}

	if !machine.backend.Submit(machine.community) {
		if err = machine.publishTelemetry(machine.community); err != nil {
			return nil, err
		}

		machine.pruneExpiredEphemeral()

		return machine.resolvedValues(), nil
	}

	spawned := machine.backend.Sync(machine.ctx)
	if len(spawned) > 0 {
		machine.community = append(machine.community, spawned...)
		machine.markEphemeral(spawned)
		if err = machine.publishTelemetry(machine.community); err != nil {
			return nil, err
		}

		machine.pruneExpiredEphemeral()

		return spawned, nil
	}

	if err = machine.publishTelemetry(machine.community); err != nil {
		return nil, err
	}

	machine.pruneExpiredEphemeral()

	return machine.resolvedValues(), nil
}

/*
publishTelemetry writes full Value frames through the bridge after an observed
tick. Bridge-level fingerprints suppress unchanged frames, so this call can be
made once per cycle without recreating the old tight websocket loop.
*/
func (machine *Machine) publishTelemetry(values []*primitive.Value) error {
	if machine.telemetry == nil || !core.Cfg.TelemetryEnabled {
		return nil
	}

	var stackFrame [1024]byte
	frame := stackFrame[:]
	if core.Cfg.Value.Bytes > len(stackFrame) {
		frame = make([]byte, core.Cfg.Value.Bytes)
	} else {
		frame = stackFrame[:core.Cfg.Value.Bytes]
	}

	for _, value := range values {
		if value == nil {
			continue
		}

		n, readErr := value.Read(frame)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		if _, writeErr := machine.telemetry.Write(frame[:n]); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

/*
stageEncounters materializes the current peer view before the ALU tick.
The program still decides what to consume; this only performs the same
Value.Write staging that a Conn would perform when a resident meets a peer.
*/
func (machine *Machine) stageEncounters() error {
	if len(machine.community) < 2 {
		return nil
	}

	var stackFrame [1024]byte
	frame := stackFrame[:]
	if core.Cfg.Value.Bytes > len(stackFrame) {
		frame = make([]byte, core.Cfg.Value.Bytes)
	} else {
		frame = stackFrame[:core.Cfg.Value.Bytes]
	}

	for hostIdx, host := range machine.community {
		if host == nil || !host.ReadyForALU() {
			continue
		}

		peer := machine.peerAfter(hostIdx)
		if peer == nil {
			continue
		}

		n, readErr := peer.Read(frame)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		if _, writeErr := host.Write(frame[:n]); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

func (machine *Machine) peerAfter(hostIdx int) *primitive.Value {
	count := len(machine.community)
	for offset := 1; offset < count; offset++ {
		peer := machine.community[(hostIdx+offset)%count]
		if peer != nil {
			return peer
		}
	}

	return nil
}

func (machine *Machine) resolvedValues() []*primitive.Value {
	resolved := make([]*primitive.Value, 0)

	for _, value := range machine.community {
		if value == nil {
			continue
		}

		switch value.Status() {
		case primitive.DONE, primitive.RESOLVED:
			resolved = append(resolved, value)
		}
	}

	return resolved
}

func (machine *Machine) markEphemeral(values []*primitive.Value) {
	for _, value := range values {
		if value == nil || value.TTL() == 0 {
			continue
		}

		if machine.ephemeral == nil {
			machine.ephemeral = make(map[uint64]struct{})
		}

		machine.ephemeral[value.ID()] = struct{}{}
	}
}

func (machine *Machine) pruneExpiredEphemeral() {
	if len(machine.ephemeral) == 0 || len(machine.community) == 0 {
		return
	}

	kept := machine.community[:0]
	for _, value := range machine.community {
		if value == nil {
			continue
		}

		id := value.ID()
		if _, ok := machine.ephemeral[id]; ok && value.TTL() == 0 {
			delete(machine.ephemeral, id)
			value.Close()
			continue
		}

		kept = append(kept, value)
	}

	clear(machine.community[len(kept):])
	machine.community = kept
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then runs Cycle.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	var segments []*primitive.Value

	for sample := range dataset.Generate() {
		if segments, err = machine.tokenizer.IngestSample(
			machine.ctx, sample,
		); err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		machine.community = append(machine.community, segments...)
	}

	if _, err := machine.Cycle(); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	return nil
}

/*
Prompt injects the prompt segment Values into the community and cycles until settled.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (
	resolved []*primitive.Value, err error,
) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errors.Join(machine.err, errnie.Error(err))
	}

	machine.community = append(machine.community, values...)

	done := false

	for !done {
		if resolved, err = machine.Cycle(); err != nil {
			return nil, errors.Join(machine.err, errnie.Error(err))
		}

		if len(resolved) > 0 {
			done = true
		}
	}

	return resolved, nil
}
