package compute

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Optimizer is a Workload generator that wraps around a slice of
Values, and looks for opportunities to optimize the workload.
*/
type Optimizer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	values  []*primitive.Value
	spawned []*primitive.Value
}

func NewOptimizer(
	ctx context.Context, values []*primitive.Value) *Optimizer {
	ctx, cancel := context.WithCancel(ctx)

	return &Optimizer{
		ctx:    ctx,
		cancel: cancel,
		values: values,
	}
}

/*
Close closes the Optimizer.
*/
func (optimizer *Optimizer) Close() error {
	optimizer.cancel()
	return optimizer.err
}

/*
Error implements the error interface.
*/
func (optimizer *Optimizer) Error() string {
	if optimizer.err == nil {
		return ""
	}
	return optimizer.err.Error()
}

/*
Workload returns the workload to be executed.
*/
func (optimizer *Optimizer) Workload() func() {
	return func() {
		for _, value := range optimizer.values {
			if value == nil || !value.ReadyForALU() {
				continue
			}

			before := snapshotProgram(value)

			value.SetStatus(primitive.BUSY)
			optimizer.spawned = append(optimizer.spawned, cpu.HypercubeGossip(value, optimizer.values)...)

			finalizeExecutedOwner(value, before)
		}
	}
}

/*
Optimize optimizes the workload.
*/
func (optimizer *Optimizer) Optimize() {}

func (optimizer *Optimizer) Spawned() []*primitive.Value {
	return optimizer.spawned
}
