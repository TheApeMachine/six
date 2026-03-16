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

type Builder struct {
	backends []Backend
}

type builderOpts func(*Builder)

func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backends = append(builder.backends, backend)
	}
}

/*
NewBuilder creates the backends prioritized array.
Automatically detects and appends CUDA, Metal, and Distributed backends if available.
CPU is always the ultimate fallback.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{}

	for _, opt := range opts {
		opt(builder)
	}

	if len(builder.backends) == 0 {
		cnd := &cuda.CUDABackend{}
		if cnd.Available() {
			builder.backends = append(builder.backends, cnd)
		}

		mtl := &metal.MetalBackend{}
		if mtl.Available() {
			builder.backends = append(builder.backends, mtl)
		}

		dist := &DistributedBackend{}
		if dist.Available() {
			builder.backends = append(builder.backends, dist)
		}

		builder.backends = append(builder.backends, &cpu.CPUBackend{})
	}

	return builder
}

func (builder *Builder) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (val uint64, err error) {
	var lastErr error
	for _, backend := range builder.backends {
		if !backend.Available() {
			continue
		}

		result, resolveErr := func() (uint64, error) {
			var v uint64
			var e error
			defer func() {
				if r := recover(); r != nil {
					e = fmt.Errorf("backend panic: %v", r)
					errnie.ErrorSafe(e, false)
				}
			}()
			v, e = backend.Resolve(graphNodes, numNodes, context)
			return v, e
		}()

		if resolveErr == nil {
			return result, nil
		}
		lastErr = resolveErr
	}
	return 0, lastErr
}

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
