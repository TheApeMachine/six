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
	promptIdx    int
	energy       int
	path         []data.Chord
	metaPath     []data.Chord
	visited      map[uint64]bool
	fuzzyErrs    int
}

/*
Wavefront propagates multiple competing search states through the
spatial index in parallel, implementing the prompt-to-phase
injection and branch-prediction model from the Fermat Braid spec.

The wavefront is seeded with a prompt phase and expands through
Morton-keyed space. At each position, heads score candidates via
XOR + popcount against the prompt chord (lower residue = better
resonance). At collision chains, heads fork. Dead ends (phase
mismatch or exhausted branches) are pruned.
*/
type Wavefront struct {
	idx              *SpatialIndexServer
	calc             *numeric.Calculus
	maxHeads         int
	maxDepth         uint32
	maxFuzzy         int
	fe               *goal.FrustrationEngineServer
	target           numeric.Phase
	anchorStride     uint32
	anchorTolerance  uint32
	carryEnabled     bool
	carryMinOverlap  int
	carryMaxEntries  int
	carrySeedLimit   int
	carryBiasDivisor int
	persistencePhase numeric.Phase
	carryFrames      []promptCarryFrame
}

type wavefrontOpts func(*Wavefront)

/*
NewWavefront creates a wavefront search engine bound to a spatial index.
*/
func NewWavefront(idx *SpatialIndexServer, opts ...wavefrontOpts) *Wavefront {
	wf := &Wavefront{
		idx:              idx,
		calc:             numeric.NewCalculus(),
		maxHeads:         64,
		maxDepth:         4096,
		maxFuzzy:         2,   // Allow up to 2 edit operations per branch
		anchorStride:     256, // Periodic master phases for phase-drift correction
		anchorTolerance:  10,
		carryEnabled:     true,
		carryMinOverlap:  3,
		carryMaxEntries:  4,
		carrySeedLimit:   4,
		carryBiasDivisor: 16,
	}

	for _, opt := range opts {
		opt(wf)
	}

	return wf
}

/*
ContextSteering sets up pre-loaded steering vectors from semantic context.
Instead of a single phase bit, the steering chord carries both the lexical
signatures and the rolling prompt phases so branch ranking can feel more like
resonance and less like plain substring matching.
*/
func (wf *Wavefront) ContextSteering(contextData, dangerData string) (*data.Chord, *data.Chord) {
	interest := wf.steeringChord([]byte(contextData))
	danger := wf.steeringChord([]byte(dangerData))
	return &interest, &danger
}

func (wf *Wavefront) steeringChord(payload []byte) data.Chord {
	steering := data.MustNewChord()
	if len(payload) == 0 {
		return steering
	}

	state := numeric.Phase(1)
	for _, b := range payload {
		steering = steering.OR(data.BaseChord(b))
		state = wf.calc.Multiply(
			state,
			wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(b)),
		)
		steering.Set(int(state))
	}

	return steering
}

/*
Search propagates the wavefront from a prompt chord and returns the
best-matching paths ranked by energy (lowest first).

The algorithm:
1. Seed heads at every position-0 entry that resonates with the prompt
2. For each head, advance to pos+1 and score all candidates
3. At collision chains, fork into multiple heads
4. Prune heads that exceed energy budget or have phase mismatch
5. Return paths sorted by cumulative energy
*/
func (wf *Wavefront) Search(
	prompt data.Chord, interest *data.Chord, danger *data.Chord,
) []WavefrontResult {
	wf.idx.mu.RLock()
	defer wf.idx.mu.RUnlock()

	heads := wf.seed(prompt)
	if len(heads) == 0 {
		return nil
	}

	if len(heads) > wf.maxHeads {
		heads = wf.prune(heads)
	}

	for depth := uint32(0); depth < wf.maxDepth && len(heads) > 0; depth++ {
		heads = wf.advance(heads, prompt, interest, danger)

		if len(heads) > wf.maxHeads {
			heads = wf.prune(heads)
		}
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
	prompt []byte, interest *data.Chord, danger *data.Chord,
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

	if len(heads) > wf.maxHeads {
		heads = wf.prune(heads)
	}

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
	interest *data.Chord,
	danger *data.Chord,
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
	interest *data.Chord,
	danger *data.Chord,
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
			alignedPhase := wf.advancePromptPhase(1, symbol)

			for _, stateChord := range chain {
				phase, ok := extractStatePhase(stateChord, symbol)
				if !ok {
					continue
				}

				fuzzyErrs := skipped
				if symbol != matchByte {
					fuzzyErrs++
				}
				if fuzzyErrs > wf.maxFuzzy {
					continue
				}

				stepEnergy := skipped * wf.promptEditPenalty(matchByte)
				stepEnergy += lexicalDistance(symbol, matchByte) * 8
				stepEnergy += int(wf.phaseDistance(alignedPhase, queryPhase))
				stepEnergy += wf.persistencePenalty(phase)
				if interest != nil {
					stepEnergy -= stateChord.AND(*interest).ActiveCount()
				}
				if danger != nil {
					stepEnergy += stateChord.AND(*danger).ActiveCount()
				}

				heads = append(heads, &WavefrontHead{
					phase:        phase,
					alignedPhase: alignedPhase,
					queryPhase:   queryPhase,
					pos:          pos,
					promptIdx:    skipped + 1,
					energy:       stepEnergy,
					path:         []data.Chord{stateChord},
					metaPath:     []data.Chord{meta},
					visited:      map[uint64]bool{key: true},
					fuzzyErrs:    fuzzyErrs,
				})
			}
		}
	}

	return heads
}

func (wf *Wavefront) expandPromptHead(
	head *WavefrontHead,
	prompt []byte,
	interest *data.Chord,
	danger *data.Chord,
) []*WavefrontHead {
	if head == nil || head.promptIdx >= len(prompt) {
		return nil
	}

	expectedByte := prompt[head.promptIdx]
	next := wf.advancePromptMatch(head, expectedByte, interest, danger)

	if head.fuzzyErrs >= wf.maxFuzzy {
		return next
	}

	if skipped := wf.skipPromptByte(head, expectedByte); skipped != nil {
		next = append(next, skipped)
	}

	next = append(next, wf.advancePromptInsertion(head, expectedByte, interest, danger)...)

	return next
}

func (wf *Wavefront) advancePromptMatch(
	head *WavefrontHead,
	expectedByte byte,
	interest *data.Chord,
	danger *data.Chord,
) []*WavefrontHead {
	if head == nil {
		return nil
	}

	step := wf.advanceDistance(head)
	if step == 0 {
		return nil
	}

	nextPos := head.pos + step
	nextKeys, hasNext := wf.idx.positionIndex[nextPos]
	if !hasNext || len(nextKeys) == 0 {
		return nil
	}

	var next []*WavefrontHead
	newQueryPhase := wf.advancePromptPhase(head.queryPhase, expectedByte)

	for _, key := range wf.sortedKeys(nextKeys) {
		if head.visited[key] {
			continue
		}

		_, exists := wf.idx.entries[key]
		if !exists {
			continue
		}

		_, nextSymbol := morton.Unpack(key)
		expectedState := wf.advancePromptPhase(head.phase, nextSymbol)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateChord := range chain {
			storedPhase, ok := extractStatePhase(stateChord, nextSymbol)
			if !ok {
				continue
			}

			resolvedPhase := expectedState
			stepEnergy := 0
			if snapped, penalty, ok := wf.anchorCorrect(nextPos, expectedState, stateChord); ok {
				resolvedPhase = snapped
				stepEnergy += penalty
			} else if wf.anchorViolates(nextPos, expectedState, stateChord) {
				continue
			}

			if storedPhase != resolvedPhase {
				continue
			}

			fuzzyErrs := head.fuzzyErrs
			if nextSymbol != expectedByte {
				fuzzyErrs++
			}
			if fuzzyErrs > wf.maxFuzzy {
				continue
			}

			alignedPhase := wf.advancePromptPhase(head.alignedPhase, nextSymbol)
			stepEnergy += lexicalDistance(nextSymbol, expectedByte) * 8
			stepEnergy += int(wf.phaseDistance(alignedPhase, newQueryPhase))
			stepEnergy += wf.persistencePenalty(resolvedPhase)
			if interest != nil {
				stepEnergy -= stateChord.AND(*interest).ActiveCount()
			}
			if danger != nil {
				stepEnergy += stateChord.AND(*danger).ActiveCount()
			}

			fork := wf.forkHead(head, key, nextPos, resolvedPhase, stateChord, meta, head.energy+stepEnergy, fuzzyErrs)
			fork.alignedPhase = alignedPhase
			fork.queryPhase = newQueryPhase
			fork.promptIdx = head.promptIdx + 1
			next = append(next, fork)
		}
	}

	return next
}

func (wf *Wavefront) advancePromptInsertion(
	head *WavefrontHead,
	expectedByte byte,
	interest *data.Chord,
	danger *data.Chord,
) []*WavefrontHead {
	if head == nil {
		return nil
	}

	fuzzyErrs := head.fuzzyErrs + 1
	if fuzzyErrs > wf.maxFuzzy {
		return nil
	}

	step := wf.advanceDistance(head)
	if step == 0 {
		return nil
	}

	nextPos := head.pos + step
	nextKeys, hasNext := wf.idx.positionIndex[nextPos]
	if !hasNext || len(nextKeys) == 0 {
		bridged := wf.bridgeHead(head)
		if bridged == nil {
			return nil
		}

		bridged.energy += wf.promptEditPenalty(expectedByte)
		bridged.fuzzyErrs = fuzzyErrs
		bridged.promptIdx = head.promptIdx
		bridged.alignedPhase = head.alignedPhase
		bridged.queryPhase = head.queryPhase

		return []*WavefrontHead{bridged}
	}

	var next []*WavefrontHead

	for _, key := range wf.sortedKeys(nextKeys) {
		if head.visited[key] {
			continue
		}

		_, exists := wf.idx.entries[key]
		if !exists {
			continue
		}

		_, nextSymbol := morton.Unpack(key)
		expectedState := wf.advancePromptPhase(head.phase, nextSymbol)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateChord := range chain {
			storedPhase, ok := extractStatePhase(stateChord, nextSymbol)
			if !ok {
				continue
			}

			resolvedPhase := expectedState
			stepEnergy := wf.promptEditPenalty(expectedByte)
			if snapped, penalty, ok := wf.anchorCorrect(nextPos, expectedState, stateChord); ok {
				resolvedPhase = snapped
				stepEnergy += penalty
			} else if wf.anchorViolates(nextPos, expectedState, stateChord) {
				continue
			}

			if storedPhase != resolvedPhase {
				continue
			}

			stepEnergy += lexicalDistance(nextSymbol, expectedByte) * 4
			stepEnergy += int(stateChord.Branches()) * 2
			stepEnergy += wf.persistencePenalty(resolvedPhase)
			if interest != nil {
				stepEnergy -= stateChord.AND(*interest).ActiveCount()
			}
			if danger != nil {
				stepEnergy += stateChord.AND(*danger).ActiveCount()
			}

			fork := wf.forkHead(head, key, nextPos, resolvedPhase, stateChord, meta, head.energy+stepEnergy, fuzzyErrs)
			fork.alignedPhase = head.alignedPhase
			fork.queryPhase = head.queryPhase
			fork.promptIdx = head.promptIdx
			next = append(next, fork)
		}
	}

	return next
}

func (wf *Wavefront) skipPromptByte(head *WavefrontHead, expectedByte byte) *WavefrontHead {
	if head == nil || head.fuzzyErrs >= wf.maxFuzzy {
		return nil
	}

	visited := make(map[uint64]bool, len(head.visited))
	for key, seen := range head.visited {
		visited[key] = seen
	}

	return &WavefrontHead{
		phase:        head.phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          head.pos,
		promptIdx:    head.promptIdx + 1,
		energy:       head.energy + wf.promptEditPenalty(expectedByte),
		path:         append([]data.Chord(nil), head.path...),
		metaPath:     append([]data.Chord(nil), head.metaPath...),
		visited:      visited,
		fuzzyErrs:    head.fuzzyErrs + 1,
	}
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
	return data.BaseChord(symbol).ActiveCount() * 2
}

/*
seed creates initial wavefront heads at position 0 by scanning all
entries whose byte identity (BaseChord of the Morton symbol) resonates
with the prompt chord. The stored state chord is kept for path tracking.
*/
func (wf *Wavefront) seed(prompt data.Chord) []*WavefrontHead {
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
		symbolChord := data.BaseChord(symbol)
		sim := data.ChordSimilarity(&symbolChord, &prompt)
		if sim == 0 {
			continue
		}

		startPhase := wf.calc.Multiply(
			numeric.Phase(1),
			wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(symbol)),
		)

		meta := firstMetaForKeyUnsafe(wf.idx, key)
		chain := wf.idx.followChainUnsafe(key)
		for _, stateChord := range chain {
			heads = append(heads, &WavefrontHead{
				phase:     startPhase,
				pos:       0,
				energy:    symbolChord.XOR(prompt).ActiveCount(),
				path:      []data.Chord{stateChord},
				metaPath:  []data.Chord{meta},
				visited:   map[uint64]bool{key: true},
				fuzzyErrs: 0,
			})
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
	prompt data.Chord,
	interest *data.Chord,
	danger *data.Chord,
) []*WavefrontHead {
	var next []*WavefrontHead

	for _, head := range heads {
		step := wf.advanceDistance(head)
		if step == 0 {
			next = append(next, head)
			continue
		}

		nextPos := head.pos + step
		nextKeys, hasNext := wf.idx.positionIndex[nextPos]
		didAdvance := false

		if hasNext && len(nextKeys) > 0 {
			for _, key := range wf.sortedKeys(nextKeys) {
				if head.visited[key] {
					continue
				}

				_, exists := wf.idx.entries[key]
				if !exists {
					continue
				}

				_, nextSymbol := morton.Unpack(key)
				expectedPhase := wf.calc.Multiply(
					head.phase,
					wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(nextSymbol)),
				)

				meta := firstMetaForKeyUnsafe(wf.idx, key)
				chain := wf.idx.followChainUnsafe(key)
				for _, stateChord := range chain {
					fuzzyErrs := head.fuzzyErrs
					stepEnergy := 0
					resolvedPhase := expectedPhase

					storedPhase, ok := extractStatePhase(stateChord, nextSymbol)
					if !ok {
						continue
					}

					phaseMatches := storedPhase == expectedPhase
					if snapped, penalty, ok := wf.anchorCorrect(nextPos, expectedPhase, stateChord); ok {
						resolvedPhase = snapped
						phaseMatches = storedPhase == snapped
						stepEnergy += penalty
					} else if wf.anchorViolates(nextPos, expectedPhase, stateChord) {
						continue
					}

					if !phaseMatches {
						fuzzyErrs++
						if fuzzyErrs > wf.maxFuzzy {
							continue
						}
						stepEnergy += 100
					}

					symbolChord := data.BaseChord(nextSymbol)
					residue := prompt.XOR(symbolChord)
					stepEnergy += residue.ActiveCount()
					stepEnergy += wf.persistencePenalty(resolvedPhase)

					if interest != nil {
						stepEnergy -= stateChord.AND(*interest).ActiveCount()
					}
					if danger != nil {
						stepEnergy += stateChord.AND(*danger).ActiveCount()
					}

					fork := wf.forkHead(head, key, nextPos, resolvedPhase, stateChord, meta, head.energy+stepEnergy, fuzzyErrs)
					next = append(next, fork)
					didAdvance = true
				}
			}
		}

		if didAdvance {
			continue
		}

		if bridged := wf.bridgeHead(head); bridged != nil {
			next = append(next, bridged)
			continue
		}

		next = append(next, head)
	}

	return next
}

func (wf *Wavefront) continueHead(
	head *WavefrontHead,
	budget uint32,
	interest *data.Chord,
	danger *data.Chord,
) *WavefrontHead {
	if head == nil || budget == 0 {
		return head
	}

	for stepCount := uint32(0); stepCount < budget; stepCount++ {
		step := wf.advanceDistance(head)
		if step == 0 {
			break
		}

		nextPos := head.pos + step
		nextKeys, hasNext := wf.idx.positionIndex[nextPos]
		if !hasNext || len(nextKeys) == 0 {
			break
		}

		var best *WavefrontHead
		bestKey := uint64(0)
		for _, key := range wf.sortedKeys(nextKeys) {
			if head.visited[key] {
				continue
			}

			_, exists := wf.idx.entries[key]
			if !exists {
				continue
			}

			_, symbol := morton.Unpack(key)
			expectedState := wf.calc.Multiply(
				head.phase,
				wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(symbol)),
			)

			meta := firstMetaForKeyUnsafe(wf.idx, key)
			chain := wf.idx.followChainUnsafe(key)
			for _, stateChord := range chain {
				storedPhase, ok := extractStatePhase(stateChord, symbol)
				if !ok {
					continue
				}

				resolvedPhase := expectedState
				stepEnergy := int(stateChord.Branches()) * 2
				if snapped, penalty, ok := wf.anchorCorrect(nextPos, expectedState, stateChord); ok {
					resolvedPhase = snapped
					stepEnergy += penalty
				} else if wf.anchorViolates(nextPos, expectedState, stateChord) {
					continue
				}

				if storedPhase != resolvedPhase {
					continue
				}

				stepEnergy += wf.persistencePenalty(resolvedPhase)

				if interest != nil {
					stepEnergy -= stateChord.AND(*interest).ActiveCount()
				}
				if danger != nil {
					stepEnergy += stateChord.AND(*danger).ActiveCount()
				}

				candidate := wf.forkHead(head, key, nextPos, resolvedPhase, stateChord, meta, head.energy+stepEnergy, head.fuzzyErrs)
				if best == nil || candidate.energy < best.energy || (candidate.energy == best.energy && key < bestKey) {
					best = candidate
					bestKey = key
				}
			}
		}

		if best == nil {
			break
		}

		head = best
	}

	return head
}

func (wf *Wavefront) advanceDistance(head *WavefrontHead) uint32 {
	if head == nil || len(head.path) == 0 {
		return 0
	}

	last := head.path[len(head.path)-1]
	if last.Terminal() || last.Opcode() == uint64(data.OpcodeHalt) {
		return 0
	}

	if jump := last.Jump(); jump > 0 {
		return jump
	}

	return 1
}

func (wf *Wavefront) bridgeHead(head *WavefrontHead) *WavefrontHead {
	if head == nil || wf.fe == nil || wf.target == 0 {
		return nil
	}

	opcodes, err := wf.fe.Resolve(head.phase, wf.target, 50)
	if err != nil || len(opcodes) == 0 {
		return nil
	}

	newPhase := head.phase
	for _, op := range opcodes {
		newPhase = wf.calc.Multiply(newPhase, op.Rotation)
	}

	synChord := data.MustNewChord()
	synChord.Set(int(newPhase))

	meta := data.MustNewChord()
	return &WavefrontHead{
		phase:        newPhase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          head.pos + uint32(len(opcodes)),
		promptIdx:    head.promptIdx,
		energy:       head.energy,
		path:         append(append([]data.Chord{}, head.path...), synChord),
		metaPath:     append(append([]data.Chord{}, head.metaPath...), meta),
		visited:      head.visited,
		fuzzyErrs:    head.fuzzyErrs,
	}
}

func (wf *Wavefront) forkHead(
	head *WavefrontHead,
	key uint64,
	pos uint32,
	phase numeric.Phase,
	chord data.Chord,
	meta data.Chord,
	energy int,
	fuzzyErrs int,
) *WavefrontHead {
	visited := make(map[uint64]bool, len(head.visited)+1)
	for k, v := range head.visited {
		visited[k] = v
	}
	visited[key] = true

	return &WavefrontHead{
		phase:        phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          pos,
		promptIdx:    head.promptIdx,
		energy:       energy,
		path:         append(append([]data.Chord{}, head.path...), chord),
		metaPath:     append(append([]data.Chord{}, head.metaPath...), meta),
		visited:      visited,
		fuzzyErrs:    fuzzyErrs,
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
	sort.Slice(heads, func(i, j int) bool {
		if heads[i].energy == heads[j].energy {
			return heads[i].pos < heads[j].pos
		}
		return heads[i].energy < heads[j].energy
	})

	if len(heads) > wf.maxHeads {
		return heads[:wf.maxHeads]
	}

	return heads
}

func (wf *Wavefront) prunePrompt(heads []*WavefrontHead) []*WavefrontHead {
	type promptKey struct {
		phase        numeric.Phase
		alignedPhase numeric.Phase
		queryPhase   numeric.Phase
		pos          uint32
		promptIdx    int
	}

	bestByKey := make(map[promptKey]*WavefrontHead, len(heads))
	for _, head := range heads {
		if head == nil {
			continue
		}

		key := promptKey{
			phase:        head.phase,
			alignedPhase: head.alignedPhase,
			queryPhase:   head.queryPhase,
			pos:          head.pos,
			promptIdx:    head.promptIdx,
		}

		existing, exists := bestByKey[key]
		if !exists || head.energy < existing.energy || (head.energy == existing.energy && head.fuzzyErrs < existing.fuzzyErrs) {
			bestByKey[key] = head
		}
	}

	compacted := make([]*WavefrontHead, 0, len(bestByKey))
	for _, head := range bestByKey {
		compacted = append(compacted, head)
	}

	sort.Slice(compacted, func(i, j int) bool {
		if compacted[i].promptIdx != compacted[j].promptIdx {
			return compacted[i].promptIdx > compacted[j].promptIdx
		}
		if compacted[i].fuzzyErrs != compacted[j].fuzzyErrs {
			return compacted[i].fuzzyErrs < compacted[j].fuzzyErrs
		}
		if compacted[i].energy != compacted[j].energy {
			return compacted[i].energy < compacted[j].energy
		}
		return compacted[i].pos < compacted[j].pos
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
	Path     []data.Chord
	MetaPath []data.Chord
	Energy   int
	Phase    numeric.Phase
	Depth    uint32
}

/*
phaseDistance returns the shortest modular distance between two GF(257) phases.
Used by the anchor PLL to decide whether drift can be corrected or must be killed.
*/
func (wf *Wavefront) phaseDistance(left, right numeric.Phase) uint32 {
	delta := int32(left) - int32(right)
	if delta < 0 {
		delta = -delta
	}
	if delta > int32(numeric.FermatPrime)/2 {
		delta = int32(numeric.FermatPrime) - delta
	}
	return uint32(delta)
}

/*
anchorCorrect implements the phase-locked loop described in INSIGHT.md.
At configured anchor positions the stored master phase in ResidualCarry can
snap a nearby drifting state back onto the canonical trajectory.
*/
func (wf *Wavefront) anchorCorrect(pos uint32, expected numeric.Phase, chord data.Chord) (numeric.Phase, int, bool) {
	if wf.anchorStride == 0 || pos == 0 || pos%wf.anchorStride != 0 {
		return 0, 0, false
	}

	anchor := numeric.Phase(chord.ResidualCarry() % uint64(numeric.FermatPrime))
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
func (wf *Wavefront) anchorViolates(pos uint32, expected numeric.Phase, chord data.Chord) bool {
	if wf.anchorStride == 0 || pos == 0 || pos%wf.anchorStride != 0 {
		return false
	}

	anchor := numeric.Phase(chord.ResidualCarry() % uint64(numeric.FermatPrime))
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
WavefrontWithFrustrationEngine attaches the Phase 4 logic solver to the search.
*/
func WavefrontWithFrustrationEngine(fe *goal.FrustrationEngineServer, target numeric.Phase) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.fe = fe
		wf.target = target
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
