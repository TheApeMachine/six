package vm

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/kadabra"
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
	pool      *compute.Pool
	tokenizer *Tokenizer
	kadabra   *kadabra.Node
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	machine := &Machine{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.kadabra, machine.err = kadabra.NewNode(
		ctx,
		machine.host.Name,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.pool, machine.err = compute.NewPool(
		ctx,
		compute.PoolWithContext(ctx),
		compute.PoolWithErrBuffer(1),
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
		TokenizerWithPool(machine.pool),
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"kadabra":   machine.kadabra,
		"pool":      machine.pool,
		"tokenizer": machine.tokenizer,
	})
}

/*
Close the machine.
*/
func (machine *Machine) Close() error {
	machine.cancel()

	var errs []error

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

	if machine.kadabra != nil {
		if err := machine.kadabra.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

/*
Error returns the error of the machine.
*/
func (machine *Machine) Error() error {
	return machine.err
}

/*
Load a dataset into the machine.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	var n int64

	buf := make([]byte, 1024)

	for {
		if n, machine.err = io.CopyBuffer(machine.tokenizer, dataset, buf); machine.err != nil {
			if errors.Is(machine.err, io.EOF) {
				return nil
			}

			return errnie.Error(machine.err)
		}

		if n == 0 {
			break
		}
	}

	return machine.err
}

/*
Prompt the machine and retrieve both a prediction and a classification.

The prompt is converted into a temporary Value so we can compute its
affinity vector, which the Kadabra node uses to route the query to the
closest trie cluster(s). This ensures the prompt reaches the trie that
holds the most relevant data.
*/
func (machine *Machine) Prompt(prompt string) (*algo.Prediction, error) {
	if err := validate.Require(map[string]any{
		"kadabra": machine.kadabra,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	value, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	return machine.kadabra.Predict(value)
}
