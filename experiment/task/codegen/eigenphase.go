package codegen

import (
	"math"
	"sort"
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/tokenizer"
)

// buildEigenMode builds an EigenMode topology from a text corpus.
func buildEigenMode(corpus []string) *geometry.EigenMode {
	var fullCorpus []byte
	for _, fn := range corpus {
		fullCorpus = append(fullCorpus, []byte(fn)...)
		fullCorpus = append(fullCorpus, '\n')
	}
	chords := textToChords(string(fullCorpus))
	ei := geometry.NewEigenMode()
	if err := ei.BuildMultiScaleCooccurrence(chords); err != nil {
		return geometry.NewEigenMode()
	}
	return ei
}

// textToChords tokenizes a raw string into atomic topological chords.
func textToChords(text string) []data.Chord {
	chords := make([]data.Chord, len(text))
	for i, b := range []byte(text) {
		chords[i] = tokenizer.BaseChord(b)
	}
	return chords
}

// IsGeometricallyClosed wraps the native geometry validation for raw text.
func IsGeometricallyClosed(ei *geometry.EigenMode, code string, anchorPhase float64) bool {
	return ei.IsGeometricallyClosed(textToChords(code), anchorPhase)
}

// weightedCircularMean wraps the native Toroidal weighting function over chords.
func weightedCircularMean(ei *geometry.EigenMode, text string) (float64, float64) {
	return ei.WeightedCircularMean(textToChords(text))
}

// isValidSyntax performs a lightweight AST-like verification of Python syntax.
// It checks for balanced parentheses, braces, brackets.
func isValidSyntax(code string) bool {
	stack := make([]rune, 0)
	for _, c := range code {
		switch c {
		case '(', '{', '[':
			stack = append(stack, c)
		case ')':
			if len(stack) == 0 || stack[len(stack)-1] != '(' {
				return false
			}
			stack = stack[:len(stack)-1]
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return false
			}
			stack = stack[:len(stack)-1]
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

func normalizeVec256(v *[256]float64) {
	var normSq float64
	for _, x := range v {
		normSq += x * x
	}
	if normSq < 1e-24 {
		return
	}
	norm := math.Sqrt(normSq)
	for i := range v {
		v[i] /= norm
	}
}

// classifyRole tags a span with its dominant structural role.
func classifyRole(text string) string {
	hasDefStart := strings.HasPrefix(text, "def ")
	hasReturn := strings.Contains(text, "return")
	hasFor := strings.Contains(text, "for ")
	hasWhile := strings.Contains(text, "while ")
	hasIf := strings.Contains(text, "if ")
	hasCall := strings.Contains(text, "(") && strings.Contains(text, ")")
	hasAssign := strings.Contains(text, "=") && !strings.Contains(text, "==")
	if hasDefStart {
		return "header"
	}
	if hasReturn && !hasFor && !hasWhile {
		return "return"
	}
	if hasFor || hasWhile {
		return "loop"
	}
	if hasIf {
		return "conditional"
	}
	if hasAssign {
		return "assignment"
	}
	if hasCall {
		return "call"
	}
	return "other"
}

// cantileverSpan holds span metadata for cantilever tests.
type cantileverSpan struct {
	tokens     []string
	source     int
	spanLen    int
	eigenPhase float64
	conc       float64
}

// CantileverEstimate holds the result of a cantilever probe.
type CantileverEstimate struct {
	MaxCoherentScale int
	Extent           int
	ScaleScores      []float64
}

func cantileverProbe(
	boundaryFP geometry.PhaseDial,
	spanIndex []cantileverSpan,
	substrate *geometry.HybridSubstrate,
	_ int,
	threshold float64,
) CantileverEstimate {
	reversed := make([]int, len(numeric.FibWindows))
	copy(reversed, numeric.FibWindows)
	sort.Sort(sort.Reverse(sort.IntSlice(reversed)))
	var scores []float64
	maxCoherent := 0
	extent := 0
	for _, w := range reversed {
		bestSim := 0.0
		for idx, sp := range spanIndex {
			if sp.spanLen != w {
				continue
			}
			s := boundaryFP.Similarity(substrate.Entries[idx].Fingerprint)
			if s > bestSim {
				bestSim = s
			}
		}
		scores = append(scores, bestSim)
		if bestSim > threshold {
			if w > maxCoherent {
				maxCoherent = w
			}
			extent += w
		}
	}
	return CantileverEstimate{
		MaxCoherentScale: maxCoherent,
		Extent:           extent,
		ScaleScores:      scores,
	}
}

// RelativeCantilever holds the relative scale analysis.
type RelativeCantilever struct {
	Scores     []float64
	Ratios     []float64
	MaxSafeLen int
}

func relativeCantileverProbe(
	boundaryFP geometry.PhaseDial,
	spanIndex []cantileverSpan,
	substrate *geometry.HybridSubstrate,
	_ int,
	ratioThreshold float64,
) RelativeCantilever {
	windows := make([]int, len(numeric.FibWindows))
	copy(windows, numeric.FibWindows)
	sort.Ints(windows)
	scores := make([]float64, len(windows))
	for wi, w := range windows {
		bestSim := 0.0
		for idx, sp := range spanIndex {
			if sp.spanLen != w {
				continue
			}
			s := boundaryFP.Similarity(substrate.Entries[idx].Fingerprint)
			if s > bestSim {
				bestSim = s
			}
		}
		scores[wi] = bestSim
	}
	ratios := make([]float64, len(windows)-1)
	for i := 0; i < len(windows)-1; i++ {
		if scores[i] > 0 {
			ratios[i] = scores[i+1] / scores[i]
		}
	}
	maxSafe := windows[0]
	for i, r := range ratios {
		if r >= ratioThreshold {
			maxSafe = windows[i+1]
		} else {
			break
		}
	}
	reversedScores := make([]float64, len(scores))
	for i, s := range scores {
		reversedScores[len(scores)-1-i] = s
	}
	return RelativeCantilever{
		Scores:     reversedScores,
		Ratios:     ratios,
		MaxSafeLen: maxSafe,
	}
}
