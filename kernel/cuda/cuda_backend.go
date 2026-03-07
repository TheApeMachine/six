//go:build cuda && cgo

package cuda

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo darwin LDFLAGS: -L${SRCDIR} -lsixcuda -lcudart
#cgo linux LDFLAGS: -L${SRCDIR} -lsixcuda -lcudart
#include "cuda.h"
*/
import "C"

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

var cudaReady atomic.Bool

func init() {
	if C.init_cuda() == 0 {
		cudaReady.Store(true)
	}
}

func CudaAvailable() bool {
	return cudaReady.Load()
}

func BestFillCUDAPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	if !CudaAvailable() {
		return 0, errors.New("cuda backend unavailable")
	}
	if numChords == 0 {
		return 0, nil
	}
	if expectedReality == nil {
		expectedReality = context
	}

	packed := uint64(C.bitwise_best_fill_cuda(
		dictionary,
		C.uint32_t(numChords),
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	))

	return packed, nil
}
