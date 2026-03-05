//go:build !darwin || !cgo

package metal

import (
	"errors"
	"unsafe"
)

func MetalAvailable() bool {
	return false
}

func BestFillMetalPacked(
	_ unsafe.Pointer,
	_ int,
	_ unsafe.Pointer,
	_ unsafe.Pointer,
	_ int,
	_ unsafe.Pointer,
) (uint64, error) {
	return 0, errors.New("metal backend unavailable")
}
