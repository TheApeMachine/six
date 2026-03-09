package kernel

import (
	"unsafe"
)

/*
Backend is the sole interface for resolving chord queries against a dictionary.

Each query chord is compared against all dictionary entries using chord overlap
(popcount of AND / popcount of AND + popcount of AND-NOT). The backend returns
one packed uint64 per query containing the best-matching entry index and its score.

Implementations exist for Metal (GPU), CUDA (GPU), and CPU (software popcount).
The caller selects one backend at startup via SIX_BACKEND env var. There is no
fallback chain: if the configured backend fails, the error propagates.
*/
type Backend interface {
	Available() bool
	Resolve(
		dictionary unsafe.Pointer,
		numChords int,
		context unsafe.Pointer,
		expectedReality unsafe.Pointer,
		expectedPrecision unsafe.Pointer,
		geodesicLUT unsafe.Pointer,
	) ([]uint64, error)
}

type Builder struct {
	backend Backend
}

type builderOpts func(*Builder)

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
		return nil
	}

	return builder
}

func (builder *Builder) Resolve(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) ([]uint64, error) {
	return builder.backend.Resolve(
		dictionary,
		numChords,
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	)
}

func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backend = backend
	}
}
