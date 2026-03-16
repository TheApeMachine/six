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


