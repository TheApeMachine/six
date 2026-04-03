package compute

import (
	"math/rand"
	"sync"

	"github.com/theapemachine/six/pkg/core"
)

/*
EliteArchive implements a minimal MAP-Elites style reservoir: one elite program
band per affinity bin (high bits). StoreIfBetter keeps the highest
SubstrateExploitScore observed for each bin; TryInject copies a sampled
neighbor bin's program onto a working frame.
*/
type EliteArchive struct {
	mu sync.Mutex

	fitness map[uint16]float64
	bands   map[uint16][]uint64
}

/*
NewEliteArchive constructs an empty archive.
*/
func NewEliteArchive() *EliteArchive {

	return &EliteArchive{
		fitness: make(map[uint16]float64),
		bands:   make(map[uint16][]uint64),
	}
}

func eliteBinFromFrame(frame *[128]uint64) uint16 {

	if frame == nil {
		return 0
	}

	affWord := core.Cfg.Value.Region.Affinity.Start
	if affWord < 0 || affWord >= len(frame) {
		return 0
	}

	shift := core.Cfg.System.MapElitesGridShift
	if shift == 0 {
		shift = 8
	}

	if shift >= 64 {
		shift = 63
	}

	return uint16(frame[affWord] >> (64 - shift))
}

func snapshotProgramBand(frame *[128]uint64) []uint64 {

	reg := core.Cfg.Value.Region.Program
	nWords := int((reg.Bits + 63) / 64)
	out := make([]uint64, nWords)

	for offset := 0; offset < nWords; offset++ {
		idx := reg.Start + offset
		if idx >= 0 && idx < len(frame) {
			out[offset] = frame[idx]
		}
	}

	return out
}

func applyProgramBand(dst *[128]uint64, band []uint64) {

	reg := core.Cfg.Value.Region.Program
	nWords := int((reg.Bits + 63) / 64)

	for offset := 0; offset < nWords && offset < len(band); offset++ {
		idx := reg.Start + offset
		if idx >= 0 && idx < len(dst) {
			dst[idx] = band[offset]
		}
	}
}

/*
StoreIfBetter retains the program band when fitness exceeds the elite already
held for this frame's bin.
*/
func (archive *EliteArchive) StoreIfBetter(frame *[128]uint64, fitness float64) {

	if archive == nil || frame == nil {
		return
	}

	bin := eliteBinFromFrame(frame)

	archive.mu.Lock()
	defer archive.mu.Unlock()

	if prev, exists := archive.fitness[bin]; exists && prev >= fitness {
		return
	}

	archive.fitness[bin] = fitness
	archive.bands[bin] = snapshotProgramBand(frame)
}

/*
TryInject copies a neighbor elite program band into dst. Returns true when a
band was applied.
*/
func (archive *EliteArchive) TryInject(dst *[128]uint64, rng *rand.Rand) bool {

	if archive == nil || dst == nil || rng == nil {
		return false
	}

	base := int(eliteBinFromFrame(dst))
	offsets := []int{0, 1, -1}

	rng.Shuffle(len(offsets), func(i, j int) {
		offsets[i], offsets[j] = offsets[j], offsets[i]
	})

	archive.mu.Lock()
	defer archive.mu.Unlock()

	for _, off := range offsets {
		idx := base + off
		if idx < 0 {
			idx = 0
		}

		bin := uint16(idx)
		band, ok := archive.bands[bin]
		if !ok || len(band) == 0 {
			continue
		}

		applyProgramBand(dst, band)

		return true
	}

	return false
}
