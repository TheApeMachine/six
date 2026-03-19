//go:build !darwin || !cgo

package metal

import "unsafe"

type MetalBackend struct{}

func (backend *MetalBackend) Available() bool {
	return false
}

func (backend *MetalBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	return 0, MetalErrorUnavailable
}

func (backend *MetalBackend) ResolvePhaseDial(
	cacheNodes unsafe.Pointer,
	numNodes int,
	queryDial unsafe.Pointer,
	similarities unsafe.Pointer,
) error {
	return MetalErrorUnavailable
}

func (backend *MetalBackend) EncodePhaseDial(
	structuralPhases unsafe.Pointer,
	numValues int,
	outDial unsafe.Pointer,
) error {
	return MetalErrorUnavailable
}

func (backend *MetalBackend) SeqToroidalMeanPhase(
	valueBlocks unsafe.Pointer,
	numValues int,
) (theta float64, phi float64, err error) {
	return 0, 0, MetalErrorUnavailable
}

func (backend *MetalBackend) WeightedCircularMean(
	valueBlocks unsafe.Pointer,
	numValues int,
) (phase float64, concentration float64, err error) {
	return 0, 0, MetalErrorUnavailable
}

func (backend *MetalBackend) SolveBVP(
	startBlocks unsafe.Pointer,
	goalBlocks unsafe.Pointer,
) (scale uint16, translate uint16, distance float64, err error) {
	return 0, 0, 0, MetalErrorUnavailable
}

type MetalError string

const (
	MetalErrorUnavailable   MetalError = "metal backend unavailable"
	MetalErrorInitFailed    MetalError = "metal backend init failed"
	MetalErrorResolveFailed MetalError = "metal backend resolve failed"
)

func (err MetalError) Error() string {
	return string(err)
}
