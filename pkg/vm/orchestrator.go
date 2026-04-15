package vm

import (
	"context"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
Orchestrator is the single rule evaluator for the Value pipeline. For each
Value it evaluates config rules. When a rule matches it wraps the Value
in an Executable with a Finalizer and submits a task to the pool. The pool
worker produces the Executable, the Backend compiles it for the picked
substrate, executes, and calls the Finalizer. The Finalizer re-enters
submitStep so the next rule can fire. When no rule matches the Value is
routed to a community field.
*/
type Orchestrator struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	queue    *pool.Queue
	firmware *programmer.Firmware
	router   *Router
	inbox    *data.Ring
	lastID   uint64
	enqueued atomic.Uint64
	draining atomic.Uint32
}

/*
NewOrchestrator creates a new orchestrator wired to the queue.
*/
func NewOrchestrator(
	ctx context.Context,
	conn *gossip.Conn,
	queue *pool.Queue,
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	orchestrator := &Orchestrator{
		ctx:      ctx,
		cancel:   cancel,
		queue:    queue,
		firmware: programmer.NewFirmware(),
		router:   NewRouter(),
	}

	orchestrator.inbox, orchestrator.err = data.NewRing(ctx, data.RingCapacity)

	if orchestrator.err != nil {
		cancel()
		return nil, orchestrator.err
	}

	if err := validate.Require(map[string]any{
		"ctx":      orchestrator.ctx,
		"cancel":   orchestrator.cancel,
		"queue":    orchestrator.queue,
		"firmware": orchestrator.firmware,
		"router":   orchestrator.router,
		"inbox":    orchestrator.inbox,
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

	if orchestrator.inbox != nil {
		_ = orchestrator.inbox.Close()
	}

	return orchestrator.err
}

/*
Error returns the error of the orchestrator.
*/
func (orchestrator *Orchestrator) Error() error {
	return orchestrator.err
}

func (orchestrator *Orchestrator) publishPrepared(value *primitive.Value) {
	if orchestrator == nil || value == nil {
		return
	}

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.QueueSubmitEvent(
			1,
			value,
			value.String(),
		))
		viz.PublishWireValueFrame(value.ID(), value.Bytes())
	}

	orchestrator.publishExecuted(value)
}

/*
Publish stages the predecessor ID into the asset region so the link program
can copy it into prev, then hands the Value to the rule evaluator.
*/
func (orchestrator *Orchestrator) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	for index, value := range values {
		if value == nil {
			continue
		}

		assetStart, _ := primitive.AssetRegion.WordExtent()
		previousID := orchestrator.lastID
		nextID := uint64(0)

		if index+1 < len(values) && values[index+1] != nil {
			nextID = values[index+1].ID()
		}

		if viz.DefaultBus.IsActive() {
			viz.DefaultBus.Publish(viz.QueueSubmitEvent(
				int64(len(values)),
				value,
				value.String(),
			))
		}

		value.Set(assetStart, previousID)
		value.Set(assetStart+1, nextID)
		orchestrator.lastID = value.ID()
		orchestrator.publishPrepared(value)
	}

	return nil, nil
}

/*
Cycle is the prompt-time heartbeat. Values re-enter the rule evaluator.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) ([]*primitive.Value, error) {
	for _, value := range values {
		if value == nil {
			continue
		}

		orchestrator.publishExecuted(value)
	}

	return nil, nil
}

/*
publishExecuted publishes a Value back to the orchestrator's single-writer
rule lane. Backend workers may call this concurrently; the router and
community fields are only touched by drainInbox.
*/
func (orchestrator *Orchestrator) publishExecuted(value *primitive.Value) {
	if orchestrator == nil || value == nil {
		return
	}

	for !orchestrator.inbox.Push(unsafe.Pointer(value)) {
		runtime.Gosched()
	}

	orchestrator.enqueued.Add(1)
	orchestrator.drainInbox()
}

/*
drainInbox serializes rule evaluation and routing without taking a mutex.
The generation check closes the race where a producer enqueues after the
last Pop but before the active drainer releases ownership.
*/
func (orchestrator *Orchestrator) drainInbox() {
	if !orchestrator.draining.CompareAndSwap(0, 1) {
		return
	}

	for {
		observed := orchestrator.enqueued.Load()

		for {
			ptr := orchestrator.inbox.Pop()

			if ptr == nil {
				break
			}

			orchestrator.submitStep((*primitive.Value)(ptr))
		}

		if orchestrator.enqueued.Load() != observed {
			continue
		}

		orchestrator.draining.Store(0)

		if orchestrator.enqueued.Load() == observed {
			return
		}

		if !orchestrator.draining.CompareAndSwap(0, 1) {
			return
		}
	}
}

/*
submitStep evaluates firmware rules for the Value. When a rule matches it
wraps the Value in an Executable whose Finalizer re-enters submitStep, then
submits a task that returns the Executable. The pool worker runs the task,
the Backend receives the Executable via the dispatch handler, compiles for
the chosen substrate, executes, and calls the Finalizer — which lands back
here for the next firmware pass. When no rule matches the Value is routed
to a community field.
*/
func (orchestrator *Orchestrator) submitStep(value *primitive.Value) {
	if value == nil {
		return
	}

	name := orchestrator.firmware.Next(value)

	if name == "" && value[kernel.SchedulingNextProgramWord] == value.ID() {
		executable := programmer.NewResidentExecutable(value)

		executable.SetFinalizer(orchestrator.publishExecuted)

		orchestrator.queue.Submit(func() *programmer.Executable {
			return executable
		})

		return
	}

	if name == "" {
		orchestrator.clearProgram(value)
		orchestrator.router.Route(value)
		return
	}

	executable := programmer.NewExecutable(value, name, nil)

	executable.SetFinalizer(orchestrator.publishExecuted)

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.CompilerCompileEvent(
			"queued", 0, 0, 0, false, 0,
		))
	}

	orchestrator.queue.Submit(func() *programmer.Executable {
		return executable
	})
}

/*
clearProgram removes stale in-Value firmware when no rule validates.
*/
func (orchestrator *Orchestrator) clearProgram(value *primitive.Value) {
	if value == nil {
		return
	}

	start, words := primitive.ProgramRegion.WordExtent()

	for offset := range words {
		value.Set(start+offset, 0)
	}

	value.Set(kernel.SchedulingNextProgramWord, 0)
}
