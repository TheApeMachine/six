package lsm

import (
	"bytes"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
promptCarryFrame stores the tail states of a prior prompt so the next prompt can
inherit that resonance instead of always re-seeding from a cold unit phase.
The stored heads represent the prompt boundary before free continuation starts,
which makes them a stable hand-off point for contextual prompting.
*/
type promptCarryFrame struct {
	prompt []byte
	heads  []*WavefrontHead
}

type carrySeedKey struct {
	phase     numeric.Phase
	pos       uint32
	promptIdx int
}

func (wf *Wavefront) seedCarryForward(prompt []byte) []*WavefrontHead {
	if !wf.carryEnabled || len(prompt) == 0 || len(wf.carryFrames) == 0 || wf.carrySeedLimit <= 0 {
		return nil
	}

	var seeds []*WavefrontHead
	seen := make(map[carrySeedKey]bool)

	for i := len(wf.carryFrames) - 1; i >= 0; i-- {
		frame := wf.carryFrames[i]
		overlap := longestSuffixPrefixOverlap(frame.prompt, prompt, wf.carryMinOverlap)
		if overlap < wf.carryMinOverlap {
			continue
		}

		for _, head := range frame.heads {
			seed := wf.carrySeedFromHead(head, overlap, prompt)
			if seed == nil {
				continue
			}

			key := carrySeedKey{phase: seed.phase, pos: seed.pos, promptIdx: seed.promptIdx}
			if seen[key] {
				continue
			}
			seen[key] = true
			seeds = append(seeds, seed)

			if len(seeds) >= wf.carrySeedLimit {
				return wf.prunePrompt(seeds)
			}
		}
	}

	return wf.prunePrompt(seeds)
}

func (wf *Wavefront) carrySeedFromHead(head *WavefrontHead, overlap int, prompt []byte) *WavefrontHead {
	if head == nil || overlap <= 0 || overlap > len(prompt) {
		return nil
	}

	queryPhase := wf.PromptToPhase(prompt[:overlap])
	energy := -overlap * 2

	return &WavefrontHead{
		phase:        head.phase,
		alignedPhase: queryPhase,
		queryPhase:   queryPhase,
		pos:          head.pos,
		promptIdx:    overlap,
		energy:       energy,
		path:         cloneChordTail(head.path, overlap),
		metaPath:     cloneChordTail(head.metaPath, overlap),
		visited:      cloneVisitedMap(head.visited),
		fuzzyErrs:    0,
	}
}

func (wf *Wavefront) mergePromptSeeds(warm, cold []*WavefrontHead) []*WavefrontHead {
	if len(warm) == 0 {
		return wf.prunePrompt(cold)
	}
	if len(cold) == 0 {
		return wf.prunePrompt(warm)
	}

	warm = wf.prunePrompt(warm)
	cold = wf.prunePrompt(cold)

	warmCap := wf.maxHeads / 2
	if warmCap < 1 {
		warmCap = 1
	}
	coldCap := wf.maxHeads - warmCap
	if coldCap < 1 {
		coldCap = 1
	}

	if len(warm) > warmCap {
		warm = warm[:warmCap]
	}
	if len(cold) > coldCap {
		cold = cold[:coldCap]
	}

	return wf.prunePrompt(append(warm, cold...))
}

func (wf *Wavefront) rememberPrompt(prompt []byte, heads []*WavefrontHead) {
	if !wf.carryEnabled || len(prompt) == 0 || len(heads) == 0 {
		return
	}

	snapshot := wf.prunePrompt(heads)
	if len(snapshot) == 0 {
		return
	}

	if wf.carrySeedLimit > 0 && len(snapshot) > wf.carrySeedLimit {
		snapshot = snapshot[:wf.carrySeedLimit]
	}

	wf.carryFrames = append(wf.carryFrames, promptCarryFrame{
		prompt: append([]byte(nil), prompt...),
		heads:  snapshot,
	})

	if wf.carryMaxEntries > 0 && len(wf.carryFrames) > wf.carryMaxEntries {
		wf.carryFrames = append([]promptCarryFrame(nil), wf.carryFrames[len(wf.carryFrames)-wf.carryMaxEntries:]...)
	}

	if len(snapshot) > 0 {
		wf.persistencePhase = snapshot[0].phase
	}
}

func (wf *Wavefront) updatePersistenceFromResults(results []WavefrontResult) {
	if !wf.carryEnabled || len(results) == 0 {
		return
	}

	if results[0].Phase != 0 {
		wf.persistencePhase = results[0].Phase
	}
}

func (wf *Wavefront) persistencePenalty(phase numeric.Phase) int {
	if !wf.carryEnabled || wf.persistencePhase == 0 || wf.carryBiasDivisor <= 0 || phase == 0 {
		return 0
	}

	return int(wf.phaseDistance(phase, wf.persistencePhase)) / wf.carryBiasDivisor
}

func (wf *Wavefront) clonePromptHeads(heads []*WavefrontHead) []*WavefrontHead {
	out := make([]*WavefrontHead, 0, len(heads))
	for _, head := range heads {
		if cloned := cloneWavefrontHead(head); cloned != nil {
			out = append(out, cloned)
		}
	}
	return out
}

func cloneWavefrontHead(head *WavefrontHead) *WavefrontHead {
	if head == nil {
		return nil
	}

	return &WavefrontHead{
		phase:        head.phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          head.pos,
		promptIdx:    head.promptIdx,
		energy:       head.energy,
		path:         cloneChordSlice(head.path),
		metaPath:     cloneChordSlice(head.metaPath),
		visited:      cloneVisitedMap(head.visited),
		fuzzyErrs:    head.fuzzyErrs,
	}
}

func cloneChordSlice(in []data.Chord) []data.Chord {
	if len(in) == 0 {
		return nil
	}
	return append([]data.Chord(nil), in...)
}

func cloneChordTail(in []data.Chord, tail int) []data.Chord {
	if len(in) == 0 {
		return nil
	}
	if tail <= 0 || tail >= len(in) {
		return cloneChordSlice(in)
	}
	return append([]data.Chord(nil), in[len(in)-tail:]...)
}

func cloneVisitedMap(in map[uint64]bool) map[uint64]bool {
	if len(in) == 0 {
		return map[uint64]bool{}
	}

	out := make(map[uint64]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func longestSuffixPrefixOverlap(previous, current []byte, minOverlap int) int {
	limit := len(previous)
	if len(current) < limit {
		limit = len(current)
	}

	for overlap := limit; overlap >= minOverlap; overlap-- {
		if bytes.Equal(previous[len(previous)-overlap:], current[:overlap]) {
			return overlap
		}
	}

	return 0
}

/*
WavefrontWithCarryForward enables prompt-to-prompt resonance hand-off. maxEntries
bounds the number of remembered prompt frames; minOverlap controls the minimum
exact suffix/prefix overlap required before a warm start is injected; biasDivisor
scales how strongly the residual phase nudges branch ranking.
*/
func WavefrontWithCarryForward(maxEntries, minOverlap, biasDivisor int) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.carryEnabled = true
		if maxEntries > 0 {
			wf.carryMaxEntries = maxEntries
		}
		if minOverlap > 0 {
			wf.carryMinOverlap = minOverlap
		}
		if biasDivisor > 0 {
			wf.carryBiasDivisor = biasDivisor
		}
		if wf.carrySeedLimit <= 0 {
			wf.carrySeedLimit = 4
		}
	}
}

/*
WavefrontWithoutCarryForward disables the contextual cache so every prompt begins
from a cold state again.
*/
func WavefrontWithoutCarryForward() wavefrontOpts {
	return func(wf *Wavefront) {
		wf.carryEnabled = false
		wf.persistencePhase = 0
		wf.carryFrames = nil
	}
}
