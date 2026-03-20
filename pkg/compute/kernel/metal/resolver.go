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
	"os"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/numeric/geometry"
	"github.com/theapemachine/six/pkg/system/transport"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c resolver.metal -o resolver.air
//go:generate xcrun -sdk macosx metallib resolver.air -o resolver.metallib

//go:embed resolver.metallib
var resolverMetallib []byte

var metalReady atomic.Bool

type MetalBackend struct {
	*transport.Stream
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func (backend *MetalBackend) Available() (int, error) {
	if !metalReady.Load() {
		return 0, MetalErrorUnavailable
	}

	return int(C.count_metal_devices()), nil
}

func (backend *MetalBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 {
		return 0, MetalErrorResolveFailed
	}
	if graphNodes == nil || context == nil {
		return 0, MetalErrorResolveFailed
	}
	if n, err := backend.Available(); n == 0 || err != nil {
		return 0, MetalErrorUnavailable
	}

	if numNodes < 1024 {
		nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
		ctx := (*geometry.GFRotation)(context)
		return resolve.PackedNearest(nodes, *ctx), nil
	}
	if uint64(numNodes) > uint64(^uint32(0)) {
		return 0, MetalErrorResolveFailed
	}

	var packed C.uint64_t
	status := C.resolve_resonance_metal(
		graphNodes,
		C.uint32_t(numNodes),
		context,
		&packed,
	)
	if status != 0 {
		return 0, MetalErrorResolveFailed
	}

	return uint64(packed), nil
}

func (backend *MetalBackend) ResolvePhaseDial(
	cacheNodes unsafe.Pointer,
	numNodes int,
	queryDial unsafe.Pointer,
	similarities unsafe.Pointer,
) error {
	if n, err := backend.Available(); n == 0 || err != nil {
		return MetalErrorUnavailable
	}
	if cacheNodes == nil || queryDial == nil || similarities == nil || numNodes <= 0 {
		return MetalErrorResolveFailed
	}
	if C.resolve_phasedial_metal(cacheNodes, C.uint32_t(numNodes), queryDial, similarities) != 0 {
		return MetalErrorResolveFailed
	}
	return nil
}

func (backend *MetalBackend) EncodePhaseDial(
	structuralPhases unsafe.Pointer,
	numValues int,
	outDial unsafe.Pointer,
) error {
	if n, err := backend.Available(); n == 0 || err != nil {
		return MetalErrorUnavailable
	}
	if structuralPhases == nil || outDial == nil || numValues <= 0 {
		return MetalErrorResolveFailed
	}
	primes := make([]float32, 512)
	for i, p := range numeric.Primes {
		primes[i] = float32(p)
	}
	if C.encode_phasedial_metal(structuralPhases, unsafe.Pointer(&primes[0]), C.uint32_t(numValues), outDial) != 0 {
		return MetalErrorResolveFailed
	}
	return nil
}

func (backend *MetalBackend) SeqToroidalMeanPhase(
	valueBlocks unsafe.Pointer,
	numValues int,
) (theta float64, phi float64, err error) {
	if n, availErr := backend.Available(); n == 0 || availErr != nil {
		return 0, 0, MetalErrorUnavailable
	}
	if valueBlocks == nil || numValues <= 0 {
		return 0, 0, nil
	}
	var outTheta, outPhi C.double
	if C.seq_toroidal_mean_phase_metal(valueBlocks, C.uint32_t(numValues), &outTheta, &outPhi) != 0 {
		return 0, 0, MetalErrorResolveFailed
	}
	return float64(outTheta), float64(outPhi), nil
}

func (backend *MetalBackend) WeightedCircularMean(
	valueBlocks unsafe.Pointer,
	numValues int,
) (phase float64, concentration float64, err error) {
	if n, availErr := backend.Available(); n == 0 || availErr != nil {
		return 0, 0, MetalErrorUnavailable
	}
	if valueBlocks == nil || numValues <= 0 {
		return 0, 0, nil
	}
	var outPhase, outConcentration C.double
	if C.weighted_circular_mean_metal(valueBlocks, C.uint32_t(numValues), &outPhase, &outConcentration) != 0 {
		return 0, 0, MetalErrorResolveFailed
	}
	return float64(outPhase), float64(outConcentration), nil
}

func (backend *MetalBackend) SolveBVP(
	startBlocks unsafe.Pointer,
	goalBlocks unsafe.Pointer,
) (scale uint16, translate uint16, distance float64, err error) {
	if n, availErr := backend.Available(); n == 0 || availErr != nil {
		return 0, 0, 0, MetalErrorUnavailable
	}
	if startBlocks == nil || goalBlocks == nil {
		return 0, 0, 0, MetalErrorResolveFailed
	}
	var outScale, outTranslate C.uint16_t
	var outDistance C.double
	if C.solve_bvp_metal(startBlocks, goalBlocks, &outScale, &outTranslate, &outDistance) != 0 {
		return 0, 0, 0, MetalErrorResolveFailed
	}
	return uint16(outScale), uint16(outTranslate), float64(outDistance), nil
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

var initErr error

func InitError() error {
	return initErr
}

func init() {
	tmpFile, err := os.CreateTemp("", "sensorium-shader-*.metallib")
	if err != nil {
		initErr = err
		return
	}

	name := tmpFile.Name()
	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := tmpFile.Write(resolverMetallib); err != nil {
		tmpFile.Close()
		initErr = err
		return
	}
	if err := tmpFile.Close(); err != nil {
		initErr = err
		return
	}

	cPath := C.CString(name)
	defer C.free(unsafe.Pointer(cPath))

	if res := C.init_metal(cPath); res != 0 {
		initErr = MetalErrorInitFailed
		return
	}

	metalReady.Store(true)
	initErr = nil
}
