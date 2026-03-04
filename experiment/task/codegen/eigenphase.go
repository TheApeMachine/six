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

// eigenPhaseTable wraps geometry.EigenMode with chord-native co-occurrence
// and provides weighted circular mean over PhaseTheta for text spans.
type eigenPhaseTable struct {
	ei     *geometry.EigenMode
	weight [256]float64 // structural informativeness per byte
}

// buildEigenPhaseTable builds chord sequence from corpus, runs EigenMode
// BuildMultiScaleCooccurrence, returns wrapper for weighted phase lookup.
func buildEigenPhaseTable(corpus []string) *eigenPhaseTable {
	var fullCorpus []byte
	for _, fn := range corpus {
		fullCorpus = append(fullCorpus, []byte(fn)...)
		fullCorpus = append(fullCorpus, '\n')
	}
	chords := make([]data.Chord, len(fullCorpus))
	for i, b := range fullCorpus {
		chords[i] = tokenizer.BaseChord(b)
	}
	ei := geometry.NewEigenMode()
	if err := ei.BuildMultiScaleCooccurrence(chords); err != nil {
		// Fallback: zero phases if build fails (e.g. tiny corpus)
		ei = geometry.NewEigenMode()
	}
	table := &eigenPhaseTable{ei: ei}
	for i := 0; i < 256; i++ {
		b := byte(i)
		chord := tokenizer.BaseChord(b)
		bin := data.ChordBin(&chord)
		// Derive structural informativeness dynamically from eigenvector magnitudes
		// (embodied in FreqTheta computed from magsTheta during EigenMode init).
		table.weight[i] = table.ei.FreqTheta[bin]
	}
	return table
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

func (t *eigenPhaseTable) weightedCircularMean(text string) (phase float64, concentration float64) {
	if len(text) == 0 {
		return 0, 0
	}
	var sinSum, cosSum, wSum float64
	for _, b := range []byte(text) {
		chord := tokenizer.BaseChord(b)
		theta, _ := t.ei.PhaseForChord(&chord)
		w := t.weight[b]
		sinSum += w * math.Sin(theta)
		cosSum += w * math.Cos(theta)
		wSum += w
	}
	if wSum == 0 {
		return 0, 0
	}
	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum
	return
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
