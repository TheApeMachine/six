package programmer

import (
	"errors"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
	"github.com/theapemachine/six/pkg/viz"
)

/*
SubmitCompiledQueue is the subset of pool.Queue this package needs so
programmer does not import pool (that would cycle with queue.go).
*/
type SubmitCompiledQueue interface {
	SubmitTracked(task func())
	CompileAndExecute(program *Compiler) error
}

/*
pipelinePublisher runs a per-chunk Compiler on the shared pool.Queue (via
CompileAndExecute) and forwards finalized *primitive.Value results to sink.

SubmitTracked ensures Machine.Load’s Queue.Drain observes this work.
*/
type pipelinePublisher struct {
	queue   SubmitCompiledQueue
	factory func(*primitive.Value, string) (*Compiler, error)
	sink    transport.Publishable
}

/*
NewPublisher builds a transport.Publishable that schedules factory-built
programs on queue’s backend and publishes emitted values to sink.
*/
func NewPublisher(
	queue SubmitCompiledQueue,
	factory func(*primitive.Value, string) (*Compiler, error),
	sink transport.Publishable,
) (transport.Publishable, error) {
	if queue == nil || factory == nil || sink == nil {
		return nil, errors.New("programmer.NewPublisher: nil argument")
	}

	return &pipelinePublisher{
		queue:   queue,
		factory: factory,
		sink:    sink,
	}, nil
}

/*
Publish implements transport.Publishable.
*/
func (publisher *pipelinePublisher) Publish(
	value *primitive.Value,
	label string,
) error {
	program, err := publisher.factory(value, label)

	if err != nil {
		return err
	}

	publisher.queue.SubmitTracked(func() {
		if program == nil {
			return
		}

		if execErr := publisher.queue.CompileAndExecute(program); execErr != nil {
			return
		}

		outs, finalizeErr := program.Finalize()

		var corr uint64

		if program.Frame() != nil {
			corr = kernel.FrameCorrelationID(unsafe.Pointer(program.Frame()))
		}

		viz.DefaultBus.Publish(viz.FinalizerRunEvent(
			corr,
			program.FinalizerDepth(),
			len(outs),
			finalizeErr != nil,
		))

		if finalizeErr != nil {
			return
		}

		for _, out := range outs {
			if out == nil {
				continue
			}

			_ = publisher.sink.Publish(out, label)
		}
	})

	return nil
}
