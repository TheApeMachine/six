package kernel

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Backend is the sole interface for resolving topological queries against the network.

Each query node's GF(257) state is compared against all nodes in the thermodynamic field.
The backend calculates geometric distance mapping. It returns one packed uint64
per query containing the best-matching entry index and its distance score.

Implementations exist for Metal (GPU), CUDA (GPU), and CPU.
The caller selects one backend at startup via SIX_BACKEND env var.
*/
type Backend interface {
	Available() bool
	Resolve(
		graphNodes unsafe.Pointer,
		numNodes int,
		context unsafe.Pointer,
	) (uint64, error)
}

/*
Builder aggregates available Backend implementations such as GPU or CPU cores
and constructs or dispatches kernels to the most suitable available backing store.
*/
type Builder struct {
	backends []Backend
}

type builderOpts func(*Builder)

/*
WithBackend returns a builderOpts closure appending the given backend
implementation to the builder's local slice of available computing backends.
*/
func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backends = append(builder.backends, backend)
	}
}

/*
NewBuilder creates the backends prioritized array.
To detect the backends, we had to instantiate them, and we also
check availability in the Resolve method, so we might as well
just instantiate them all and let the Resolve method handle it.
They will be overridden by the WithBackend option if provided.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{
		backends: []Backend{
			&cpu.CPUBackend{},
			&cuda.CUDABackend{},
			&metal.MetalBackend{},
			&DistributedBackend{},
		},
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder
}

func (builder *Builder) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (val uint64, err error) {
	var lastErr error
	var attempted bool

	for _, backend := range builder.backends {
		if !backend.Available() {
			continue
		}

		attempted = true

		result, resolveErr := func() (uint64, error) {
			var resolvedID uint64
			var resolveErrInside error

			defer func() {
				if recoverVal := recover(); recoverVal != nil {
					resolveErrInside = fmt.Errorf("backend panic: %v", recoverVal)
					errnie.ErrorSafe(resolveErrInside, false)
				}
			}()

			resolvedID, resolveErrInside = backend.Resolve(graphNodes, numNodes, context)
			return resolvedID, resolveErrInside
		}()

		if resolveErr == nil {
			return result, nil
		}

		lastErr = resolveErr
	}

	if !attempted || lastErr == nil {
		return 0, fmt.Errorf("no available backends")
	}

	return 0, lastErr
}

/*
Available returns true if any backend within the Builder's backends list
reports it is actively available, short-circuiting on the first success.
*/
func (builder *Builder) Available() bool {
	for _, backend := range builder.backends {
		if backend.Available() {
			return true
		}
	}
	return false
}

/*
maxEncodedDistSq is the encoding bias/upper bound (2^17) used by the kernel backend.
Higher inverted values represent closer matches for atomicMax semantics.
CUDA uses scale 1024 for fractional precision; scaledMax is the CUDA upper bound.
*/
const maxEncodedDistSq = 1 << 17
const scaledMaxEncoded = maxEncodedDistSq * 1024

/*
DecodePacked unwraps the 64-bit result from the kernel backend.
The backend returns (maxEncodedDistSq - distance_squared) in the upper 32 bits,
and the node index in the lower 32 bits. High values = lower distance.
CUDA uses scale 1024 for fractional distance; CPU uses integer distSq.
*/
func DecodePacked(packed uint64) (idx int, distSq float64) {
	invertedDist := uint32(packed >> 32)
	idxU32 := uint32(packed & 0xFFFFFFFF)

	if invertedDist > maxEncodedDistSq {
		distSq = float64(scaledMaxEncoded-invertedDist) / 1024
	} else {
		distSq = float64(maxEncodedDistSq - invertedDist)
	}

	return int(idxU32), distSq
}
