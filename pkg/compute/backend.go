package compute

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

type QueueType uint

const (
	// PRIORITY is used for Values that set a loop or branch
	// flag at the end of the program. This is the way we keep
	// the ability to do looping and branching, while keeping
	// the execution model in-hardware totally branchless,
	// preventing thread-divergence.
	PRIORITY QueueType = iota
	// NORMAL is used as the default queue for Values. The way
	// queues are drained is dynamically determined by various
	// factors, such as the number of available substrates, the
	// load on the substrates, and the number of Values in the
	// queues.
	NORMAL
)

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It monitors
pressure across available local arithmetic hardware (GPU/CPU) and geometrically
overflows into the local Region (which acts as a local clustering space and
network mesh interface) if local capabilities are fully saturated.

Ingress splits into two FIFOs so accelerator batching never interleaves frames
that must execute the in-band CPU interpreter with SIMD‑only GPU kernels — a
single program-bearing frame previously forced the entire batch onto the CPU
after packing on the GPU path.

Accelerators are chosen by least in-flight depth, then lowest EMA service time,
rather than round‑robin.
*/
type Backend struct {
	ctx      context.Context
	cancel   context.CancelFunc
	hardware []kernel.Substrate
	pool     *Pool
}

// BackendOption configures the multi-substrate router.
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
Accelerators are registered before the CPU fallback so the fast path is used
first when more than one substrate is available.
*/
func NewBackend(ctx context.Context, opts ...BackendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:      ctx,
		cancel:   cancel,
		hardware: make([]kernel.Substrate, 0),
	}

	for _, opt := range opts {
		opt(backend)
	}

	if err := validate.Require(map[string]any{
		"ctx":    backend.ctx,
		"cancel": backend.cancel,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.hardware = append(backend.hardware, cuda.NewBackend(idx))
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.hardware = append(backend.hardware, metal.NewBackend(idx))
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.hardware = append(backend.hardware, cpu.NewBackend(backend.ctx))

	if err := validate.Require(map[string]any{
		"hardware": backend.hardware,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	return backend
}
