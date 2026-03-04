package vision

import (
	"bytes"
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

// BuildBoundaryFP encodes prefix+suffix into a normalized combined fingerprint.
func BuildBoundaryFP(prefix, suffix []byte) geometry.PhaseDial {
	D := numeric.NBasis
	fp := geometry.NewPhaseDial().Encode(string(prefix))
	if len(suffix) == 0 {
		return fp
	}
	fpS := geometry.NewPhaseDial().Encode(string(suffix))
	out := make(geometry.PhaseDial, D)
	var norm float64
	for k := 0; k < D; k++ {
		out[k] = fp[k] + fpS[k]
		r, im := real(out[k]), imag(out[k])
		norm += r*r + im*im
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for k := 0; k < D; k++ {
			out[k] /= complex(norm, 0)
		}
	}
	return out
}

// SpanMeta holds metadata for one stored span.
type SpanMeta struct {
	Tokens []byte
	Source int
	Length int
}

// SpanMemory is the shared multi-length span substrate used by all tests.
type SpanMemory struct {
	Substrate  *geometry.HybridSubstrate
	Candidates []int
	Index      []SpanMeta
}

// BuildSpanMemory processes each corpus image, extracts FibWindow spans,
// encodes them, and returns the ready SpanMemory.
func BuildSpanMemory(corpus [][]byte) SpanMemory {
	sub := geometry.NewHybridSubstrate()
	var filter data.Chord
	var index []SpanMeta

	for corpIdx, toks := range corpus {
		for _, sLen := range numeric.FibWindows {
			if len(toks) < sLen {
				continue
			}
			for start := 0; start <= len(toks)-sLen; start++ {
				span := make([]byte, sLen)
				copy(span, toks[start:start+sLen])
				fp := geometry.NewPhaseDial().Encode(string(span))
				sub.Add(filter, fp, span)
				index = append(index, SpanMeta{Tokens: span, Source: corpIdx, Length: sLen})
			}
		}
	}

	cands := make([]int, len(sub.Entries))
	for i := range cands {
		cands[i] = i
	}
	return SpanMemory{Substrate: sub, Candidates: cands, Index: index}
}

// ScoredSpan pairs an index with its retrieval score.
type ScoredSpan struct {
	Idx   int
	Score float64
}

// maxInt returns max integer.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RetrieveDiverse sweeps nDial torus angles, deduplicates, and returns up to
// topK candidates sorted by score descending.
func (sm SpanMemory) RetrieveDiverse(fpBoundary geometry.PhaseDial, nDial, topK int) []ScoredSpan {
	D := numeric.NBasis
	seen := make(map[int]bool)
	var out []ScoredSpan

	for d := 0; d < nDial; d++ {
		alpha := float64(d) * (2.0 * math.Pi / float64(nDial))
		rotated := make(geometry.PhaseDial, D)
		if d == 0 {
			copy(rotated, fpBoundary)
		} else {
			f1 := cmplx.Rect(1.0, alpha)
			f2 := cmplx.Rect(1.0, -alpha*0.5)
			for k := 0; k < D; k++ {
				if k < D/2 {
					rotated[k] = fpBoundary[k] * f1
				} else {
					rotated[k] = fpBoundary[k] * f2
				}
			}
		}
		ranked := sm.Substrate.PhaseDialRank(sm.Candidates, rotated)
		
		maxPerAngle := maxInt(topK/nDial, 4)
		added := 0
		for _, r := range ranked {
			if !seen[r.Idx] {
				seen[r.Idx] = true
				out = append(out, ScoredSpan{r.Idx, r.Score})
				added++
			}
			if added >= maxPerAngle {
				break
			}
		}
		if len(out) >= topK {
			break
		}
	}

	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Score > out[i].Score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// OverlapLen computes the suffix-prefix overlap between out and span.
func OverlapLen(out, span []byte) int {
	maxOvl := len(out)
	if len(span) < maxOvl {
		maxOvl = len(span)
	}
	for ovl := maxOvl; ovl > 0; ovl-- {
		if bytes.Equal(out[len(out)-ovl:], span[:ovl]) {
			return ovl
		}
	}
	return 0
}
