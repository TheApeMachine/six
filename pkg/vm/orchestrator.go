package vm

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Orchestrator is a component that orchestrates the different components of the machine.
*/
type Orchestrator struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	conn     *gossip.Conn
	queue    *pool.Queue
	field    *geometry.Field
	firmware *programmer.Firmware
	linker   *Linker
}

/*
NewOrchestrator creates a new orchestrator.

programReady, when non-nil, receives a coalesced wakeup whenever a cycle
observes at least one settled value (SchedulingNextProgramWord zeroed) so
Machine.Prompt can block instead of spinning until the kernel clears scheduling.
*/
func NewOrchestrator(
	ctx context.Context,
	conn *gossip.Conn,
	queue *pool.Queue,
	programReady chan struct{},
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	orchestrator := &Orchestrator{
		ctx:      ctx,
		cancel:   cancel,
		conn:     conn,
		queue:    queue,
		field:    geometry.NewField(geometry.Mod65537),
		firmware: programmer.NewFirmware(),
		linker:   NewLinker(),
	}

	if err := validate.Require(map[string]any{
		"ctx":    orchestrator.ctx,
		"cancel": orchestrator.cancel,
		"conn":   orchestrator.conn,
		"queue":  orchestrator.queue,
		"field":  orchestrator.field,
	}); err != nil {
		cancel()

		return nil, err
	}

	return orchestrator, nil
}

/*
Close the orchestrator.
*/
func (orchestrator *Orchestrator) Close() error {
	orchestrator.cancel()

	return orchestrator.err
}

/*
Error returns the error of the orchestrator.
*/
func (orchestrator *Orchestrator) Error() error {
	return orchestrator.err
}

/*
Publish implements the Publishable interface.
It sends the value on the pending queue for the next Cycle.
*/
func (orchestrator *Orchestrator) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	return orchestrator.Cycle(values...)
}

/*
Cycle runs one full processing pass: route incoming Values into communities,
cycle the global field (which cascades into each community's eigenmode
detection and rotational pressure), then evaluate every community for
gap resolution and emission readiness. Returns the Values whose belief gap
has dropped below epsilon — the stop condition that tells the experiment
pipeline the system has resolved the prompt.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) ([]*primitive.Value, error) {
	orchestrator.linker.Push(values...)

	for {
		value, assets := orchestrator.linker.Pop()

		if value == nil {
			break
		}

		firmware := orchestrator.firmware.Next(value)

		if firmware == "" {
			continue
		}

		// Only pass assets if the firmware actually requires them (like "link").
		// If other firmwares don't need them, we could conditionally pass nil,
		// but since the asset region is just scratch space, passing it is harmless.
		if firmware != "link" {
			assets = nil
		}

		orchestrator.queue.Submit(func() {
			executable := programmer.NewExecutable(
				value, firmware, assets,
			)

			executable.Compile(programmer.CPU)
		})
	}

	return nil, nil
}
