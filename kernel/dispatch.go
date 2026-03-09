package kernel

import (
	"os"
	"unsafe"

	"github.com/theapemachine/six/kernel/cpu"
	"github.com/theapemachine/six/kernel/cuda"
	"github.com/theapemachine/six/kernel/metal"
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
	backend  Backend
	backends []namedBackend
}

type builderOpts func(*Builder)

func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backends = append(builder.backends, namedBackend{
			name:    backendName(backend),
			backend: backend,
		})
	}
}

type namedBackend struct {
	name    string
	backend Backend
}

/*
NewBuilder creates the backend specified by the SIX_BACKEND environment variable.
Valid values: "metal", "cuda", "cpu". If unset, the first available configured
backend is selected.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{}

	for _, opt := range opts {
		opt(builder)
	}

	if len(builder.backends) == 0 {
		builder.backends = []namedBackend{
			{name: "cpu", backend: &cpu.CPUBackend{}},
			{name: "cuda", backend: &cuda.CUDABackend{}},
			{name: "metal", backend: &metal.MetalBackend{}},
		}
	}

	builder.backend = builder.selectBackend(os.Getenv("SIX_BACKEND"))
	return builder
}

func (builder *Builder) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if builder.backend == nil {
		return 0, nil
	}

	return builder.backend.Resolve(
		graphNodes,
		numNodes,
		context,
	)
}

func (builder *Builder) Available() bool {
	return builder.backend != nil && builder.backend.Available()
}

func (builder *Builder) selectBackend(requested string) Backend {
	if requested != "" {
		for _, candidate := range builder.backends {
			if candidate.name == requested && candidate.backend.Available() {
				return candidate.backend
			}
		}

		return nil
	}

	for _, candidate := range builder.backends {
		if candidate.backend.Available() {
			return candidate.backend
		}
	}

	return nil
}

func backendName(backend Backend) string {
	switch backend.(type) {
	case *cpu.CPUBackend:
		return "cpu"
	case *cuda.CUDABackend:
		return "cuda"
	case *metal.MetalBackend:
		return "metal"
	default:
		return ""
	}
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
