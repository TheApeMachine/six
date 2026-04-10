package programmer

import (
	"errors"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/errnie"
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

		framePtr := unsafe.Pointer(program.Frame())

		if execErr := publisher.queue.CompileAndExecute(program); execErr != nil {
			kv := append([]any{
				"stage", "programmer.SubmitTracked.CompileAndExecute",
				"output_label", label,
			}, kernel.CorrelationKeyvals(framePtr)...)

			_ = errnie.Error(execErr, kv...)

			execEv := viz.NewEvent(viz.EventQueueSubmit, "programmer")
			execEv.Label = "CompileAndExecute"
			execEv.Meta["stage"] = "CompileAndExecute"
			execEv.Meta["error"] = execErr.Error()
			execEv.Meta["output_label"] = label

			if corr := kernel.FrameCorrelationID(framePtr); corr != 0 {
				execEv.Meta["correlation"] = kernel.FormatCorrelationDecimal(corr)
			}

			viz.DefaultBus.Publish(execEv)

			return
		}

		outs, finalizeErr := program.Finalize()
		corr := kernel.FrameCorrelationID(framePtr)

		finEv := viz.FinalizerRunEvent(
			corr,
			program.FinalizerDepth(),
			len(outs),
			finalizeErr != nil,
		)

		if finalizeErr != nil {
			finEv.Meta["finalize_error"] = finalizeErr.Error()
		}

		viz.DefaultBus.Publish(finEv)

		if finalizeErr != nil {
			kv := append([]any{
				"stage", "programmer.SubmitTracked.Finalize",
				"output_label", label,
			}, kernel.CorrelationKeyvals(framePtr)...)

			_ = errnie.Error(finalizeErr, kv...)

			return
		}

		for _, out := range outs {
			if out == nil {
				continue
			}

			if pubErr := publisher.sink.Publish(out, label); pubErr != nil {
				kv := append([]any{
					"stage", "programmer.SubmitTracked.sink.Publish",
					"output_label", label,
				}, kernel.CorrelationKeyvals(framePtr)...)

				_ = errnie.Error(pubErr, kv...)

				pubEv := viz.NewEvent(viz.EventQueueSubmit, "programmer")
				pubEv.Label = "sink.Publish"
				pubEv.Meta["stage"] = "sink.Publish"
				pubEv.Meta["error"] = pubErr.Error()
				pubEv.Meta["output_label"] = label

				if corr != 0 {
					pubEv.Meta["correlation"] = kernel.FormatCorrelationDecimal(corr)
				}

				viz.DefaultBus.Publish(pubEv)
			}
		}
	})

	return nil
}
