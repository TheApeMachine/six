package cpu

import (
	"context"
	"runtime"

	"github.com/theapemachine/six/pkg/compute/kernel"
	pospop "github.com/theapemachine/six/pkg/compute/kernel/cpu/csa"
)

type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type backendOption func(*Backend)

func NewBackend(ctx context.Context, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Shutdown() {
	if backend == nil || backend.cancel == nil {
		return
	}

	backend.cancel()
}

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

/*
UniversalBitwise runs the SIMD ALU pass on the given Optimizer frame.
*/
func (backend *Backend) UniversalBitwise(frame *kernel.Optimizer) {
	if frame == nil {
		return
	}

	for idx, op := range frame.OP {
		aSlice := frame.A[idx]
		bSlice := frame.B[idx]
		dstSlice := frame.DST[idx]

		// If this instruction slot is empty, we're done
		if len(aSlice) == 0 || len(bSlice) == 0 || len(dstSlice) == 0 {
			break
		}

		mode := frame.MODE[idx]
		imm := frame.IMM[idx]

		if mode == 2 { // cmov
			if bSlice[0] != 0 {
				for i := range dstSlice {
					dstSlice[i] = aSlice[i%len(aSlice)]
				}
			}
			continue
		}

		if mode == 3 { // imm
			a := aSlice[0]
			b := imm
			notA, notB := ^a, ^b

			m0, m1, m2, m3 := uint64(0), uint64(0), uint64(0), uint64(0)
			if op&0x1 != 0 {
				m0 = ^uint64(0)
			}
			if op&0x2 != 0 {
				m1 = ^uint64(0)
			}
			if op&0x4 != 0 {
				m2 = ^uint64(0)
			}
			if op&0x8 != 0 {
				m3 = ^uint64(0)
			}

			dstSlice[0] = (a & b & m0) | (a & notB & m1) | (notA & b & m2) | (notA & notB & m3)
			continue
		}

		if mode == 4 { // tally
			var counts [64]int
			pospop.Count64(&counts, aSlice)

			var winner uint64
			threshold := len(aSlice) / 2
			for i := 0; i < 64; i++ {
				if counts[i] > threshold {
					winner |= (1 << i)
				}
			}

			dstSlice[0] = winner
			continue
		}

		for i := range dstSlice {
			a := aSlice[idx%len(aSlice)]
			b := bSlice[idx%len(bSlice)]

			frame.RETURN[idx][i] ^= (a & b & (uint64(0) - (op & 1))) |
				(a & ^b & (uint64(0) - ((op >> 1) & 1))) |
				(^a & b & (uint64(0) - ((op >> 2) & 1))) |
				(^a & ^b & (uint64(0) - ((op >> 3) & 1)))

			// Accumulate mode (0) writes back to DST
			if mode == 0 {
				dstSlice[i] ^= frame.RETURN[idx][i]
			}
		}
	}
}
