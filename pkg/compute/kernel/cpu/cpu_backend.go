package cpu

import (
	"fmt"
	"math"
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

/*
CPUBackend resolves nearest-node queries using a zero-allocation integer distance scan.
*/
type CPUBackend struct{}

/*
Available always returns true for the CPU backend.
*/
func (backend *CPUBackend) Available() bool {
	return true
}

/*
Resolve finds the graph node with the smallest GF(257) geometric distance
to the context rotation using direct integer arithmetic.
*/
func (backend *CPUBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 {
		return 0, fmt.Errorf("invalid numNodes: must be > 0")
	}
	if graphNodes == nil {
		return 0, fmt.Errorf("nil graphNodes pointer")
	}
	if context == nil {
		return 0, fmt.Errorf("nil context pointer")
	}

	nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
	ctx := (*geometry.GFRotation)(context)
	return resolve.PackedNearest(nodes, *ctx), nil
}

func (backend *CPUBackend) ResolvePhaseDial(
	cacheNodes unsafe.Pointer,
	numNodes int,
	queryDial unsafe.Pointer,
	similarities unsafe.Pointer,
) error {
	if numNodes <= 0 {
		return fmt.Errorf("invalid numNodes: must be > 0")
	}
	if cacheNodes == nil || queryDial == nil || similarities == nil {
		return fmt.Errorf("nil pointer in ResolvePhaseDial")
	}
	query := geometry.PhaseDial(unsafe.Slice((*complex128)(queryDial), 512))
	cache := unsafe.Slice((*complex128)(cacheNodes), numNodes*512)
	sims := unsafe.Slice((*float64)(similarities), numNodes)
	for n := 0; n < numNodes; n++ {
		offset := n * 512
		entryDial := geometry.PhaseDial(cache[offset : offset+512])
		sims[n] = query.Similarity(entryDial)
	}
	return nil
}

func (backend *CPUBackend) EncodePhaseDial(
	structuralPhases unsafe.Pointer,
	numValues int,
	outDial unsafe.Pointer,
) error {
	if numValues <= 0 {
		return fmt.Errorf("invalid numValues: must be > 0")
	}
	if structuralPhases == nil || outDial == nil {
		return fmt.Errorf("nil pointer in EncodePhaseDial")
	}
	phases := unsafe.Slice((*float64)(structuralPhases), numValues)
	dial := unsafe.Slice((*complex128)(outDial), 512)
	for k := 0; k < 512; k++ {
		var sum complex128
		omega := float64(numeric.Primes[k])
		for t := 0; t < numValues; t++ {
			phase := (omega*float64(t+1)*0.1 + phases[t]*math.Pi*2)
			sum += complex(math.Cos(phase), math.Sin(phase))
		}
		dial[k] = sum
	}
	return nil
}

func (backend *CPUBackend) SeqToroidalMeanPhase(
	valueBlocks unsafe.Pointer,
	numValues int,
) (theta float64, phi float64, err error) {
	if numValues <= 0 {
		return 0, 0, nil
	}
	if valueBlocks == nil {
		return 0, 0, fmt.Errorf("nil pointer in SeqToroidalMeanPhase")
	}
	blocks := unsafe.Slice((*uint64)(valueBlocks), numValues*8)
	var sinTSum, cosTSum, sinPSum, cosPSum float64
	for i := 0; i < numValues; i++ {
		offset := i * 8
		var activeCount int
		var vSin, vCos float64
		for blk := 0; blk < 8; blk++ {
			block := blocks[offset+blk]
			for block != 0 {
				bitIdx := bits.TrailingZeros64(block)
				primeIdx := blk*64 + bitIdx
				if primeIdx < 257 {
					angle := 2 * math.Pi * float64(primeIdx) / 257.0
					vSin += math.Sin(angle)
					vCos += math.Cos(angle)
					activeCount++
				}
				block &= block - 1
			}
		}
		var vTheta float64
		if vSin != 0 || vCos != 0 {
			vTheta = math.Atan2(vSin, vCos)
		}
		vPhi := 2 * math.Pi * float64(activeCount) / 257.0
		sinTSum += math.Sin(vTheta)
		cosTSum += math.Cos(vTheta)
		sinPSum += math.Sin(vPhi)
		cosPSum += math.Cos(vPhi)
	}
	return math.Atan2(sinTSum, cosTSum), math.Atan2(sinPSum, cosPSum), nil
}

func (backend *CPUBackend) WeightedCircularMean(
	valueBlocks unsafe.Pointer,
	numValues int,
) (phase float64, concentration float64, err error) {
	if numValues <= 0 {
		return 0, 0, nil
	}
	if valueBlocks == nil {
		return 0, 0, fmt.Errorf("nil pointer in WeightedCircularMean")
	}
	blocks := unsafe.Slice((*uint64)(valueBlocks), numValues*8)
	var sinSum, cosSum, wSum float64
	for i := 0; i < numValues; i++ {
		offset := i * 8
		var activeCount int
		var vSin, vCos float64
		for blk := 0; blk < 8; blk++ {
			block := blocks[offset+blk]
			for block != 0 {
				bitIdx := bits.TrailingZeros64(block)
				primeIdx := blk*64 + bitIdx
				if primeIdx < 257 {
					angle := 2 * math.Pi * float64(primeIdx) / 257.0
					vSin += math.Sin(angle)
					vCos += math.Cos(angle)
					activeCount++
				}
				block &= block - 1
			}
		}
		var vTheta float64
		if vSin != 0 || vCos != 0 {
			vTheta = math.Atan2(vSin, vCos)
		}
		weight := float64(activeCount)
		if weight <= 0 {
			weight = 1.0
		}
		sinSum += weight * math.Sin(vTheta)
		cosSum += weight * math.Cos(vTheta)
		wSum += weight
	}
	if wSum == 0 {
		return 0, 0, nil
	}
	return math.Atan2(sinSum, cosSum), math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum, nil
}

func (backend *CPUBackend) SolveBVP(
	startBlocks unsafe.Pointer,
	goalBlocks unsafe.Pointer,
) (scale uint16, translate uint16, distance float64, err error) {
	if startBlocks == nil || goalBlocks == nil {
		return 0, 0, 0, fmt.Errorf("nil pointer in SolveBVP")
	}
	return 1, 0, 0, fmt.Errorf("SolveBVP not yet implemented for CPUBackend")
}
