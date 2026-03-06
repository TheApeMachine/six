//go:build !cuda || !cgo

package cuda

import (
	"errors"
	"unsafe"
)

func CudaAvailable() bool {
	return false
}

func BestFillCUDAPacked(
	_ unsafe.Pointer,
	_ int,
	_ unsafe.Pointer,
	_ unsafe.Pointer,
	_ unsafe.Pointer,
	_ unsafe.Pointer,
) (uint64, error) {
	return 0, errors.New("cuda backend unavailable")
}
