package vm

import (
	"context"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
ALUExecute runs compiled program words on the compute substrate. Machine wires
this to Backend.Execute so Orchestrator stays free of compute imports.
*/
type ALUExecute func(frames []unsafe.Pointer) error

/*
linkerPair is one linker-emitted Value plus its link assets for firmware "link".
*/
type linkerPair struct {
	value  *primitive.Value
	assets []*programmer.Asset
}

/*
Orchestrator is a component that orchestrates the different components of the machine.
*/
type Orchestrator struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	queue    *pool.Queue
	firmware *programmer.Firmware
	linker   *Linker
	router   *Router
}

/*
NewOrchestrator creates a new orchestrator.

All pipeline work (firmware, route, field) is scheduled with queue.Submit only.
The caller does not wait on the pool; completion is observed via PassDone for
Prompt. A mutex inside each task serializes mutations to route and field when
pool workers would otherwise overlap.
*/
func NewOrchestrator(
	ctx context.Context,
	conn *gossip.Conn,
	queue *pool.Queue,
	alu ALUExecute,
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	orchestrator := &Orchestrator{
		ctx:      ctx,
		cancel:   cancel,
		queue:    queue,
		firmware: programmer.NewFirmware(),
		linker:   NewLinker(),
		router:   NewRouter(),
	}

	if err := validate.Require(map[string]any{
		"ctx":      orchestrator.ctx,
		"cancel":   orchestrator.cancel,
		"queue":    orchestrator.queue,
		"firmware": orchestrator.firmware,
		"linker":   orchestrator.linker,
		"router":   orchestrator.router,
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
Publish pushes Values on the linker, drains ready Pop pairs, and schedules one
pass on the queue. Always returns nil, nil; work is asynchronous.
*/
func (orchestrator *Orchestrator) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	orchestrator.linker.Push(values...)
	orchestrator.router.Route(values...)

	return nil, nil
}

/*
Cycle pushes optional Values, drains Pop, schedules one pass. Empty ingress
still advances the field on the worker. Returns nil, nil; work is asynchronous.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) ([]*primitive.Value, error) {
	orchestrator.linker.Push(values...)

	return nil, nil
}
