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

type MetalError string

const (
	MetalErrorUnavailable   MetalError = "metal backend unavailable"
	MetalErrorInitFailed    MetalError = "metal backend init failed"
	MetalErrorResolveFailed MetalError = "metal backend resolve failed"
)

func (err MetalError) Error() string {
	return string(err)
}


