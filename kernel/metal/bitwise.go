//go:build darwin && cgo

package metal

/*
#cgo CXXFLAGS: -x objective-c++
#cgo LDFLAGS: -framework Metal -framework Foundation
#include "metal.h"
#include <stdlib.h>
*/
import "C"
import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"unsafe"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c bitwise.metal -o bitwise.air
//go:generate xcrun -sdk macosx metallib bitwise.air -o bitwise.metallib

//go:embed bitwise.metallib
var bitwiseMetallib []byte

var metalReady atomic.Bool

func init() {
	tmpFile, err := os.CreateTemp("", "sensorium-shader-*.metallib")

	if err != nil {
		return
	}

	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(bitwiseMetallib); err != nil {
		tmpFile.Close()
		return
	}

	tmpFile.Close()

	cPath := C.CString(tmpFile.Name())
	defer C.free(unsafe.Pointer(cPath))

	res := C.init_metal(cPath)

	if res != 0 {
		return
	}

	metalReady.Store(true)
}

func MetalAvailable() bool {
	return metalReady.Load()
}

func BestFillMetalPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	if !MetalAvailable() {
		return 0, errors.New("metal backend unavailable")
	}

	if numChords == 0 {
		return 0, nil
	}

	if expectedReality == nil {
		expectedReality = context
	}

	var packed C.uint64_t
	status := C.bitwise_best_fill_metal(
		dictionary,
		C.uint32_t(numChords),
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
		&packed,
	)
	if status != 0 {
		return 0, fmt.Errorf("metal best fill failed: status=%d", int(status))
	}

	return uint64(packed), nil
}
