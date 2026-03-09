package cpu

import (
	"sync"
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/tphakala/simd/f32"
)

var bufPool = sync.Pool{
	New: func() any {
		return &simdBufs{}
	},
}

type simdBufs struct {
	aVals   []float32
	bVals   []float32
	ctxAVec []float32
	ctxBVec []float32
	da      []float32
	db      []float32
	distSq  []float32
}

func (bufs *simdBufs) ensure(size int) {
	if cap(bufs.aVals) >= size {
		bufs.aVals = bufs.aVals[:size]
		bufs.bVals = bufs.bVals[:size]
		bufs.ctxAVec = bufs.ctxAVec[:size]
		bufs.ctxBVec = bufs.ctxBVec[:size]
		bufs.da = bufs.da[:size]
		bufs.db = bufs.db[:size]
		bufs.distSq = bufs.distSq[:size]
		return
	}

	bufs.aVals = make([]float32, size)
	bufs.bVals = make([]float32, size)
	bufs.ctxAVec = make([]float32, size)
	bufs.ctxBVec = make([]float32, size)
	bufs.da = make([]float32, size)
	bufs.db = make([]float32, size)
	bufs.distSq = make([]float32, size)
}

/*
CPUBackend resolves nearest-node queries using SIMD-accelerated distance computation.
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
to the context rotation. Uses SIMD f32 operations for vectorized distance
computation: 8x throughput on AVX, 4x on NEON vs scalar loop.
*/
func (backend *CPUBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 || graphNodes == nil || context == nil {
		return 0, nil
	}

	nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
	ctx := (*geometry.GFRotation)(context)

	bufs := bufPool.Get().(*simdBufs)
	defer bufPool.Put(bufs)

	bufs.ensure(numNodes)

	ctxA := float32(ctx.A)
	ctxB := float32(ctx.B)

	for idx := range numNodes {
		bufs.aVals[idx] = float32(nodes[idx].A)
		bufs.bVals[idx] = float32(nodes[idx].B)
		bufs.ctxAVec[idx] = ctxA
		bufs.ctxBVec[idx] = ctxB
	}

	f32.Sub(bufs.da, bufs.aVals, bufs.ctxAVec)
	f32.Sub(bufs.db, bufs.bVals, bufs.ctxBVec)
	f32.Mul(bufs.da, bufs.da, bufs.da)
	f32.Mul(bufs.db, bufs.db, bufs.db)
	f32.Add(bufs.distSq, bufs.da, bufs.db)

	bestIdx := f32.MinIdx(bufs.distSq)
	bestDistSq := min(uint32(bufs.distSq[bestIdx]), 131072)
	invertedDist := uint32(131072) - bestDistSq
	packed := (uint64(invertedDist) << 32) | uint64(uint32(bestIdx))

	return packed, nil
}
