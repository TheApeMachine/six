package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

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

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

/*
Execute reads the opcode from each Value's program region (word 16)
and dispatches to the appropriate kernel:

  - 0x0–0xE and 0xF with idle profile: truth table ALU via
    universalBitwiseV2. The programmer pre-compiles B rotations into
    reserved (words 32-95).
  - 0x6 with batch marker at word 124: fused XOR+popcount+sum for
    Hamming distance. Query in the head of the token region, candidates
    packed contiguously starting at NearestAffinityCandidatesStartWord
    (56), count at word 124, uint32 results written to signals (words
    24-31).
  - 0xF with word 124>0 and word 125>0: CSA positional popcount on
    word-striped vectors starting at word 32; counts pointer at words
    126–127. Opcode 0xF with idle profile counters uses the truth table
    path instead.
*/
func (backend *Backend) Execute(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for idx := range frames {
		if frames[idx] == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "Execute")
		}
	}

	for _, ptr := range frames {
		v := (*[128]uint64)(ptr)
		rawOpcode := v[kernel.ProgramStartWord] & 0xFF
		opcode := rawOpcode & kernel.OpcodeBooleanMask
		batchCount := v[kernel.NearestAffinityBatchWord]

		switch {
		case kernel.ExecuteGeometricFrame(ptr, rawOpcode):
			continue

		case opcode == 0x6 && batchCount > 0:
			backend.executeBatchDistance(v, int(batchCount))

		case opcode == 0xF && v[124] > 0 && v[125] > 0:
			backend.executeProfile(v)

		default:
			universalBitwiseV2((*uint64)(ptr), 16)
		}
	}

	return nil
}

/*
executeBatchDistance runs the fused XOR+popcount+sum SIMD kernel
for Hamming distance. The programmer packed the query vector into the
frame head, candidates contiguously from NearestAffinityCandidatesStartWord,
count into word 124. Results are uint32 distances starting at
SignalsStartWord.

After the SIMD pass, an in-band argmin reduction writes the best index
and distance at SignalsStartWord+SignalBestIdxOffset and
SignalsStartWord+SignalBestDistOffset so the caller never touches raw
distance arrays.
*/
func (backend *Backend) executeBatchDistance(v *[128]uint64, count int) {
	if count > kernel.MaxNearestAffinityCandidates {
		count = kernel.MaxNearestAffinityCandidates
	}

	if count <= 0 {
		return
	}

	distances := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

	batchAffinityDistances(
		&v[0],
		&v[kernel.NearestAffinityCandidatesStartWord],
		count,
		&distances[0],
	)

	bestIdx := uint64(0)
	bestDist := uint64(distances[0])

	for idx := 1; idx < count; idx++ {
		dist := uint64(distances[idx])

		if dist < bestDist {
			bestDist = dist
			bestIdx = uint64(idx)
		}
	}

	v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
	v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist
}

/*
executeProfile runs CSA positional popcount on word-striped vectors
packed into the Value by the programmer. Word 124 = vector count,
word 125 = wordsPerVec. Vectors start at word 32. Results are written
to an external counts array whose address is stored in words 126-127
as a uintptr split across two uint64s (low, high on 32-bit — on
64-bit, word 126 holds the full pointer).
*/
func (backend *Backend) executeProfile(v *[128]uint64) {
	vectorCount := int(v[124])
	wordsPerVec := int(v[125])

	if vectorCount == 0 || wordsPerVec == 0 {
		return
	}

	/*
		Words 126–127 store the counts pointer bits; load them as unsafe.Pointer
		without a uintptr round-trip so vet unsafeptr stays satisfied.
	*/
	countsPtr := (*[64][64]int)(*(*unsafe.Pointer)(unsafe.Pointer(&v[126])))
	counts := countsPtr[:wordsPerVec]

	for word := range wordsPerVec {
		counts[word] = [64]int{}
	}

	stripe := make([]uint64, vectorCount)
	base := 32

	for word := range wordsPerVec {
		for vec := range vectorCount {
			stripe[vec] = v[base+vec*wordsPerVec+word]
		}

		pospop.Count64(&counts[word], stripe)
	}
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
