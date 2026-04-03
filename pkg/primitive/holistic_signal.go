package primitive

import (
	"math"

	"github.com/theapemachine/six/pkg/core"
)

/*
ChunkedHolisticStrength scores [0,1] token-region similarity without relying on
longest contiguous runs. For every chunk of chunkBits, we take normalized
Hamming distance; the best (lowest) chunk must be at or below holisticMax to
count as a match. Strength is 1 - d/holisticMax on that best chunk.
*/
func ChunkedHolisticStrength(
	parent, workspace *Value,
	chunkBits int,
	holisticMax float64,
) (strength float64, matches bool) {

	if parent == nil || workspace == nil {
		return 0, false
	}

	if chunkBits <= 0 {
		chunkBits = core.Cfg.System.HolisticChunkBits
	}

	if holisticMax <= 0 {
		holisticMax = core.Cfg.System.HolisticHammingMax
	}

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	if tokenBits <= 0 {
		return 0, false
	}

	if chunkBits > tokenBits {
		chunkBits = tokenBits
	}

	base := core.Cfg.Value.Region.Tokens.Start
	bestNorm := float64(-1)

	for chunkStart := 0; chunkStart < tokenBits; chunkStart += chunkBits {
		width := chunkBits
		if chunkStart+width > tokenBits {
			width = tokenBits - chunkStart
		}

		if width <= 0 {
			break
		}

		var dist int

		if TokenAttentionActive(parent, workspace) {
			dist = tokenRegionHammingWindowMasked(
				parent,
				workspace,
				base,
				chunkStart,
				width,
			)
		} else {
			dist = tokenRegionHammingWindow(parent, workspace, base, chunkStart, width)
		}
		norm := float64(dist) / float64(width)

		if bestNorm < 0 || norm < bestNorm {
			bestNorm = norm
		}
	}

	if bestNorm < 0 {
		return 0, false
	}

	if bestNorm > holisticMax {
		return 0, false
	}

	strength = 1.0 - bestNorm/holisticMax

	if strength < 0 {
		return 0, false
	}

	if strength > 1 {
		strength = 1
	}

	matches = true

	return strength, matches
}

func tokenRegionHammingWindow(
	a, b *Value,
	base, bitStart, width int,
) int {

	if a == nil || b == nil {
		return 0
	}

	if width <= 0 {
		return 0
	}

	dist := 0

	for offset := 0; offset < width; offset++ {
		bit := bitStart + offset
		wordIdx := base + bit/64
		shift := bit % 64

		if wordIdx < 0 || wordIdx >= len(*a) || wordIdx >= len(*b) {
			break
		}

		va := (a[wordIdx] >> uint(shift)) & 1
		vb := (b[wordIdx] >> uint(shift)) & 1

		if va != vb {
			dist++
		}
	}

	return dist
}

func tokenRegionHammingWindowMasked(
	a, b *Value,
	base, bitStart, width int,
) int {

	if a == nil || b == nil {
		return 0
	}

	if width <= 0 {
		return 0
	}

	dist := 0

	for offset := 0; offset < width; offset++ {
		bit := bitStart + offset
		wordOffset := bit / 64
		shift := bit % 64
		wordIdx := base + wordOffset

		if wordIdx < 0 || wordIdx >= len(*a) || wordIdx >= len(*b) {
			break
		}

		maskA := TokenAttentionMaskForWord(a, wordOffset)
		maskB := TokenAttentionMaskForWord(b, wordOffset)

		va := (a[wordIdx] >> uint(shift)) & 1
		vb := (b[wordIdx] >> uint(shift)) & 1
		ma := (maskA >> uint(shift)) & 1
		mb := (maskB >> uint(shift)) & 1

		if va&ma != vb&mb {
			dist++
		}
	}

	return dist
}

/*
HolisticSubstrateScore is a fitness-facing scalar aligned with
SubstrateExploitScore magnitude when holistic strength fires.
*/
func HolisticSubstrateScore(parent, workspace *Value) float64 {

	strength, ok := ChunkedHolisticStrength(
		parent,
		workspace,
		core.Cfg.System.HolisticChunkBits,
		core.Cfg.System.HolisticHammingMax,
	)
	if !ok {
		return 0
	}

	tokenBits := float64(core.Cfg.Value.Region.Tokens.Bits)
	if tokenBits <= 0 {
		return 0
	}

	chunk := float64(core.Cfg.System.HolisticChunkBits)
	if chunk <= 0 {
		chunk = 512
	}

	chunkCount := math.Ceil(tokenBits / chunk)
	if chunkCount < 1 {
		chunkCount = 1
	}

	// Softer than linear in chunk count so holistic signal remains usable.
	return math.Min(1.0, strength/math.Sqrt(chunkCount))
}
