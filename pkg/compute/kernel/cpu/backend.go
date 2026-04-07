package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
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

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

/*
UniversalBitwise executes the in-band program carried by each Value.

A (4 words) is held steady. B (4 words) is expanded into all 16
rotations (8 bits apart), producing a 64-word A surface and 64-word
B surface. The 8-word program region supplies one 4-bit opcode per
rotation. The truth table is applied across the entire 64-word
surface in a single SIMD pass. Results are written to the 8-word
Signals region (64 bytes, one per surface element).

The Value's Token and Program regions are never mutated.
*/
func (backend *Backend) UniversalBitwise(values []unsafe.Pointer) error {
	if len(values) == 0 {
		return nil
	}

	for i := range values {
		if values[i] == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "UniversalBitwise")
		}
	}

	for _, ptr := range values {
		execute((*[128]uint64)(ptr))
	}

	return nil
}

func execute(v *[128]uint64) {
	tokStart := core.Cfg.Value.Region.Tokens.Start
	progStart := core.Cfg.Value.Region.Program.Start
	sigStart := core.Cfg.Value.Region.Signals.Start

	a := v[tokStart : tokStart+4]

	// Expand B into 16 rotations × 4 words.
	var aSurface, bSurface [64]uint64
	var b [4]uint64
	b[0] = v[tokStart+4]
	b[1] = v[tokStart+5]
	b[2] = v[tokStart+6]
	b[3] = v[tokStart+7]

	for rot := range 16 {
		off := rot * 4
		aSurface[off] = a[0]
		aSurface[off+1] = a[1]
		aSurface[off+2] = a[2]
		aSurface[off+3] = a[3]
		bSurface[off] = b[0]
		bSurface[off+1] = b[1]
		bSurface[off+2] = b[2]
		bSurface[off+3] = b[3]

		b[0] = bits.RotateLeft64(b[0], 8)
		b[1] = bits.RotateLeft64(b[1], 8)
		b[2] = bits.RotateLeft64(b[2], 8)
		b[3] = bits.RotateLeft64(b[3], 8)
	}

	// Build per-element opcode masks from the program region.
	var m0, m1, m2, m3 [64]uint64
	prog := v[progStart : progStart+8]

	for rot := range 16 {
		op := uint8((prog[rot/2] >> uint((rot%2)*32)) & 0xF)
		mask0 := -uint64(op & 1)
		mask1 := -uint64((op >> 1) & 1)
		mask2 := -uint64((op >> 2) & 1)
		mask3 := -uint64((op >> 3) & 1)
		off := rot * 4
		m0[off], m0[off+1], m0[off+2], m0[off+3] = mask0, mask0, mask0, mask0
		m1[off], m1[off+1], m1[off+2], m1[off+3] = mask1, mask1, mask1, mask1
		m2[off], m2[off+1], m2[off+2], m2[off+3] = mask2, mask2, mask2, mask2
		m3[off], m3[off+1], m3[off+2], m3[off+3] = mask3, mask3, mask3, mask3
	}

	// Apply truth table across the full surface via SIMD.
	universalBitwise(
		&v[sigStart],
		&aSurface[0], &bSurface[0],
		&m0[0], &m1[0], &m2[0], &m3[0],
	)
}

/*
BatchDistances computes Hamming distances from query to count candidate
affinity vectors using SIMD assembly (NEON 4x unrolled on ARM64, AVX2
2x unrolled on AMD64). Each vector is 8 × uint64 = 64 bytes.
*/
func (backend *Backend) BatchDistances(
	query unsafe.Pointer,
	candidates unsafe.Pointer,
	count int,
	distances []uint32,
) error {
	if count == 0 {
		return nil
	}

	batchAffinityDistances(
		(*uint64)(query),
		(*uint64)(candidates),
		count,
		&distances[0],
	)

	return nil
}

func Popcount(value unsafe.Pointer, startBit, bitLen int) int {
	v := (*[128]uint64)(value)

	if bitLen <= 0 {
		return 0
	}

	startWord := startBit >> 6
	startShift := startBit & 63
	remaining := bitLen
	total := 0
	word := startWord
	shift := startShift

	for remaining > 0 {
		chunk := min(64-shift, remaining)

		lane := v[word] >> uint(shift)

		if shift > 0 && word+1 < 128 {
			lane |= v[word+1] << uint(64-shift)
		}

		mask := uint64(1<<chunk) - 1
		if chunk == 64 {
			mask = ^uint64(0)
		}

		total += bits.OnesCount64(lane & mask)

		remaining -= chunk
		word++
		shift = 0
	}

	return total
}
