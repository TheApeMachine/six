//go:build !cuda || !cgo

package cuda

import "unsafe"

/*
CUDABackend is a stub implementation used for non-CUDA builds.
It provides no-op Available/Resolve so the package compiles without CUDA tooling.
*/
type CUDABackend struct{}

func (backend *CUDABackend) Available() bool {
	return false
}

func (backend *CUDABackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	return 0, nil
}

func (backend *CUDABackend) ResolvePhaseDial(
	cacheNodes unsafe.Pointer,
	numNodes int,
	queryDial unsafe.Pointer,
	similarities unsafe.Pointer,
) error {
	return nil
}

func (backend *CUDABackend) EncodePhaseDial(
	structuralPhases unsafe.Pointer,
	numValues int,
	outDial unsafe.Pointer,
) error {
	return nil
}

func (backend *CUDABackend) SeqToroidalMeanPhase(
	valueBlocks unsafe.Pointer,
	numValues int,
) (theta float64, phi float64, err error) {
	return 0, 0, nil
}

func (backend *CUDABackend) WeightedCircularMean(
	valueBlocks unsafe.Pointer,
	numValues int,
) (phase float64, concentration float64, err error) {
	return 0, 0, nil
}

func (backend *CUDABackend) SolveBVP(
	startBlocks unsafe.Pointer,
	goalBlocks unsafe.Pointer,
) (scale uint16, translate uint16, distance float64, err error) {
	return 0, 0, 0, nil
}
