package codegen

import (
	"fmt"
	"math/bits"
	"strings"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/gpu/metal"
	"github.com/theapemachine/six/numeric"
)

// ────────────────────────────────────────────────────────────────────
// Test 12: Chord-Based Generation (BVP over FibWindow Chords)
//
// Architecture:
//   1. Each byte value gets a deterministic base chord (K bits set in 512-bit space)
//   2. FibWindow spans are stored as OR-aggregated chords
//   3. BestFill (GPU popcount) finds the stored chord that best matches the prompt
//   4. ChordHole computes what's structurally missing
//   5. ReverseLookup recovers the next byte
//
// This test bypasses the PhaseDial/fingerprint layer entirely.
// Generation is pure bitwise Boolean algebra.
// ────────────────────────────────────────────────────────────────────

const (
	// baseK is how many bits each byte value gets in its base chord.
	// 5 bits per byte means a 21-byte FibWindow span has at most 105
	// bits set out of 512 — ~20% density, good for discrimination.
	baseK = 5
)

// byteBaseChords is the deterministic mapping from byte value (0-255)
// to its base 512-bit chord. Computed once.
var byteBaseChords [256]data.Chord

func init() {
	// Use a deterministic hash-like spreading function so that nearby
	// byte values don't cluster in the same chord region.
	// We use coprime multipliers to spread K bits across 512 positions.
	multipliers := [baseK]int{1, 7, 31, 127, 211}
	offsets := [baseK]int{0, 13, 97, 257, 389}

	for b := 0; b < 256; b++ {
		for k := 0; k < baseK; k++ {
			bitIdx := (b*multipliers[k] + offsets[k]) % numeric.NBasis
			byteBaseChords[b].Set(bitIdx)
		}
	}
}

// ─── Storage types ──────────────────────────────────────────────────

// ChordEntry stores one FibWindow chord along with its origin info.
type ChordEntry struct {
	Chord    data.Chord
	Position int // start position in corpus
	Scale    int // FibWindow size (3, 5, 8, 13, 21)
}

// ChordStore is a flat array of chords for GPU BestFill, plus metadata.
type ChordStore struct {
	Entries []ChordEntry
	// Flat chord array for GPU dispatch (contiguous memory)
	flatChords []data.Chord
}

func newChordStore() *ChordStore {
	return &ChordStore{}
}

func (cs *ChordStore) insert(chord data.Chord, pos, scale int) {
	cs.Entries = append(cs.Entries, ChordEntry{
		Chord:    chord,
		Position: pos,
		Scale:    scale,
	})
}

// buildFlat creates the contiguous chord array for GPU dispatch.
func (cs *ChordStore) buildFlat() {
	cs.flatChords = make([]data.Chord, len(cs.Entries))
	for i, e := range cs.Entries {
		cs.flatChords[i] = e.Chord
	}
}

// bestFillGPU calls the Metal compute shader to find the best match.
func (cs *ChordStore) bestFillGPU(context *data.Chord) (int, float64, error) {
	if len(cs.flatChords) == 0 {
		return 0, 0, fmt.Errorf("empty chord store")
	}
	return metal.BestFill(
		unsafe.Pointer(&cs.flatChords[0]),
		len(cs.flatChords),
		unsafe.Pointer(context),
		0,
	)
}

// bestFillSoftware is a CPU fallback for environments without Metal.
func (cs *ChordStore) bestFillSoftware(context *data.Chord) (int, float64) {
	bestIdx := 0
	bestScore := 0.0

	for i, entry := range cs.Entries {
		matchCount := 0
		noiseCount := 0

		for j := 0; j < numeric.ChordBlocks; j++ {
			cBits := entry.Chord[j]
			aBits := context[j]
			matchCount += bits.OnesCount64(cBits & aBits)
			noiseCount += bits.OnesCount64(cBits &^ aBits)
		}

		score := float64(matchCount) / float64(matchCount+noiseCount+1)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx, bestScore
}

// ─── Corpus Ingestion ───────────────────────────────────────────────

// buildChordStore ingests a corpus into the ChordStore at all FibWindow scales.
func buildChordStore(corpus []byte) *ChordStore {
	store := newChordStore()

	for _, windowSize := range numeric.FibWindows {
		for pos := 0; pos+windowSize <= len(corpus); pos++ {
			// Build the window chord by OR-ing base chords
			var windowChord data.Chord
			for i := 0; i < windowSize; i++ {
				b := corpus[pos+i]
				for j := 0; j < numeric.ChordBlocks; j++ {
					windowChord[j] |= byteBaseChords[b][j]
				}
			}
			store.insert(windowChord, pos, windowSize)
		}
	}

	store.buildFlat()
	return store
}

// ─── Generation Loop ────────────────────────────────────────────────

// ChordGenStep captures one step of the generation process.
type ChordGenStep struct {
	StepNum   int
	BestScore float64
	BestScale int
	BestPos   int
	HoleBits  int // how many bits in the structural hole
	EmittedN  int // how many new bytes emitted from this match
	Emitted   string
}

// ChordGenEntry holds the result for one prompt.
type ChordGenEntry struct {
	Prompt    string
	Generated string
	Steps     []ChordGenStep
	Tokens    int
	HasReturn bool
	HasColon  bool
}

// ChordGenResult is the output of Test 12.
type ChordGenResult struct {
	Entries    []ChordGenEntry
	StoreSize  int
	CorpusSize int
}

// generateFromChords runs the BVP span-chain generation loop.
//
// Step 1: BestFill locates the prompt in the corpus → set corpusHead.
// Step N: Context chord from corpus tail → BestFill → emit forward → advance.
// No overlap detection. Runs until stop token (\ndef) or end of corpus.
func generateFromChords(store *ChordStore, corpus []byte, prompt string) ChordGenEntry {
	const maxBytes = 4096 // safety cap

	promptBytes := []byte(prompt)
	output := make([]byte, 0, 512)
	output = append(output, promptBytes...)
	steps := make([]ChordGenStep, 0)

	// Step 1: Locate the prompt in the corpus.
	var promptChord data.Chord
	for _, b := range promptBytes {
		for j := 0; j < numeric.ChordBlocks; j++ {
			promptChord[j] |= byteBaseChords[b][j]
		}
	}

	bestIdx, bestScore := store.bestFillSoftware(&promptChord)
	entry := store.Entries[bestIdx]

	// Set the read head to where the prompt ends in the corpus.
	corpusHead := entry.Position + len(promptBytes)
	if corpusHead >= len(corpus) {
		corpusHead = entry.Position + entry.Scale
	}
	if corpusHead >= len(corpus) {
		return ChordGenEntry{Prompt: prompt, Generated: "(corpus too short)", Tokens: len(promptBytes)}
	}

	steps = append(steps, ChordGenStep{
		StepNum:   1,
		BestScore: bestScore,
		BestScale: entry.Scale,
		BestPos:   entry.Position,
		HoleBits:  0,
		EmittedN:  0,
		Emitted:   fmt.Sprintf("(located at corpus pos %d)", entry.Position),
	})

	// Generate until stop token or end of corpus.
	step := 1
	for corpusHead < len(corpus) && len(output) < maxBytes {
		step++

		// Build context chord from the corpus tail (last 21 bytes before corpusHead)
		contextWindow := 21
		ctxStart := corpusHead - contextWindow
		if ctxStart < 0 {
			ctxStart = 0
		}

		var activeContext data.Chord
		for i := ctxStart; i < corpusHead; i++ {
			b := corpus[i]
			for j := 0; j < numeric.ChordBlocks; j++ {
				activeContext[j] |= byteBaseChords[b][j]
			}
		}

		// BestFill
		bestIdx, bestScore := store.bestFillSoftware(&activeContext)
		entry := store.Entries[bestIdx]

		// ChordHole
		hole := data.ChordHole(&entry.Chord, &activeContext)
		holeBits := 0
		for j := 0; j < numeric.ChordBlocks; j++ {
			holeBits += bits.OnesCount64(hole[j])
		}

		// Emit from corpusHead forward
		emitEnd := corpusHead + entry.Scale
		if emitEnd > len(corpus) {
			emitEnd = len(corpus)
		}
		newBytes := corpus[corpusHead:emitEnd]

		// Check for stop token: \ndef marks a new function definition
		stopIdx := -1
		for i := 0; i < len(newBytes)-1; i++ {
			if newBytes[i] == '\n' && i+1 < len(newBytes) && newBytes[i+1] == 'd' {
				remaining := string(newBytes[i:])
				if len(remaining) >= 4 && remaining[:4] == "\ndef" {
					stopIdx = i
					break
				}
			}
		}

		if stopIdx >= 0 {
			newBytes = newBytes[:stopIdx]
			if len(newBytes) > 0 {
				output = append(output, newBytes...)
			}
			steps = append(steps, ChordGenStep{
				StepNum:   step,
				BestScore: bestScore,
				BestScale: entry.Scale,
				BestPos:   entry.Position,
				HoleBits:  holeBits,
				EmittedN:  len(newBytes),
				Emitted:   truncateStr(string(newBytes), 40) + " [STOP]",
			})
			break
		}

		output = append(output, newBytes...)
		corpusHead = emitEnd

		steps = append(steps, ChordGenStep{
			StepNum:   step,
			BestScore: bestScore,
			BestScale: entry.Scale,
			BestPos:   entry.Position,
			HoleBits:  holeBits,
			EmittedN:  len(newBytes),
			Emitted:   truncateStr(string(newBytes), 40),
		})
	}

	generated := string(output[len(promptBytes):])
	return ChordGenEntry{
		Prompt:    prompt,
		Generated: generated,
		Steps:     steps,
		Tokens:    len(output),
		HasReturn: strings.Contains(generated, "return"),
		HasColon:  strings.Contains(generated, ":"),
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ─── Test Runner ────────────────────────────────────────────────────

func (experiment *Experiment) testChordGeneration(corpusStrs []string) ChordGenResult {
	// Build the raw corpus bytes
	corpusText := strings.Join(corpusStrs, "\n")
	corpus := []byte(corpusText)

	console.Info(fmt.Sprintf("Corpus: %d bytes, %d functions", len(corpus), len(corpusStrs)))

	// Ingest into ChordStore at all FibWindow scales
	store := buildChordStore(corpus)
	console.Info(fmt.Sprintf("ChordStore: %d entries across %d FibWindow scales",
		len(store.Entries), len(numeric.FibWindows)))

	// Log scale distribution
	scaleCounts := map[int]int{}
	for _, e := range store.Entries {
		scaleCounts[e.Scale]++
	}
	for _, w := range numeric.FibWindows {
		console.Info(fmt.Sprintf("  Scale %2d: %d entries", w, scaleCounts[w]))
	}

	// Verify base chord uniqueness
	uniqueChords := map[data.Chord]int{}
	for b := 0; b < 256; b++ {
		uniqueChords[byteBaseChords[b]]++
	}
	console.Info(fmt.Sprintf("Base chord uniqueness: %d unique out of 256", len(uniqueChords)))

	// Test prompts
	prompts := []string{
		"def factorial(",
		"def find_max(",
		"def binary_search(",
		"def dfs(",
		"def insertion_sort(",
	}

	entries := make([]ChordGenEntry, 0, len(prompts))

	for _, prompt := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ Prompt: %q", prompt))

		entry := generateFromChords(store, corpus, prompt)

		for _, step := range entry.Steps {
			console.Info(fmt.Sprintf("  │  Step %d (score=%.3f, scale=%d, pos=%d, hole=%d, new=%d): +[%s]",
				step.StepNum, step.BestScore, step.BestScale, step.BestPos,
				step.HoleBits, step.EmittedN, step.Emitted))
		}

		console.Info(fmt.Sprintf("  └─ %d tokens, return=%v, colon=%v: %s",
			entry.Tokens, entry.HasReturn, entry.HasColon,
			truncateStr(entry.Prompt+entry.Generated, 100)))

		entries = append(entries, entry)
	}

	// Summary
	totalTokens := 0
	returns := 0
	colons := 0
	for _, e := range entries {
		totalTokens += e.Tokens
		if e.HasReturn {
			returns++
		}
		if e.HasColon {
			colons++
		}
	}

	console.Info("\n  ── Chord Generation Summary ──")
	console.Info(fmt.Sprintf("  Mean tokens:    %.1f", float64(totalTokens)/float64(len(entries))))
	console.Info(fmt.Sprintf("  Has return:     %d/%d", returns, len(entries)))
	console.Info(fmt.Sprintf("  Has colon:      %d/%d", colons, len(entries)))

	return ChordGenResult{
		Entries:    entries,
		StoreSize:  len(store.Entries),
		CorpusSize: len(corpus),
	}
}
