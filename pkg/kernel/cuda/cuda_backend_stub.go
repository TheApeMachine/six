//go:build !cuda || !cgo

package cuda

import "unsafe"

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
