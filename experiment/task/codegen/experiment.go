package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/geometry"
)

// Experiment holds the state for the BVP text generation experiment suite.
type Experiment struct {
	Substrate *geometry.HybridSubstrate
}

// New creates a new textgen experiment.
func New() *Experiment {
	return &Experiment{
		Substrate: geometry.NewHybridSubstrate(),
	}
}

// pythonCorpus provides a small, deterministic corpus of Python function bodies
// for the span solver to learn from. Each entry is a complete function.
func pythonCorpus() []string {
	return []string{
		// Arithmetic / math
		"def factorial(n):\n    if n <= 1:\n        return 1\n    return n * factorial(n - 1)",
		"def fibonacci(n):\n    if n <= 1:\n        return n\n    a, b = 0, 1\n    for i in range(2, n + 1):\n        a, b = b, a + b\n    return b",
		"def gcd(a, b):\n    while b:\n        a, b = b, a % b\n    return a",
		"def lcm(a, b):\n    return a * b // gcd(a, b)",
		"def power(base, exp):\n    if exp == 0:\n        return 1\n    return base * power(base, exp - 1)",
		"def is_prime(n):\n    if n < 2:\n        return False\n    for i in range(2, int(n ** 0.5) + 1):\n        if n % i == 0:\n            return False\n    return True",
		"def abs_val(x):\n    if x < 0:\n        return -x\n    return x",
		"def max_val(a, b):\n    if a > b:\n        return a\n    return b",
		"def min_val(a, b):\n    if a < b:\n        return a\n    return b",
		"def sum_list(lst):\n    total = 0\n    for x in lst:\n        total += x\n    return total",

		// List operations
		"def reverse_list(lst):\n    result = []\n    for i in range(len(lst) - 1, -1, -1):\n        result.append(lst[i])\n    return result",
		"def find_max(lst):\n    if not lst:\n        return None\n    best = lst[0]\n    for x in lst[1:]:\n        if x > best:\n            best = x\n    return best",
		"def find_min(lst):\n    if not lst:\n        return None\n    best = lst[0]\n    for x in lst[1:]:\n        if x < best:\n            best = x\n    return best",
		"def contains(lst, target):\n    for x in lst:\n        if x == target:\n            return True\n    return False",
		"def count_occurrences(lst, target):\n    count = 0\n    for x in lst:\n        if x == target:\n            count += 1\n    return count",
		"def flatten(lst):\n    result = []\n    for item in lst:\n        if isinstance(item, list):\n            result.extend(flatten(item))\n        else:\n            result.append(item)\n    return result",
		"def unique(lst):\n    seen = set()\n    result = []\n    for x in lst:\n        if x not in seen:\n            seen.add(x)\n            result.append(x)\n    return result",
		"def zip_lists(a, b):\n    result = []\n    for i in range(min(len(a), len(b))):\n        result.append((a[i], b[i]))\n    return result",

		// String operations
		"def reverse_string(s):\n    return s[::-1]",
		"def is_palindrome(s):\n    return s == s[::-1]",
		"def count_chars(s):\n    counts = {}\n    for c in s:\n        counts[c] = counts.get(c, 0) + 1\n    return counts",
		"def capitalize_words(s):\n    words = s.split()\n    result = []\n    for w in words:\n        result.append(w[0].upper() + w[1:])\n    return ' '.join(result)",
		"def remove_duplicates(s):\n    seen = set()\n    result = []\n    for c in s:\n        if c not in seen:\n            seen.add(c)\n            result.append(c)\n    return ''.join(result)",

		// Sorting
		"def bubble_sort(lst):\n    n = len(lst)\n    for i in range(n):\n        for j in range(0, n - i - 1):\n            if lst[j] > lst[j + 1]:\n                lst[j], lst[j + 1] = lst[j + 1], lst[j]\n    return lst",
		"def insertion_sort(lst):\n    for i in range(1, len(lst)):\n        key = lst[i]\n        j = i - 1\n        while j >= 0 and lst[j] > key:\n            lst[j + 1] = lst[j]\n            j -= 1\n        lst[j + 1] = key\n    return lst",
		"def selection_sort(lst):\n    for i in range(len(lst)):\n        min_idx = i\n        for j in range(i + 1, len(lst)):\n            if lst[j] < lst[min_idx]:\n                min_idx = j\n        lst[i], lst[min_idx] = lst[min_idx], lst[i]\n    return lst",

		// Data structures
		"def binary_search(lst, target):\n    low, high = 0, len(lst) - 1\n    while low <= high:\n        mid = (low + high) // 2\n        if lst[mid] == target:\n            return mid\n        elif lst[mid] < target:\n            low = mid + 1\n        else:\n            high = mid - 1\n    return -1",
		"def merge_sorted(a, b):\n    result = []\n    i, j = 0, 0\n    while i < len(a) and j < len(b):\n        if a[i] <= b[j]:\n            result.append(a[i])\n            i += 1\n        else:\n            result.append(b[j])\n            j += 1\n    result.extend(a[i:])\n    result.extend(b[j:])\n    return result",

		// Higher-order / functional
		"def map_list(fn, lst):\n    result = []\n    for x in lst:\n        result.append(fn(x))\n    return result",
		"def filter_list(fn, lst):\n    result = []\n    for x in lst:\n        if fn(x):\n            result.append(x)\n    return result",
		"def reduce_list(fn, lst, initial):\n    acc = initial\n    for x in lst:\n        acc = fn(acc, x)\n    return acc",
	}
}

// Run executes all tests in the BVP text generation experiment suite.
func (experiment *Experiment) Run() error {
	corpus := pythonCorpus()

	// Compute corpus signature for regression tracking
	h := sha256.New()
	for _, s := range corpus {
		h.Write([]byte(s))
	}
	corpusSig := hex.EncodeToString(h.Sum(nil))

	console.Info("╔══════════════════════════════════════════════════════════════╗")
	console.Info("║  BVP Span Solver — Text Generation Experiment Suite        ║")
	console.Info("╚══════════════════════════════════════════════════════════════╝")
	console.Info(fmt.Sprintf("  Corpus: %d Python functions", len(corpus)))
	console.Info(fmt.Sprintf("  Corpus Hash: %s", corpusSig[:16]))

	// ── Test 1: Core BVP Span Solver ──
	console.Info("\n=============================================================")
	console.Info("TEST 1: Core BVP Span Solver (Retrieve + Vote + Refine)")
	console.Info("=============================================================")
	spanData := experiment.testSpanSolver(corpus)

	// ── Test 2: Span Ranking BVP ──
	console.Info("\n=============================================================")
	console.Info("TEST 2: Span Ranking BVP (Whole-Span Selection)")
	console.Info("=============================================================")
	rankingData := experiment.testSpanRanking(corpus)

	// ── Test 3: Span Chaining ──
	console.Info("\n=============================================================")
	console.Info("TEST 3: Span Chaining (Multi-Span Generation)")
	console.Info("=============================================================")
	chainingData := experiment.testSpanChaining(corpus)

	// ── Test 4: Overlap-Aware Span Chaining ──
	console.Info("\n=============================================================")
	console.Info("TEST 4: Overlap-Aware Span Chaining")
	console.Info("=============================================================")
	overlapData := experiment.testOverlapChaining(corpus)

	// ── Test 5: Long Program Generation ──
	console.Info("\n=============================================================")
	console.Info("TEST 5: Long Program Generation")
	console.Info("=============================================================")
	extendedCorpus := append(corpus, longCorpus()...)
	longGenData := experiment.testLongGeneration(extendedCorpus)

	// ── Test 6: Out-of-Corpus Compositional Generation ──
	console.Info("\n=============================================================")
	console.Info("TEST 6: Out-of-Corpus Compositional Generation (Zero Heuristics)")
	console.Info("=============================================================")
	compGenData := experiment.testCompositionalGeneration(extendedCorpus)

	// ── Test 7: Structural Sensitivity Probe ──
	console.Info("\n=============================================================")
	console.Info("TEST 7: Structural Sensitivity Probe")
	console.Info("=============================================================")
	structSensData := experiment.testStructuralSensitivity()

	// ── Test 8: Eigenmode Probe ──
	console.Info("\n=============================================================")
	console.Info("TEST 8: Eigenmode Probe (PCA over Span Fingerprints)")
	console.Info("=============================================================")
	eigenmodeData := experiment.testEigenmodeProbe(extendedCorpus)

	// ── Test 9: Phase-Triggered Manifold Bridging ──
	console.Info("\n=============================================================")
	console.Info("TEST 9: Phase-Triggered Manifold Bridging")
	console.Info("=============================================================")
	bridgingData := experiment.testPhaseBridging(extendedCorpus)

	// ── Test 10: Cantilever-Gated Span Retrieval ──
	console.Info("\n=============================================================")
	console.Info("TEST 10: Cantilever-Gated Span Retrieval")
	console.Info("=============================================================")
	cantileverData := experiment.testCantileverGating(extendedCorpus)

	// ── Test 11: Relative Cantilever Scale Selection ──
	console.Info("\n=============================================================")
	console.Info("TEST 11: Relative Cantilever Scale Selection")
	console.Info("=============================================================")
	relCantData := experiment.testRelativeCantilever(extendedCorpus)

	// ── Test 12: Chord-Based Generation (BVP over FibWindow Chords) ──
	console.Info("\n=============================================================")
	console.Info("TEST 12: Chord-Based Generation (BVP over FibWindow Chords)")
	console.Info("=============================================================")
	chordGenData := experiment.testChordGeneration(extendedCorpus)
	// ── Test 13: Pipeline Integration ──
	console.Info("\n=============================================================")
	console.Info("TEST 13: Pipeline Integration (HuggingFace → Tokenizer → LSM)")
	console.Info("=============================================================")
	pipelineData := NewPipeline()
	pipelineData.Run()

	report := ValidationReport{
		CorpusHash:     corpusSig,
		CorpusSize:     len(corpus),
		SpanData:       spanData,
		RankingData:    rankingData,
		ChainingData:   chainingData,
		OverlapData:    overlapData,
		LongGenData:    longGenData,
		CompGenData:    compGenData,
		StructSensData: structSensData,
		EigenmodeData:  eigenmodeData,
		BridgingData:   bridgingData,
		CantileverData: cantileverData,
		RelCantData:    relCantData,
		ChordGenData:   chordGenData,
		PipelineData:   pipelineData,
	}

	console.Info("\n=======================================================")
	console.Info("Exporting dynamically generated TeX Section & ECharts figures...")
	console.Info("=======================================================")
	if err := generatePaperOutput(report); err != nil {
		console.Warn(fmt.Sprintf("Failed to generate LaTeX inclusions: %v", err))
	} else {
		console.Info("Successfully generated paper/include/textgen/textgen.tex and figures.")
	}

	return nil
}

// tokenize splits text into simple whitespace+punctuation tokens.
// This is intentionally naive — the experiment tests the BVP mechanism, not tokenization.
func tokenize(text string) []string {
	// Split on whitespace first
	words := strings.Fields(text)
	var tokens []string
	for _, w := range words {
		// Keep punctuation-attached tokens as-is for now
		tokens = append(tokens, w)
	}
	return tokens
}

// detokenize joins tokens back into text.
func detokenize(tokens []string) string {
	return strings.Join(tokens, " ")
}
