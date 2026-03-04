package codegen

import (
	"fmt"
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testStructuralSensitivity implements Test 7: Structural Sensitivity Probe.
//
// Tests whether the encoding is structure-sensitive or token-count-sensitive.
//
// For each function prefix, we encode:
//   1. prefix only
//   2. prefix + comment/whitespace (structural no-op)
//   3. prefix + unrelated token (structural noise)
//   4. prefix + correct continuation (structural extension)
//
// If the encoding is structure-sensitive:
//   - correct continuation should produce the largest directional change
//   - comment/whitespace should barely move the vector
//   - unrelated tokens should move it, but in the wrong direction
//
// Measured by:
//   - Δsim: how much the similarity to a query changes
//   - Δnorm: the magnitude of the vector displacement
//   - direction: cosine between (Fext - Fprefix) and (Ffull - Fprefix)
func (experiment *Experiment) testStructuralSensitivity() StructSensResult {

	sim := func(a, b numeric.PhaseDial) float64 {
		var dot complex128
		var na, nb float64
		for i := range a {
			dot += cmplx.Conj(a[i]) * b[i]
			na += real(a[i])*real(a[i]) + imag(a[i])*imag(a[i])
			nb += real(b[i])*real(b[i]) + imag(b[i])*imag(b[i])
		}
		if na == 0 || nb == 0 {
			return 0
		}
		return real(dot) / (math.Sqrt(na) * math.Sqrt(nb))
	}

	// Vector displacement magnitude
	vecDist := func(a, b numeric.PhaseDial) float64 {
		var sum float64
		for i := range a {
			d := a[i] - b[i]
			sum += real(d)*real(d) + imag(d)*imag(d)
		}
		return math.Sqrt(sum)
	}

	// Directional alignment: cos angle between (ext - prefix) and (full - prefix)
	dirAlign := func(prefix, ext, full numeric.PhaseDial) float64 {
		D := len(prefix)
		dExt := make(numeric.PhaseDial, D)
		dFull := make(numeric.PhaseDial, D)
		for i := 0; i < D; i++ {
			dExt[i] = ext[i] - prefix[i]
			dFull[i] = full[i] - prefix[i]
		}
		return sim(dExt, dFull)
	}

	type probe struct {
		name      string
		prefix    string
		full      string // the correct full function
		comment   string // prefix + structural no-op
		noise     string // prefix + unrelated token
		correct   string // prefix + correct continuation
	}

	probes := []probe{
		{
			name:    "factorial",
			prefix:  "def factorial(n):",
			full:    "def factorial(n): if n <= 1: return 1 return n * factorial(n - 1)",
			comment: "def factorial(n): #",
			noise:   "def factorial(n): import",
			correct: "def factorial(n): if n <= 1:",
		},
		{
			name:    "binary_search",
			prefix:  "def binary_search(lst, target):",
			full:    "def binary_search(lst, target): low, high = 0, len(lst) - 1 while low <= high: mid = (low + high) // 2",
			comment: "def binary_search(lst, target): #",
			noise:   "def binary_search(lst, target): import",
			correct: "def binary_search(lst, target): low, high =",
		},
		{
			name:    "filter_list",
			prefix:  "def filter_list(fn, lst):",
			full:    "def filter_list(fn, lst): result = [] for x in lst: if fn(x): result.append(x) return result",
			comment: "def filter_list(fn, lst): #",
			noise:   "def filter_list(fn, lst): import",
			correct: "def filter_list(fn, lst): result = []",
		},
		{
			name:    "find_max",
			prefix:  "def find_max(lst):",
			full:    "def find_max(lst): if not lst: return None best = lst[0] for x in lst[1:]: if x > best: best = x return best",
			comment: "def find_max(lst): #",
			noise:   "def find_max(lst): import",
			correct: "def find_max(lst): if not lst:",
		},
		{
			name:    "dfs",
			prefix:  "def dfs(graph, start):",
			full:    "def dfs(graph, start): visited = set() stack = [start] result = [] while stack: node = stack.pop()",
			comment: "def dfs(graph, start): #",
			noise:   "def dfs(graph, start): import",
			correct: "def dfs(graph, start): visited = set()",
		},
	}

	var entries []StructSensEntry

	for _, p := range probes {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.name))

		fpPrefix := numeric.EncodeText(p.prefix)
		fpFull := numeric.EncodeText(p.full)
		fpComment := numeric.EncodeText(p.comment)
		fpNoise := numeric.EncodeText(p.noise)
		fpCorrect := numeric.EncodeText(p.correct)

		// Similarities to the full function
		simPrefixFull := sim(fpPrefix, fpFull)
		simCommentFull := sim(fpComment, fpFull)
		simNoiseFull := sim(fpNoise, fpFull)
		simCorrectFull := sim(fpCorrect, fpFull)

		// Vector displacement magnitudes from prefix
		distComment := vecDist(fpPrefix, fpComment)
		distNoise := vecDist(fpPrefix, fpNoise)
		distCorrect := vecDist(fpPrefix, fpCorrect)

		// Directional alignment: does the extension move toward the full function?
		dirComment := dirAlign(fpPrefix, fpComment, fpFull)
		dirNoise := dirAlign(fpPrefix, fpNoise, fpFull)
		dirCorrect := dirAlign(fpPrefix, fpCorrect, fpFull)

		console.Info(fmt.Sprintf("  │  Prefix→Full similarity: %.4f", simPrefixFull))
		console.Info("  │")
		console.Info(fmt.Sprintf("  │  %-12s  sim→full   Δdist    dir→full", "Extension"))
		console.Info(fmt.Sprintf("  │  %-12s  -------   -----    --------", ""))
		console.Info(fmt.Sprintf("  │  %-12s  %.4f    %.4f   %.4f   %s", "comment (#)", simCommentFull, distComment, dirComment, p.comment))
		console.Info(fmt.Sprintf("  │  %-12s  %.4f    %.4f   %.4f   %s", "noise", simNoiseFull, distNoise, dirNoise, p.noise))
		console.Info(fmt.Sprintf("  │  %-12s  %.4f    %.4f   %.4f   %s", "correct", simCorrectFull, distCorrect, dirCorrect, p.correct))

		// Analysis
		correctBest := simCorrectFull > simCommentFull && simCorrectFull > simNoiseFull
		correctDirection := dirCorrect > dirComment && dirCorrect > dirNoise
		commentLeast := distComment < distNoise && distComment < distCorrect

		console.Info("  │")
		console.Info(fmt.Sprintf("  │  Correct has highest sim→full: %v", correctBest))
		console.Info(fmt.Sprintf("  │  Correct points most toward full: %v", correctDirection))
		console.Info(fmt.Sprintf("  │  Comment moves vector least: %v", commentLeast))
		console.Info(fmt.Sprintf("  └─ Structure-sensitive: %v", correctBest && correctDirection))

		entry := StructSensEntry{
			Name:   p.name,
			Prefix: p.prefix,

			SimPrefixFull:  simPrefixFull,
			SimCommentFull: simCommentFull,
			SimNoiseFull:   simNoiseFull,
			SimCorrectFull: simCorrectFull,

			DistComment: distComment,
			DistNoise:   distNoise,
			DistCorrect: distCorrect,

			DirComment: dirComment,
			DirNoise:   dirNoise,
			DirCorrect: dirCorrect,

			CorrectBestSim: correctBest,
			CorrectBestDir: correctDirection,
			CommentLeast:   commentLeast,
		}
		entries = append(entries, entry)
	}

	// Summary
	structSensCount := 0
	bestSimCount := 0
	bestDirCount := 0
	leastMoveCount := 0
	for _, e := range entries {
		if e.CorrectBestSim {
			bestSimCount++
		}
		if e.CorrectBestDir {
			bestDirCount++
		}
		if e.CommentLeast {
			leastMoveCount++
		}
		if e.CorrectBestSim && e.CorrectBestDir {
			structSensCount++
		}
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Correct has highest sim→full: %d/%d", bestSimCount, len(entries)))
	console.Info(fmt.Sprintf("  Correct points most toward full: %d/%d", bestDirCount, len(entries)))
	console.Info(fmt.Sprintf("  Comment moves vector least: %d/%d", leastMoveCount, len(entries)))
	console.Info(fmt.Sprintf("  Structure-sensitive (both): %d/%d", structSensCount, len(entries)))

	return StructSensResult{
		Entries:         entries,
		BestSimCount:    bestSimCount,
		BestDirCount:    bestDirCount,
		LeastMoveCount:  leastMoveCount,
		StructSensCount: structSensCount,
	}
}
