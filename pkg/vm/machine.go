package vm

import (
	"context"
	"errors"
	"fmt"
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

const maxRecruitmentIterations = 4096

/*
Machine is the central runtime that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx              context.Context
	cancel           context.CancelFunc
	err              error
	host             *network.Host
	tokenizer        *Tokenizer
	backend          *compute.Backend
	telemetry        *telemetry.Bridge
	telemetryCopyBuf []byte
	community        []*primitive.Value
	ephemeral        map[uint64]struct{}
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	if core.Cfg == nil || len(core.Cfg.Programs) == 0 {
		_ = core.LoadDefaultConfig()
		core.NewConfig()
	}

	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:              ctx,
		cancel:           cancel,
		telemetry:        bridge,
		telemetryCopyBuf: make([]byte, 32*1024),
		backend:          compute.NewBackend(ctx),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		machine.Close()

		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
		machine.Close()
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

	if machine == nil {
		return nil
	}

	if machine.cancel != nil {
		machine.cancel()
	}

	if len(machine.community) > 0 {
		primitive.CloseAll(machine.community)
		clear(machine.community)
		machine.community = nil
	}

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
	if machine == nil || machine.err == nil {
		return ""
	}

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

	machine.ensureCommunityRecruiter()
	machine.markEphemeral(machine.community)

	owner := machine.nextReadyValue()
	if owner != nil {
		if err := machine.backend.Submit(owner, machine.community); err != nil {
			return nil, errnie.Error(err)
		}
	}

	for spawned := range machine.backend.Sync(machine.ctx) {
		if spawned == nil {
			continue
		}

		machine.community = append(machine.community, spawned)
	}

	if err = machine.publishTelemetry(machine.community); err != nil {
		return nil, err
	}

	machine.pruneExpiredEphemeral()

	return machine.resolvedValues(), nil
}

func (machine *Machine) publishTelemetry(values []*primitive.Value) error {
	if machine.telemetry == nil || !core.Cfg.TelemetryEnabled {
		return nil
	}

	for _, value := range values {
		if value == nil {
			continue
		}

		if _, err := io.CopyBuffer(machine.telemetry, value, machine.telemetryCopyBuf); err != nil {
			return err
		}
	}

	return nil
}

func (machine *Machine) ensureCommunityRecruiter() bool {
	if machine == nil || len(machine.community) == 0 || core.Cfg == nil {
		return false
	}
	if machine.nextReadyValue() != nil {
		return false
	}

	seed := machine.firstUnassignedCommunityValue()
	if seed == nil {
		return false
	}

	recruiter := primitive.Emit(primitive.WithFirmware(core.RECRUIT_COMMUNITY))
	if !recruiter.ReadyForALU() {
		recruiter.Close()
		return false
	}

	copy(recruiter.Get(primitive.AffinityRegion), seed.Get(primitive.AffinityRegion))
	recruiter.NormalizeAffinity()
	machine.community = append(machine.community, recruiter)

	return true
}

func (machine *Machine) nextReadyValue() *primitive.Value {
	for _, value := range machine.community {
		if value != nil && value.ReadyForALU() {
			return value
		}
	}

	return nil
}

func (machine *Machine) firstUnassignedCommunityValue() *primitive.Value {
	for _, value := range machine.community {
		if value == nil || value.HasProgram() {
			continue
		}

		community, err := value.Property(primitive.COMMUNITY)
		if err != nil || community != 0 {
			continue
		}

		return value
	}

	return nil
}

func (machine *Machine) unassignedCommunityValues() int {
	count := 0
	for _, value := range machine.community {
		if value == nil || value.HasProgram() {
			continue
		}

		community, err := value.Property(primitive.COMMUNITY)
		if err == nil && community == 0 {
			count++
		}
	}

	return count
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

		if _, ok := machine.ephemeral[value.ID()]; ok && valueExpired(value) {
			delete(machine.ephemeral, value.ID())
			value.Close()
			continue
		}

		kept = append(kept, value)
	}

	clear(machine.community[len(kept):])
	machine.community = kept
}

func valueExpired(value *primitive.Value) bool {
	ttl := value.TTL()
	return ttl == 0 || ttl == compute.TTLExpiredSentinel()
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then cycles recruitment until the
community word stops changing.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	for sample := range dataset.Generate() {
		segments, err := machine.tokenizer.IngestSample(
			machine.ctx, sample,
		)
		if err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		machine.community = append(machine.community, segments...)
	}

	for iterations := 0; ; iterations++ {
		if iterations >= maxRecruitmentIterations {
			return errors.Join(
				machine.err,
				errnie.Error(fmt.Errorf(
					"vm: recruitment made no progress after %d iterations (unassigned=%d)",
					maxRecruitmentIterations,
					machine.unassignedCommunityValues(),
				)),
			)
		}

		before := machine.unassignedCommunityValues()
		if before == 0 {
			return nil
		}

		if _, err := machine.Cycle(); err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		if machine.unassignedCommunityValues() >= before {
			return nil
		}
	}
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
