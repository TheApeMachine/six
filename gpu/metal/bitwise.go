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
	"fmt"
	"os"
	"unsafe"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c bitwise.metal -o bitwise.air
//go:generate xcrun -sdk macosx metallib bitwise.air -o default.metallib

//go:embed default.metallib
var defaultMetallib []byte

func init() {
	tmpFile, err := os.CreateTemp("", "sensorium-shader-*.metallib")
	
	if err != nil {
		panic(fmt.Sprintf("Failed to create temp file for metallib: %v", err))
	}
	
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(defaultMetallib); err != nil {
		tmpFile.Close()
		panic(fmt.Sprintf("Failed to write metallib to temp file: %v", err))
	}
	
	tmpFile.Close()

	cPath := C.CString(tmpFile.Name())
	defer C.free(unsafe.Pointer(cPath))

	res := C.init_metal(cPath)

	if res != 0 {
		panic(fmt.Sprintf("Failed to initialize Metal (error code: %d)", int(res)))
	}
}

// BestFill calls the Metal compute shader to find the best match.
// dictionary is a contiguous array of 64-byte Bitsets.
func BestFill(
	dictionary unsafe.Pointer, 
	numChords int, 
	context unsafe.Pointer, 
	targetIdx int,
) (int, float64, error) {
	if numChords == 0 {
		return 0, 0.0, nil
	}

	packed := uint64(
		C.bitwise_best_fill_metal(
			dictionary, 
			C.uint32_t(numChords), 
			context, 
			C.uint32_t(targetIdx),
		),
	)

	scoreFixed := uint32(packed >> 40)
	bestIdx := int(packed & 0xFFFFFF)
	bestScore := float64(scoreFixed) / 4000000.0

	return bestIdx, bestScore, nil
}
