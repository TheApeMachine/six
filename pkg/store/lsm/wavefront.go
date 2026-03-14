package lsm

import (
	"sort"

	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
WavefrontHead is a single competing search state propagating through
the spatial index. Each head carries a GF(257) phase accumulator and
an energy score (lower is better). At collisions, heads fork.
*/
type WavefrontHead struct {
	phase   numeric.Phase
	pos     uint32
	energy  int
	path    []data.Chord
	visited map[uint64]bool
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
	idx      *SpatialIndexServer
	calc     *numeric.Calculus
	maxHeads int
	maxDepth uint32
	fe       *synthesis.FrustrationEngine
	target   numeric.Phase
}

type wavefrontOpts func(*Wavefront)

/*
NewWavefront creates a wavefront search engine bound to a spatial index.
*/
func NewWavefront(idx *SpatialIndexServer, opts ...wavefrontOpts) *Wavefront {
	wf := &Wavefront{
		idx:      idx,
		calc:     numeric.NewCalculus(),
		maxHeads: 64,
		maxDepth: 4096,
	}

	for _, opt := range opts {
		opt(wf)
	}

	return wf
}

/*
Search propagates the wavefront from a prompt chord and returns the
best-matching paths ranked by energy (lowest first).

The algorithm:
1. Convert the prompt to a starting phase via Calculus.SumBytes
2. Seed heads at every position-0 entry that resonates with the prompt
3. For each head, advance to pos+1 and score all candidates
4. At collision chains, fork into multiple heads
5. Prune heads that exceed energy budget or have phase mismatch
6. Return paths sorted by cumulative energy
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

	for depth := uint32(0); depth < wf.maxDepth && len(heads) > 0; depth++ {
		heads = wf.advance(heads, prompt, interest, danger)

		if len(heads) > wf.maxHeads {
			heads = wf.prune(heads)
		}
	}

	return wf.collect(heads)
}

/*
PromptToPhase converts a raw prompt string into a GF(257) starting phase.
This is the injection point described in INSIGHT.md lines 901–913.
*/
func (wf *Wavefront) PromptToPhase(prompt []byte) numeric.Phase {
	return wf.calc.SumBytes(prompt)
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

	for _, key := range keys {
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

		chain := wf.idx.followChainUnsafe(key)

		for _, stateChord := range chain {
			heads = append(heads, &WavefrontHead{
				phase:   startPhase,
				pos:     0,
				energy:  symbolChord.XOR(prompt).ActiveCount(),
				path:    []data.Chord{stateChord},
				visited: map[uint64]bool{key: true},
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
		nextPos := head.pos + 1
		nextKeys, hasNext := wf.idx.positionIndex[nextPos]

		didAdvance := false

		if hasNext && len(nextKeys) > 0 {
			for _, key := range nextKeys {
				if head.visited[key] {
					continue
				}

				value, exists := wf.idx.entries[key]
				if !exists {
					continue
				}

				_, nextSymbol := morton.Unpack(key)

				expectedPhase := wf.calc.Multiply(
					head.phase,
					wf.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(nextSymbol)),
				)

				chain := wf.idx.followChainUnsafe(key)

				for _, stateChord := range chain {
					if !stateChord.Has(int(expectedPhase)) {
						continue
					}

					symbolChord := data.BaseChord(nextSymbol)
					residue := prompt.XOR(symbolChord)
					stepEnergy := residue.ActiveCount()

					if interest != nil {
						resonance := value.AND(*interest)
						stepEnergy -= resonance.ActiveCount()
					}

					if danger != nil {
						punish := value.AND(*danger)
						stepEnergy += punish.ActiveCount()
					}

					visited := make(map[uint64]bool, len(head.visited)+1)
					for k, v := range head.visited {
						visited[k] = v
					}
					visited[key] = true

					fork := &WavefrontHead{
						phase:   expectedPhase,
						pos:     nextPos,
						energy:  head.energy + stepEnergy,
						path:    append(append([]data.Chord{}, head.path...), stateChord),
						visited: visited,
					}

					next = append(next, fork)
					didAdvance = true
				}
			}
		}

		if !didAdvance {
			// Phase 4 Higher Logic: If the raw data span fails, the Cantilever
			// vibrates the Frustration Engine to discover a MacroOpcode bridge.
			if wf.fe != nil && wf.target != 0 {
				opcodes, err := wf.fe.Resolve(head.phase, wf.target, 50)
				if err == nil && len(opcodes) > 0 {
					newPhase := head.phase
					for _, op := range opcodes {
						newPhase = wf.calc.Multiply(newPhase, op.Rotation)
					}

					// Package the logic circuit into a pure state representation
					synChord := data.MustNewChord()
					synChord.Set(int(newPhase))

					fork := &WavefrontHead{
						phase:   newPhase,
						pos:     head.pos + uint32(len(opcodes)),
						energy:  head.energy, // Zero penalty bridging!
						path:    append(append([]data.Chord{}, head.path...), synChord),
						visited: head.visited,
					}

					next = append(next, fork)
					continue
				}
			}

			// Normal dead end, preserve the head
			next = append(next, head)
		}
	}

	return next
}

/*
prune keeps only the top maxHeads by energy (lowest first).
*/
func (wf *Wavefront) prune(heads []*WavefrontHead) []*WavefrontHead {
	sort.Slice(heads, func(i, j int) bool {
		return heads[i].energy < heads[j].energy
	})

	if len(heads) > wf.maxHeads {
		return heads[:wf.maxHeads]
	}

	return heads
}

/*
WavefrontResult is a ranked path from a wavefront search.
*/
type WavefrontResult struct {
	Path    []data.Chord
	Energy  int
	Phase   numeric.Phase
	Depth   uint32
}

/*
collect converts surviving heads into ranked results.
*/
func (wf *Wavefront) collect(heads []*WavefrontHead) []WavefrontResult {
	results := make([]WavefrontResult, 0, len(heads))

	for _, head := range heads {
		if len(head.path) == 0 {
			continue
		}

		results = append(results, WavefrontResult{
			Path:   head.path,
			Energy: head.energy,
			Phase:  head.phase,
			Depth:  head.pos,
		})
	}

	for i := 0; i < len(results)-1; i++ {
		minIdx := i

		for j := i + 1; j < len(results); j++ {
			if results[j].Energy < results[minIdx].Energy {
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
func WavefrontWithFrustrationEngine(fe *synthesis.FrustrationEngine, target numeric.Phase) wavefrontOpts {
	return func(wf *Wavefront) {
		wf.fe = fe
		wf.target = target
	}
}
