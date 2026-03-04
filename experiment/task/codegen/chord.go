package codegen

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/tokenizer"
)

// ChordEntry stores one FibWindow chord with origin info.
type ChordEntry struct {
	Chord    data.Chord
	Position int
	Scale    int
}

// ChordStore is a flat array of chords for BestFill.
type ChordStore struct {
	Entries    []ChordEntry
	flatChords []data.Chord
}

func newChordStore() *ChordStore {
	return &ChordStore{}
}

func (cs *ChordStore) insert(chord data.Chord, pos, scale int) {
	cs.Entries = append(cs.Entries, ChordEntry{Chord: chord, Position: pos, Scale: scale})
}

func (cs *ChordStore) buildFlat() {
	cs.flatChords = make([]data.Chord, len(cs.Entries))
	for i, e := range cs.Entries {
		cs.flatChords[i] = e.Chord
	}
}

func (cs *ChordStore) bestFillSoftware(context *data.Chord) (int, float64) {
	bestIdx := 0
	bestScore := 0.0
	for i, entry := range cs.Entries {
		matchCount := data.ChordSimilarity(&entry.Chord, context)
		noiseCount := 0
		for j := 0; j < numeric.ChordBlocks; j++ {
			noiseCount += bits.OnesCount64(entry.Chord[j] &^ context[j])
		}
		score := float64(matchCount) / float64(matchCount+noiseCount+1)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx, bestScore
}

// buildChordStore ingests a corpus into ChordStore at all FibWindow scales.
// Uses tokenizer.BaseChord and data.ChordLCM for window aggregation.
func buildChordStore(corpus []byte) *ChordStore {
	store := newChordStore()
	for _, windowSize := range numeric.FibWindows {
		for pos := 0; pos+windowSize <= len(corpus); pos++ {
			baseChords := make([]data.Chord, windowSize)
			for i := 0; i < windowSize; i++ {
				baseChords[i] = tokenizer.BaseChord(corpus[pos+i])
			}
			windowChord := data.ChordLCM(baseChords)
			store.insert(windowChord, pos, windowSize)
		}
	}
	store.buildFlat()
	return store
}

// generateFromChords runs the BVP span-chain generation loop.
func generateFromChords(store *ChordStore, corpus []byte, prompt string) ChordGenEntry {
	const maxBytes = 4096
	promptBytes := []byte(prompt)
	output := make([]byte, 0, 512)
	output = append(output, promptBytes...)
	steps := make([]ChordGenStep, 0)

	baseChords := make([]data.Chord, len(promptBytes))
	for i, b := range promptBytes {
		baseChords[i] = tokenizer.BaseChord(b)
	}
	promptChord := data.ChordLCM(baseChords)
	bestIdx, bestScore := store.bestFillSoftware(&promptChord)
	entry := store.Entries[bestIdx]
	corpusHead := entry.Position + len(promptBytes)
	if corpusHead >= len(corpus) {
		corpusHead = entry.Position + entry.Scale
	}
	if corpusHead >= len(corpus) {
		return ChordGenEntry{Prompt: prompt, Generated: "(corpus too short)", Tokens: len(promptBytes)}
	}
	steps = append(steps, ChordGenStep{
		StepNum: 1, BestScore: bestScore, BestScale: entry.Scale, BestPos: entry.Position,
		HoleBits: 0, EmittedN: 0, Emitted: fmt.Sprintf("(located at corpus pos %d)", entry.Position),
	})
	step := 1
	for corpusHead < len(corpus) && len(output) < maxBytes {
		step++
		contextWindow := 21
		ctxStart := corpusHead - contextWindow
		if ctxStart < 0 {
			ctxStart = 0
		}
		ctxBaseChords := make([]data.Chord, 0, corpusHead-ctxStart)
		for i := ctxStart; i < corpusHead; i++ {
			ctxBaseChords = append(ctxBaseChords, tokenizer.BaseChord(corpus[i]))
		}
		activeContext := data.ChordLCM(ctxBaseChords)
		bestIdx, bestScore := store.bestFillSoftware(&activeContext)
		entry := store.Entries[bestIdx]
		hole := data.ChordHole(&entry.Chord, &activeContext)
		holeBits := 0
		for j := 0; j < numeric.ChordBlocks; j++ {
			holeBits += bits.OnesCount64(hole[j])
		}
		emitEnd := corpusHead + entry.Scale
		if emitEnd > len(corpus) {
			emitEnd = len(corpus)
		}
		newBytes := corpus[corpusHead:emitEnd]
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
				StepNum: step, BestScore: bestScore, BestScale: entry.Scale, BestPos: entry.Position,
				HoleBits: holeBits, EmittedN: len(newBytes), Emitted: truncateStr(string(newBytes), 40) + " [STOP]",
			})
			break
		}
		output = append(output, newBytes...)
		corpusHead = emitEnd
		steps = append(steps, ChordGenStep{
			StepNum: step, BestScore: bestScore, BestScale: entry.Scale, BestPos: entry.Position,
			HoleBits: holeBits, EmittedN: len(newBytes), Emitted: truncateStr(string(newBytes), 40),
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
