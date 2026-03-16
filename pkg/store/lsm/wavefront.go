package lsm

import (
	"sort"

	"github.com/theapemachine/six/pkg/logic/synthesis/goal"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
WavefrontHead is a single competing search state propagating through
the spatial index. Each head carries a GF(257) phase accumulator and
an energy score (lower is better). At collisions, heads fork.
fuzzyErrs tracks lexical drift tolerated during prompt matching.
*/
type WavefrontHead struct {
	phase        numeric.Phase
	alignedPhase numeric.Phase
	queryPhase   numeric.Phase
	pos          uint32
	segment      uint32
	promptIdx    int
	energy       int
	path         []data.Value
	metaPath     []data.Value
	visited      map[visitMark]bool
	fuzzyErrs    int
	age          uint16
	stalls       uint8
	frustration  uint8
	strictNext   bool
	registers    *executionRegisters
}

/*
Wavefront propagates multiple competing search states through the
spatial index in parallel, implementing the prompt-to-phase
injection and branch-prediction model from the Fermat Braid spec.

The wavefront is seeded with a prompt phase and expands through
Morton-keyed space. At each position, heads score candidates via
XOR + popcount against the prompt value (lower residue = better
resonance). At collision chains, heads fork. Dead ends (phase
mismatch or exhausted branches) are pruned.
*/
type Wavefront struct {
	idx                         *SpatialIndexServer
	calc                        *numeric.Calculus
	maxHeads                    int
	maxDepth                    uint32
	maxFuzzy                    int
	fe                          *goal.FrustrationEngineServer
	target                      numeric.Phase
	anchorStride                uint32
	anchorTolerance             uint32
	carryEnabled                bool
	carryMinOverlap             int
	carryMaxEntries             int
	carrySeedLimit              int
	carryBiasDivisor            int
	frustrationForks            int
	frustrationAttempts         int
	frustrationCheckpointFanout int
	frustrationRounds           int
	headGraceStalls             int
	headFrontierFanout          int
	checkpointTrailLimit        int
	checkpointWindow            int
	persistencePhase            numeric.Phase
	carryFrames                 []promptCarryFrame
}

type wavefrontOpts func(*Wavefront)

/*
NewWavefront creates a wavefront search engine bound to a spatial index.
*/
func NewWavefront(idx *SpatialIndexServer, opts ...wavefrontOpts) *Wavefront {
	wf := &Wavefront{
		idx:                         idx,
		calc:                        numeric.NewCalculus(),
		maxHeads:                    64,
		maxDepth:                    4096,
		maxFuzzy:                    2,   // Allow up to 2 edit operations per branch
		anchorStride:                256, // Periodic master phases for phase-drift correction
		anchorTolerance:             10,
		carryEnabled:                true,
		carryMinOverlap:             3,
		carryMaxEntries:             4,
		carrySeedLimit:              4,
		carryBiasDivisor:            16,
		frustrationForks:            4,
		frustrationAttempts:         512,
		frustrationCheckpointFanout: 2,
		frustrationRounds:           2,
		headGraceStalls:             2,
		headFrontierFanout:          4,
		checkpointTrailLimit:        execRegisterTrailLimit,
		checkpointWindow:            32,
	}

	for _, opt := range opts {
		opt(wf)
	}

	return wf
}

/*
ContextSteering sets up pre-loaded steering vectors from semantic context.
Instead of a single phase bit, the steering value carries both the lexical
signatures and the rolling prompt phases so branch ranking can feel more like
resonance and less like plain substring matching.
*/
func (wf *Wavefront) ContextSteering(contextData, dangerData string) (*data.Value, *data.Value) {
	interest := wf.steeringValue([]byte(contextData))
	danger := wf.steeringValue([]byte(dangerData))
	return &interest, &danger
}

func (wf *Wavefront) steeringValue(payload []byte) data.Value {
	steering := data.MustNewValue()
	if len(payload) == 0 {
		return steering
	}

	state := numeric.Phase(1)
	for _, b := range payload {
		steering = steering.OR(data.BaseValue(b))
		state = wf.calc.Multiply(
			state,
			wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(b)),
		)
		steering.Set(int(state))
	}

	return steering
}

/*
Search propagates the wavefront from a prompt value and returns the
best-matching paths ranked by energy (lowest first).

The algorithm:
1. Seed heads at every position-0 entry that resonates with the prompt
2. For each head, advance to pos+1 and score all candidates
3. At collision chains, fork into multiple heads
4. Prune heads that exceed energy budget or have phase mismatch
5. Return paths sorted by cumulative energy
*/
func (wf *Wavefront) Search(
	prompt data.Value, interest *data.Value, danger *data.Value,
) []WavefrontResult {
	wf.idx.mu.RLock()
	defer wf.idx.mu.RUnlock()

	heads := wf.seed(prompt)
	if len(heads) == 0 {
		return nil
	}

	heads = wf.prune(heads)

	for depth := uint32(0); depth < wf.maxDepth && len(heads) > 0; depth++ {
		heads = wf.advance(heads, prompt, interest, danger)
		heads = wf.prune(heads)
	}

	return wf.collect(heads)
}

/*
SearchPrompt performs the INSIGHT.md style prompt injection + branch prediction.
It now treats prompt alignment as a beam search over edit operations, so the
wavefront can survive substitutions, insertions, deletions, and partial matches
without abandoning the phase-authenticated traversal program.
*/
func (wf *Wavefront) SearchPrompt(
	prompt []byte, interest *data.Value, danger *data.Value,
) []WavefrontResult {
	wf.idx.mu.RLock()
	defer wf.idx.mu.RUnlock()

	if len(prompt) == 0 {
		return nil
	}

	warm := wf.seedCarryForward(prompt)
	cold := wf.seedPromptOffsets(prompt, interest, danger)
	active := wf.mergePromptSeeds(warm, cold)
	if len(active) == 0 {
		return nil
	}

	active = wf.prunePrompt(active)
	bestPartial, bestProgress := wf.promptHeadsAtProgress(active)

	var completed []*WavefrontHead

	maxSteps := int(wf.maxDepth) + len(prompt) + wf.maxFuzzy
	if maxSteps < len(prompt) {
		maxSteps = len(prompt)
	}

	for step := 0; step < maxSteps && len(active) > 0; step++ {
		var next []*WavefrontHead

		for _, head := range active {
			if head.promptIdx >= len(prompt) {
				completed = append(completed, head)
				continue
			}

			next = append(next, wf.expandPromptHead(head, prompt, interest, danger)...)
		}

		if len(completed) > 0 {
			completed = wf.prunePrompt(completed)
		}

		if len(next) == 0 {
			break
		}

		active = wf.prunePrompt(next)

		progressHeads, progress := wf.promptHeadsAtProgress(active)
		if progress > bestProgress {
			bestPartial = progressHeads
			bestProgress = progress
			continue
		}

		if progress == bestProgress {
			bestPartial = wf.prunePrompt(append(bestPartial, progressHeads...))
		}
	}

	heads := completed
	consumed := len(prompt)

	if len(heads) == 0 && bestProgress+wf.maxFuzzy >= len(prompt) {
		heads = bestPartial
		consumed = bestProgress
	}

	if len(heads) == 0 {
		return nil
	}

	carryHeads := wf.clonePromptHeads(heads)
	wf.rememberPrompt(prompt, carryHeads)

	remaining := uint32(0)
	if wf.maxDepth > uint32(consumed) {
		remaining = wf.maxDepth - uint32(consumed)
	}

	for i, head := range heads {
		heads[i] = wf.continueHead(head, remaining, interest, danger)
	}

	heads = wf.prune(heads)

	results := wf.collect(heads)
	wf.updatePersistenceFromResults(results)

	return results
}

/*
PromptToPhase converts raw prompt bytes into a GF(257) rolling state.
This matches the Rotation-IS-Data transition used by the stored program,
so the injection phase speaks the same algebra as traversal itself.
*/
func (wf *Wavefront) PromptToPhase(prompt []byte) numeric.Phase {
	state := numeric.Phase(1)
	for _, b := range prompt {
		state = wf.calc.Multiply(
			state,
			wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(b)),
		)
	}
	return state
}

func (wf *Wavefront) seedPromptOffsets(
	prompt []byte,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	var heads []*WavefrontHead

	maxSkipped := len(prompt) - 1
	if maxSkipped > wf.maxFuzzy {
		maxSkipped = wf.maxFuzzy
	}

	for skipped := 0; skipped <= maxSkipped; skipped++ {
		heads = append(heads, wf.seedPromptByte(prompt[skipped], skipped, interest, danger)...)
	}

	return heads
}

func (wf *Wavefront) seedPromptByte(
	matchByte byte,
	skipped int,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	var heads []*WavefrontHead

	queryPhase := wf.advancePromptPhase(1, matchByte)

	for _, pos := range wf.sortedPositions() {
		for _, key := range wf.sortedKeys(wf.idx.positionIndex[pos]) {
			_, exists := wf.idx.entries[key]
			if !exists {
				continue
			}

			_, symbol := morton.Unpack(key)
			chain := wf.idx.followChainUnsafe(key)
			meta := firstMetaForKeyUnsafe(wf.idx, key)
			for _, stateValue := range chain {
				phase, ok := extractStatePhase(stateValue, symbol)
				if !ok {
					continue
				}

				observable := data.ObservableValue(symbol, stateValue)

				fuzzyErrs := skipped
				if symbol != matchByte {
					fuzzyErrs++
				}
				if fuzzyErrs > wf.maxFuzzy {
					continue
				}

				stepEnergy := skipped * wf.promptEditPenalty(matchByte)
				stepEnergy += lexicalDistance(symbol, matchByte) * 8
				stepEnergy += int(wf.phaseDistance(phase, queryPhase))
				stepEnergy += wf.persistencePenalty(phase)
				if interest != nil {
					stepEnergy -= observable.AND(*interest).ActiveCount()
				}
				if danger != nil {
					stepEnergy += observable.AND(*danger).ActiveCount()
				}

				seed := &WavefrontHead{
					phase:        phase,
					alignedPhase: phase,
					queryPhase:   queryPhase,
					pos:          pos,
					segment:      0,
					promptIdx:    skipped + 1,
					energy:       stepEnergy,
					path:         []data.Value{observable},
					metaPath:     []data.Value{meta},
					visited:      map[visitMark]bool{visitFor(key, 0): true},
					fuzzyErrs:    fuzzyErrs,
					age:          0,
					stalls:       0,
					frustration:  0,
				}
				wf.initializeHeadRegisters(seed, checkpointReasonSeed)
				heads = append(heads, seed)
			}
		}
	}

	return heads
}

func (wf *Wavefront) expandPromptHead(
	head *WavefrontHead,
	prompt []byte,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	if head == nil || head.promptIdx >= len(prompt) {
		return nil
	}

	expectedByte := prompt[head.promptIdx]
	matched := wf.advancePromptMatch(head, expectedByte, interest, danger)
	next := append([]*WavefrontHead(nil), matched...)

	if head.fuzzyErrs >= wf.maxFuzzy {
		return next
	}

	inserted := wf.advancePromptInsertion(head, expectedByte, interest, danger)
	if len(matched) == 0 && len(inserted) == 0 {
		if rewound := wf.backtrackPromptHead(head, expectedByte); rewound != nil {
			next = append(next, rewound)
		}
		for _, candidate := range wf.frustrateHead(head) {
			candidate.energy += wf.promptEditPenalty(expectedByte)
			candidate.fuzzyErrs = head.fuzzyErrs + 1
			candidate.promptIdx = head.promptIdx
			candidate.alignedPhase = head.alignedPhase
			candidate.queryPhase = head.queryPhase
			next = append(next, candidate)
		}
	}

	if skipped := wf.skipPromptByte(head, expectedByte); skipped != nil {
		next = append(next, skipped)
	}

	next = append(next, inserted...)

	return next
}

func (wf *Wavefront) advancePromptMatch(
	head *WavefrontHead,
	expectedByte byte,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	if head == nil {
		return nil
	}

	nextPos, nextSegment, ok := wf.advanceTarget(head)
	if !ok {
		return nil
	}
	nextKeys, hasNext := wf.idx.positionIndex[nextPos]
	if !hasNext || len(nextKeys) == 0 {
		return nil
	}

	var next []*WavefrontHead
	newQueryPhase := wf.advancePromptPhase(head.queryPhase, expectedByte)

	for _, key := range wf.sortedKeys(nextKeys) {
		if head.visited[visitFor(key, nextSegment)] {
			continue
		}

		_, exists := wf.idx.entries[key]
		if !exists {
			continue
		}

		_, nextSymbol := morton.Unpack(key)
		expectedState := wf.predictNextPhase(head, nextSymbol)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateValue := range chain {
			observable := data.ObservableValue(nextSymbol, stateValue)

			resolvedPhase, transitionPenalty, anchored, ok := wf.resolveTransition(head, nextPos, nextSymbol, stateValue, expectedState)
			if !ok {
				continue
			}

			stepEnergy := transitionPenalty
			fuzzyErrs := head.fuzzyErrs
			if nextSymbol != expectedByte {
				fuzzyErrs++
			}
			if fuzzyErrs > wf.maxFuzzy {
				continue
			}

			alignedPhase := resolvedPhase
			stepEnergy += lexicalDistance(nextSymbol, expectedByte) * 8
			stepEnergy += int(wf.phaseDistance(alignedPhase, newQueryPhase))
			stepEnergy += wf.persistencePenalty(resolvedPhase)
			if interest != nil {
				stepEnergy -= observable.AND(*interest).ActiveCount()
			}
			if danger != nil {
				stepEnergy += observable.AND(*danger).ActiveCount()
			}

			fork := wf.forkHead(head, key, nextPos, nextSegment, resolvedPhase, observable, meta, head.energy+stepEnergy, fuzzyErrs)
			fork.alignedPhase = alignedPhase
			fork.queryPhase = newQueryPhase
			fork.promptIdx = head.promptIdx + 1
			fork.energy += wf.applyTransitionRegisters(head, fork, stateValue, newQueryPhase, anchored)
			next = append(next, fork)
		}
	}

	return next
}

func (wf *Wavefront) advancePromptInsertion(
	head *WavefrontHead,
	expectedByte byte,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	if head == nil {
		return nil
	}

	fuzzyErrs := head.fuzzyErrs + 1
	if fuzzyErrs > wf.maxFuzzy {
		return nil
	}

	nextPos, nextSegment, ok := wf.advanceTarget(head)
	if !ok {
		return nil
	}
	nextKeys, hasNext := wf.idx.positionIndex[nextPos]
	if !hasNext || len(nextKeys) == 0 {
		challengers := wf.frustrateHead(head)
		if len(challengers) == 0 {
			return nil
		}

		for _, challenger := range challengers {
			challenger.energy += wf.promptEditPenalty(expectedByte)
			challenger.fuzzyErrs = fuzzyErrs
			challenger.promptIdx = head.promptIdx
			challenger.alignedPhase = head.alignedPhase
			challenger.queryPhase = head.queryPhase
		}

		return challengers
	}

	var next []*WavefrontHead

	for _, key := range wf.sortedKeys(nextKeys) {
		if head.visited[visitFor(key, nextSegment)] {
			continue
		}

		_, exists := wf.idx.entries[key]
		if !exists {
			continue
		}

		_, nextSymbol := morton.Unpack(key)
		expectedState := wf.predictNextPhase(head, nextSymbol)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateValue := range chain {
			observable := data.ObservableValue(nextSymbol, stateValue)

			resolvedPhase, transitionPenalty, anchored, ok := wf.resolveTransition(head, nextPos, nextSymbol, stateValue, expectedState)
			if !ok {
				continue
			}

			stepEnergy := wf.promptEditPenalty(expectedByte) + transitionPenalty
			stepEnergy += lexicalDistance(nextSymbol, expectedByte) * 4
			stepEnergy += int(stateValue.Branches()) * 2
			stepEnergy += wf.persistencePenalty(resolvedPhase)
			if interest != nil {
				stepEnergy -= observable.AND(*interest).ActiveCount()
			}
			if danger != nil {
				stepEnergy += observable.AND(*danger).ActiveCount()
			}

			fork := wf.forkHead(head, key, nextPos, nextSegment, resolvedPhase, observable, meta, head.energy+stepEnergy, fuzzyErrs)
			fork.alignedPhase = head.alignedPhase
			fork.queryPhase = head.queryPhase
			fork.promptIdx = head.promptIdx
			fork.energy += wf.applyTransitionRegisters(head, fork, stateValue, head.queryPhase, anchored)
			next = append(next, fork)
		}
	}

	return next
}

func (wf *Wavefront) skipPromptByte(head *WavefrontHead, expectedByte byte) *WavefrontHead {
	if head == nil || head.fuzzyErrs >= wf.maxFuzzy {
		return nil
	}

	visited := make(map[visitMark]bool, len(head.visited))
	for key, seen := range head.visited {
		visited[key] = seen
	}

	skipped := &WavefrontHead{
		phase:        head.phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          head.pos,
		segment:      head.segment,
		promptIdx:    head.promptIdx + 1,
		energy:       head.energy + wf.promptEditPenalty(expectedByte),
		path:         append([]data.Value(nil), head.path...),
		metaPath:     append([]data.Value(nil), head.metaPath...),
		visited:      visited,
		fuzzyErrs:    head.fuzzyErrs + 1,
		age:          head.age + 1,
		stalls:       0,
		frustration:  head.frustration,
		strictNext:   head.strictNext,
		registers:    cloneExecutionRegisters(head.registers),
	}
	if skipped.registers == nil {
		skipped.registers = wf.newHeadRegisters()
	}
	skipped.registers.RecordCheckpoint(skipped, checkpointReasonSkip)
	return skipped
}

func (wf *Wavefront) promptHeadsAtProgress(heads []*WavefrontHead) ([]*WavefrontHead, int) {
	bestProgress := -1
	var best []*WavefrontHead

	for _, head := range heads {
		if head == nil {
			continue
		}

		if head.promptIdx > bestProgress {
			bestProgress = head.promptIdx
			best = best[:0]
			best = append(best, head)
			continue
		}

		if head.promptIdx == bestProgress {
			best = append(best, head)
		}
	}

	if len(best) == 0 {
		return nil, 0
	}

	return wf.prunePrompt(best), bestProgress
}

func (wf *Wavefront) advancePromptPhase(phase numeric.Phase, symbol byte) numeric.Phase {
	return wf.calc.Multiply(
		phase,
		wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(symbol)),
	)
}

func (wf *Wavefront) promptEditPenalty(symbol byte) int {
	return data.BaseValue(symbol).ActiveCount() * 2
}

/*
seed creates initial wavefront heads at local depth 0 by scanning all
entries whose key-derived lexical identity resonates with the prompt value.
The path keeps projected observables, while the index itself stores native values.
*/
func (wf *Wavefront) seed(prompt data.Value) []*WavefrontHead {
	var heads []*WavefrontHead

	keys, hasPos := wf.idx.positionIndex[0]
	if !hasPos {
		return heads
	}

	for _, key := range wf.sortedKeys(keys) {
		_, exists := wf.idx.entries[key]
		if !exists {
			continue
		}

		_, symbol := morton.Unpack(key)
		symbolValue := data.BaseValue(symbol)
		sim := data.ValueSimilarity(&symbolValue, &prompt)
		if sim == 0 {
			continue
		}

		startPhase := wf.advancePromptPhase(1, symbol)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateValue := range chain {
			phase, ok := extractStatePhase(stateValue, symbol)
			if !ok {
				phase = startPhase
			}
			observable := data.ObservableValue(symbol, stateValue)
			seed := &WavefrontHead{
				phase:        phase,
				alignedPhase: phase,
				queryPhase:   phase,
				pos:          0,
				segment:      0,
				energy:       symbolValue.XOR(prompt).ActiveCount(),
				path:         []data.Value{observable},
				metaPath:     []data.Value{meta},
				visited:      map[visitMark]bool{visitFor(key, 0): true},
				fuzzyErrs:    0,
				age:          0,
				stalls:       0,
			}
			wf.initializeHeadRegisters(seed, checkpointReasonSeed)
			heads = append(heads, seed)
		}
	}

	return heads
}

/*
advance moves every head forward by one position, scoring candidates
and forking at collision chains. Returns the surviving heads.
*/
func (wf *Wavefront) advance(
	heads []*WavefrontHead,
	prompt data.Value,
	interest *data.Value,
	danger *data.Value,
) []*WavefrontHead {
	var next []*WavefrontHead

	for _, head := range heads {
		nextPos, nextSegment, ok := wf.advanceTarget(head)
		if !ok {
			next = append(next, head)
			continue
		}
		nextKeys, hasNext := wf.idx.positionIndex[nextPos]
		didAdvance := false

		if hasNext && len(nextKeys) > 0 {
			for _, key := range wf.sortedKeys(nextKeys) {
				if head.visited[visitFor(key, nextSegment)] {
					continue
				}

				_, exists := wf.idx.entries[key]
				if !exists {
					continue
				}

				_, nextSymbol := morton.Unpack(key)
				expectedPhase := wf.predictNextPhase(head, nextSymbol)

				meta := firstMetaForKeyUnsafe(wf.idx, key)
				chain := wf.idx.followChainUnsafe(key)
				for _, stateValue := range chain {
					observable := data.ObservableValue(nextSymbol, stateValue)
					fuzzyErrs := head.fuzzyErrs

					resolvedPhase, transitionPenalty, anchored, ok := wf.resolveTransition(head, nextPos, nextSymbol, stateValue, expectedPhase)
					if !ok {
						if head.strictNext {
							continue
						}
						storedPhase, phaseOK := extractStatePhase(stateValue, nextSymbol)
						if !phaseOK || wf.anchorViolates(nextPos, expectedPhase, stateValue) {
							continue
						}

						fuzzyErrs++
						if fuzzyErrs > wf.maxFuzzy {
							continue
						}

						transitionPenalty = 100
						if len(head.path) > 0 {
							transitionPenalty += operatorRoutePenalty(head.path[len(head.path)-1], nextSymbol)
						}
						resolvedPhase = storedPhase
					}

					stepEnergy := transitionPenalty
					symbolValue := data.BaseValue(nextSymbol)
					residue := prompt.XOR(symbolValue)
					stepEnergy += residue.ActiveCount()
					stepEnergy += wf.persistencePenalty(resolvedPhase)

					if interest != nil {
						stepEnergy -= observable.AND(*interest).ActiveCount()
					}
					if danger != nil {
						stepEnergy += observable.AND(*danger).ActiveCount()
					}

					fork := wf.forkHead(head, key, nextPos, nextSegment, resolvedPhase, observable, meta, head.energy+stepEnergy, fuzzyErrs)
					fork.energy += wf.applyTransitionRegisters(head, fork, stateValue, expectedPhase, anchored)
					next = append(next, fork)
					didAdvance = true
				}
			}
		}

		if didAdvance {
			continue
		}

		challengers := wf.frustrateHead(head)
		if len(challengers) > 0 {
			next = append(next, challengers...)
			if stalled := wf.stallHead(head); stalled != nil {
				next = append(next, stalled)
			}
			continue
		}

		if stalled := wf.stallHead(head); stalled != nil {
			next = append(next, stalled)
			continue
		}

		next = append(next, head)
	}

	return next
}

func (wf *Wavefront) continueHead(
	head *WavefrontHead,
	budget uint32,
	interest *data.Value,
	danger *data.Value,
) *WavefrontHead {
	if head == nil || budget == 0 {
		return head
	}

	for stepCount := uint32(0); stepCount < budget; stepCount++ {
		nextPos, nextSegment, ok := wf.advanceTarget(head)
		if !ok {
			break
		}
		nextKeys, hasNext := wf.idx.positionIndex[nextPos]
		if !hasNext || len(nextKeys) == 0 {
			challengers := wf.frustrateHead(head)
			if len(challengers) == 0 {
				if stalled := wf.stallHead(head); stalled != nil {
					head = stalled
					continue
				}
				break
			}
			if stalled := wf.stallHead(head); stalled != nil {
				challengers = append(challengers, stalled)
			}
			challengers = wf.prune(challengers)
			head = challengers[0]
			continue
		}

		var best *WavefrontHead
		bestKey := uint64(0)
		for _, key := range wf.sortedKeys(nextKeys) {
			if head.visited[visitFor(key, nextSegment)] {
				continue
			}

			_, exists := wf.idx.entries[key]
			if !exists {
				continue
			}

			_, symbol := morton.Unpack(key)
			expectedState := wf.predictNextPhase(head, symbol)

			meta := firstMetaForKeyUnsafe(wf.idx, key)
			chain := wf.idx.followChainUnsafe(key)
			for _, stateValue := range chain {
				observable := data.ObservableValue(symbol, stateValue)

				resolvedPhase, transitionPenalty, anchored, ok := wf.resolveTransition(head, nextPos, symbol, stateValue, expectedState)
				if !ok {
					continue
				}

				stepEnergy := int(stateValue.Branches())*2 + transitionPenalty
				stepEnergy += wf.persistencePenalty(resolvedPhase)

				if interest != nil {
					stepEnergy -= observable.AND(*interest).ActiveCount()
				}
				if danger != nil {
					stepEnergy += observable.AND(*danger).ActiveCount()
				}

				candidate := wf.forkHead(head, key, nextPos, nextSegment, resolvedPhase, observable, meta, head.energy+stepEnergy, head.fuzzyErrs)
				candidate.energy += wf.applyTransitionRegisters(head, candidate, stateValue, expectedState, anchored)
				if best == nil || candidate.energy < best.energy || (candidate.energy == best.energy && key < bestKey) {
					best = candidate
					bestKey = key
				}
			}
		}

		if best == nil {
			challengers := wf.frustrateHead(head)
			if len(challengers) == 0 {
				if stalled := wf.stallHead(head); stalled != nil {
					head = stalled
					continue
				}
				break
			}
			if stalled := wf.stallHead(head); stalled != nil {
				challengers = append(challengers, stalled)
			}
			challengers = wf.prune(challengers)
			head = challengers[0]
			continue
		}

		head = best
	}

	return head
}

func (wf *Wavefront) bridgeHead(head *WavefrontHead) *WavefrontHead {
	challengers := wf.frustrateHead(head)
	if len(challengers) == 0 {
		return nil
	}
	return challengers[0]
}

func (wf *Wavefront) forkHead(
	head *WavefrontHead,
	key uint64,
	pos uint32,
	segment uint32,
	phase numeric.Phase,
	value data.Value,
	meta data.Value,
	energy int,
	fuzzyErrs int,
) *WavefrontHead {
	visited := make(map[visitMark]bool, len(head.visited)+1)
	for k, v := range head.visited {
		visited[k] = v
	}
	visited[visitFor(key, segment)] = true

	return &WavefrontHead{
		phase:        phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          pos,
		segment:      segment,
		promptIdx:    head.promptIdx,
		energy:       energy,
		path:         append(append([]data.Value{}, head.path...), value),
		metaPath:     append(append([]data.Value{}, head.metaPath...), meta),
		visited:      visited,
		fuzzyErrs:    fuzzyErrs,
		age:          head.age + 1,
		stalls:       0,
		frustration:  0,
		strictNext:   false,
		registers:    cloneExecutionRegisters(head.registers),
	}
}

func (wf *Wavefront) sortedPositions() []uint32 {
	positions := make([]uint32, 0, len(wf.idx.positionIndex))
	for pos := range wf.idx.positionIndex {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})
	return positions
}

func (wf *Wavefront) sortedKeys(keys []uint64) []uint64 {
	out := append([]uint64(nil), keys...)
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

/*
prune keeps only the top maxHeads by energy (lowest first).
*/
func (wf *Wavefront) prune(heads []*WavefrontHead) []*WavefrontHead {
	heads = wf.compactHeads(heads, false)
	sort.Slice(heads, func(i, j int) bool {
		return wf.betterHead(heads[i], heads[j], false)
	})

	if len(heads) > wf.maxHeads {
		return heads[:wf.maxHeads]
	}

	return heads
}

func (wf *Wavefront) prunePrompt(heads []*WavefrontHead) []*WavefrontHead {
	compacted := wf.compactHeads(heads, true)
	sort.Slice(compacted, func(i, j int) bool {
		return wf.betterHead(compacted[i], compacted[j], true)
	})

	if len(compacted) > wf.maxHeads {
		return compacted[:wf.maxHeads]
	}

	return compacted
}

/*
WavefrontResult is a ranked path from a wavefront search.
*/
type WavefrontResult struct {
	Path     []data.Value
	MetaPath []data.Value
	Energy   int
	Phase    numeric.Phase
	Depth    uint32
}

/*
phaseDistance returns the shortest modular distance between two GF(257) phases.
Used by the anchor PLL to decide whether drift can be corrected or must be killed.
*/
func (wf *Wavefront) phaseDistance(left, right numeric.Phase) uint32 {
	return phaseDistanceMod257(left, right)
}

/*
anchorCorrect implements the phase-locked loop described in INSIGHT.md.
At configured anchor positions the stored master phase in ResidualCarry can
snap a nearby drifting state back onto the canonical trajectory.
*/
func (wf *Wavefront) anchorCorrect(pos uint32, expected numeric.Phase, value data.Value) (numeric.Phase, int, bool) {
	if wf.anchorStride == 0 || pos == 0 || pos%wf.anchorStride != 0 {
		return 0, 0, false
	}

	anchor := numeric.Phase(value.ResidualCarry() % uint64(numeric.FermatPrime))
	if anchor == 0 {
		return 0, 0, false
	}

	drift := wf.phaseDistance(expected, anchor)
	if drift == 0 || drift > wf.anchorTolerance {
		return 0, 0, false
	}

	return anchor, int(drift), true
}

/*
anchorViolates reports whether an anchor exists at this position and the drift
is too large to trust the branch any longer.
*/
func (wf *Wavefront) anchorViolates(pos uint32, expected numeric.Phase, value data.Value) bool {
	if wf.anchorStride == 0 || pos == 0 || pos%wf.anchorStride != 0 {
		return false
	}

	anchor := numeric.Phase(value.ResidualCarry() % uint64(numeric.FermatPrime))
	if anchor == 0 {
		return false
	}

	return wf.phaseDistance(expected, anchor) > wf.anchorTolerance
}

func (wf *Wavefront) collect(heads []*WavefrontHead) []WavefrontResult {
	results := make([]WavefrontResult, 0, len(heads))

	for _, head := range heads {
		if len(head.path) == 0 {
			continue
		}

		results = append(results, WavefrontResult{
			Path:     head.path,
			MetaPath: head.metaPath,
			Energy:   head.energy,
			Phase:    head.phase,
			Depth:    head.pos,
		})
	}

	for i := 0; i < len(results)-1; i++ {
		minIdx := i
		for j := i + 1; j < len(results); j++ {
			if results[j].Energy < results[minIdx].Energy ||
				(results[j].Energy == results[minIdx].Energy && results[j].Depth < results[minIdx].Depth) {
				minIdx = j
			}
		}
		results[i], results[minIdx] = results[minIdx], results[i]
	}

	return results
}

/*
WavefrontWithMaxHeads limits the number of concurrent search states.
*/
func WavefrontWithMaxHeads(maxHeads int) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.maxHeads = maxHeads
	}
}

/*
WavefrontWithMaxDepth limits the maximum traversal depth.
*/
func WavefrontWithMaxDepth(maxDepth uint32) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.maxDepth = maxDepth
	}
}

/*
WavefrontWithMaxFuzzy sets the edit budget used during prompt alignment.
Zero turns SearchPrompt into an exact, edit-free matcher while still using the
same reset/jump/operator-aware traversal engine.
*/
func WavefrontWithMaxFuzzy(maxFuzzy int) wavefrontOpts {
	return func(wf *Wavefront) {
		if maxFuzzy >= 0 {
			wf.maxFuzzy = maxFuzzy
		}
	}
}

/*
WavefrontWithFrustrationEngine attaches the Phase 4 logic solver to the search.
*/
func WavefrontWithFrustrationEngine(fe *goal.FrustrationEngineServer, target numeric.Phase) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.fe = fe
		wf.target = target
	}
}

/*
WavefrontWithFrustrationForks tunes controlled branch forking when frustration spikes.
maxForks bounds the number of challenger heads emitted per stall, checkpointFanout
limits how many rewind points are tested, attempts controls macro-path exploration,
and rounds limits how many repeated stalls an incumbent may survive before being pruned.
*/
func WavefrontWithFrustrationForks(maxForks, checkpointFanout, attempts, rounds int) wavefrontOpts {
	return func(wf *Wavefront) {
		if maxForks > 0 {
			wf.frustrationForks = maxForks
		}
		if checkpointFanout >= 0 {
			wf.frustrationCheckpointFanout = checkpointFanout
		}
		if attempts > 0 {
			wf.frustrationAttempts = attempts
		}
		if rounds >= 0 {
			wf.frustrationRounds = rounds
		}
	}
}

/*
WavefrontWithBranchHygiene tunes challenger pruning and checkpoint garbage collection.
graceStalls bounds how many stalled rounds a branch may accumulate before it is treated
as expired during pruning, frontierFanout caps the number of survivors per frontier,
checkpointTrailLimit caps transient checkpoint snapshots, and checkpointWindow controls
how far behind the current head low-priority checkpoints may survive.
*/
func WavefrontWithBranchHygiene(graceStalls, frontierFanout, checkpointTrailLimit, checkpointWindow int) wavefrontOpts {
	return func(wf *Wavefront) {
		if graceStalls >= 0 {
			wf.headGraceStalls = graceStalls
		}
		if frontierFanout > 0 {
			wf.headFrontierFanout = frontierFanout
		}
		if checkpointTrailLimit > 0 {
			wf.checkpointTrailLimit = checkpointTrailLimit
		}
		if checkpointWindow >= 0 {
			wf.checkpointWindow = checkpointWindow
		}
	}
}

/*
WavefrontWithAnchors configures periodic phase-drift correction. A stride of 0 disables
anchor handling entirely. tolerance is the maximum modular phase distance that can be snapped.
*/
func WavefrontWithAnchors(stride, tolerance uint32) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.anchorStride = stride
		wf.anchorTolerance = tolerance
	}
}
