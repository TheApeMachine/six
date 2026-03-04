package textgen

import (
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

// BuildBoundaryFP encodes prefix+suffix into a normalized combined fingerprint.
func BuildBoundaryFP(prefix, suffix string) geometry.PhaseDial {
	D := numeric.NBasis
	fp := geometry.NewPhaseDial().Encode(prefix)
	if suffix == "" {
		return fp
	}
	fpS := geometry.NewPhaseDial().Encode(suffix)
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
	Tokens []string
	Text   string
	Source int
	Length int
}

// SpanMemory is the shared multi-length span substrate used by all tests.
type SpanMemory struct {
	Substrate  *geometry.HybridSubstrate
	Candidates []int
	Index      []SpanMeta
}

// BuildSpanMemory tokenizes each corpus string, extracts FibWindow spans,
// encodes them, and returns the ready SpanMemory.
func BuildSpanMemory(corpus []string) SpanMemory {
	sub := geometry.NewHybridSubstrate()
	var filter data.Chord
	var index []SpanMeta

	for corpIdx, fn := range corpus {
		toks := tokenize(fn)
		for _, sLen := range numeric.FibWindows {
			if len(toks) < sLen {
				continue
			}
			for start := 0; start <= len(toks)-sLen; start++ {
				span := make([]string, sLen)
				copy(span, toks[start:start+sLen])
				text := detokenize(span)
				fp := geometry.NewPhaseDial().Encode(text)
				sub.Add(filter, fp, []byte(text))
				index = append(index, SpanMeta{Tokens: span, Text: text, Source: corpIdx, Length: sLen})
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
		perAngle := max(topK/nDial, 4)
		added := 0
		for _, r := range ranked {
			if !seen[r.Idx] {
				seen[r.Idx] = true
				out = append(out, ScoredSpan{r.Idx, r.Score})
				added++
			}
			if added >= perAngle {
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

// ExactMatch reports whether spanText is a substring of any corpus string.
func ExactMatch(corpus []string, spanText string) bool {
	for _, fn := range corpus {
		if len(spanText) > 0 && len(fn) > 0 {
			if contains(fn, spanText) {
				return true
			}
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// OverlapLen computes the suffix-prefix overlap between out and span.
func OverlapLen(out, span []string) int {
	maxOvl := len(out)
	if len(span) < maxOvl {
		maxOvl = len(span)
	}
	for ovl := maxOvl; ovl > 0; ovl-- {
		match := true
		for i := 0; i < ovl; i++ {
			if out[len(out)-ovl+i] != span[i] {
				match = false
				break
			}
		}
		if match {
			return ovl
		}
	}
	return 0
}
