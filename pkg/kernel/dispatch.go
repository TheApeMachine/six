package kernel

import (
	"unsafe"

	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/kernel/cpu"
	"github.com/theapemachine/six/pkg/kernel/cuda"
	"github.com/theapemachine/six/pkg/kernel/metal"
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
	backend Backend
}

type builderOpts func(*Builder)

func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backend = backend
	}
}

/*
NewBuilder creates the backend specified by the SIX_BACKEND environment variable.
Valid values: "metal", "cuda", "cpu". Defaults to "cpu" if unset.
Returns an error if the requested backend is unavailable.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{}

	for _, opt := range opts {
		opt(builder)
	}

	if builder.backend == nil {
		switch config.System.Backend {
		case "metal":
			builder.backend = NewBuilder(
				WithBackend(&metal.MetalBackend{}),
			)
		case "cuda":
			builder.backend = NewBuilder(
				WithBackend(&cuda.CUDABackend{}),
			)
		case "distributed":
			builder.backend = NewBuilder(
				WithBackend(&DistributedBackend{}),
			)
		case "cpu":
			builder.backend = NewBuilder(
				WithBackend(&cpu.CPUBackend{}),
			)
		}
	}

	return builder
}

func (builder *Builder) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	return builder.backend.Resolve(
		graphNodes,
		numNodes,
		context,
	)
}

func (builder *Builder) Available() bool {
	if builder.backend == nil {
		return false
	}
	return builder.backend.Available()
}

/*
DecodePacked unwraps the 64-bit result from the kernel backend.
The backend returns (131072 - distance_squared) in the upper 32 bits,
and the node index in the lower 32 bits. High values = lower distance.
*/
func DecodePacked(packed uint64) (idx int, distSq float64) {
	invertedDist := uint32(packed >> 32)
	idxU32 := uint32(packed & 0xFFFFFFFF)

	distSq = float64(131072 - invertedDist)
	return int(idxU32), distSq
}
