package vm

import (
	"unsafe"

	"github.com/theapemachine/six/console"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

// maxGenerationSteps is the maximum number of tokens to generate in a single prompt.
const maxGenerationSteps = 256
const maxReasoningHops = 3

type bestFillFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

type recentSeed struct {
	pos    uint32
	chord  data.Chord
	events []int
}

type slotMask struct {
	Observed [5][27]bool
	Missing  [5][27]bool
	Hole     [5][27]data.Chord
	Count    int
}

func hasSeedEvent(events []int, wanted int) bool {
	for _, ev := range events {
		if ev == wanted {
			return true
		}
	}

	return false
}

func seedCube(events []int) int {
	switch {
	case hasSeedEvent(events, geometry.EventPhaseInversion):
		return 3
	case hasSeedEvent(events, geometry.EventDensitySpike):
		return 1
	case hasSeedEvent(events, geometry.EventLowVarianceFlux):
		return 2
	case hasSeedEvent(events, geometry.EventDensityTrough):
		return 4
	default:
		return 0
	}
}

func supportSeedCube(events []int) int {
	cube := seedCube(events)
	if cube == 4 {
		return 0
	}

	return cube
}

func vetoSeedCube(cube int) int {
	if cube == 4 {
		return 3
	}

	return 4
}

func seedBlock(pos uint32, chord data.Chord, events []int) int {
	if len(events) == 0 {
		return int(pos) % 27
	}

	role := int(pos % 3)

	temporal := 1
	if hasSeedEvent(events, geometry.EventDensityTrough) {
		temporal = 0
	} else if hasSeedEvent(events, geometry.EventDensitySpike) {
		temporal = 2
	}

	scale := 0
	active := chord.ActiveCount()
	if hasSeedEvent(events, geometry.EventLowVarianceFlux) || active >= 32 {
		scale = 2
	} else if active >= 12 {
		scale = 1
	}

	return role + 3*temporal + 9*scale
}

func pushRecentSeed(recent []recentSeed, seed recentSeed, limit int) []recentSeed {
	recent = append(recent, seed)
	if len(recent) <= limit {
		return recent
	}

	trimFrom := len(recent) - limit
	out := make([]recentSeed, limit)
	copy(out, recent[trimFrom:])
	return out
}

func seedQueryContext(queryCtx *geometry.IcosahedralManifold, recent []recentSeed) {
	for _, seed := range recent {
		cubeIdx := supportSeedCube(seed.events)
		vetoIdx := vetoSeedCube(cubeIdx)
		blockIdx := seedBlock(seed.pos, seed.chord, seed.events)
		current := queryCtx.Cubes[cubeIdx][blockIdx]
		veto := data.ChordHole(&current, &seed.chord)
		merged := data.ChordOR(&current, &seed.chord)
		queryCtx.Cubes[cubeIdx][blockIdx] = merged

		if veto.ActiveCount() > 0 {
			vetoMerged := data.ChordOR(&queryCtx.Cubes[vetoIdx][blockIdx], &veto)
			queryCtx.Cubes[vetoIdx][blockIdx] = vetoMerged
		}
	}
}

func applyEventsToContext(queryCtx *geometry.IcosahedralManifold, events []int) {
	for _, ev := range events {
		currentRotState := queryCtx.Header.RotState()
		nextRotState := geometry.StateTransitionMatrix[currentRotState][ev]
		queryCtx.Header.SetRotState(nextRotState)

		for c := range 5 {
			switch ev {
			case geometry.EventDensitySpike:
				queryCtx.Cubes[c].RotateX()
			case geometry.EventPhaseInversion:
				queryCtx.Cubes[c].RotateY()
			case geometry.EventDensityTrough:
				queryCtx.Cubes[c].RotateZ()
			case geometry.EventLowVarianceFlux:
				queryCtx.Cubes[c].RotateX()
				queryCtx.Cubes[c].RotateX()
			}
		}
	}
}

func mergeManifold(dst *geometry.IcosahedralManifold, src *geometry.IcosahedralManifold) {
	for c := range 5 {
		for b := range 27 {
			dst.Cubes[c][b] = data.ChordOR(&dst.Cubes[c][b], &src.Cubes[c][b])
		}
	}
}

func deriveSlotMask(
	queryCtx *geometry.IcosahedralManifold,
	expectedReality *geometry.IcosahedralManifold,
	targetCube, targetBlock int,
) slotMask {
	var mask slotMask

	for c := range 5 {
		for b := range 27 {
			if queryCtx.Cubes[c][b].ActiveCount() > 0 {
				mask.Observed[c][b] = true
			}
		}
	}

	for b := range 27 {
		hasSupportEvidence := false
		for c := 0; c < 4; c++ {
			if mask.Observed[c][b] {
				hasSupportEvidence = true
				break
			}
		}

		for c := 0; c < 4; c++ {
			if mask.Observed[c][b] {
				if expectedReality != nil {
					hole := data.ChordHole(&expectedReality.Cubes[c][b], &queryCtx.Cubes[c][b])
					if hole.ActiveCount() > 0 {
						mask.Missing[c][b] = true
						mask.Hole[c][b] = hole
						mask.Count++
					}
				}
				continue
			}

			if c == targetCube && b == targetBlock {
				mask.Missing[c][b] = true
				if expectedReality != nil {
					mask.Hole[c][b] = expectedReality.Cubes[c][b]
				}
				mask.Count++
				continue
			}

			if hasSupportEvidence {
				mask.Missing[c][b] = true
				mask.Count++
				continue
			}

			if expectedReality != nil && expectedReality.Cubes[c][b].ActiveCount() > 0 {
				mask.Missing[c][b] = true
				mask.Hole[c][b] = expectedReality.Cubes[c][b]
				mask.Count++
			}
		}

		if !mask.Observed[4][b] && hasSupportEvidence {
			mask.Missing[4][b] = true
			mask.Count++
		}
	}

	return mask
}

func integrateFill(
	queryCtx *geometry.IcosahedralManifold,
	matched *geometry.IcosahedralManifold,
	mask slotMask,
	primefield *store.PrimeField,
) int {
	filled := 0

	for c := range 5 {
		for b := range 27 {
			if !mask.Missing[c][b] {
				continue
			}

			candidate := matched.Cubes[c][b]
			if candidate.ActiveCount() == 0 {
				continue
			}

			fillChord := candidate
			if mask.Hole[c][b].ActiveCount() > 0 {
				fillChord = data.ChordGCD(&candidate, &mask.Hole[c][b])
				if fillChord.ActiveCount() == 0 {
					continue
				}
			}

			if c < 4 {
				fillChord = primefield.CleanupSnap(b, fillChord)
				prior := queryCtx.Cubes[c][b]
				veto := data.ChordHole(&prior, &fillChord)
				queryCtx.Cubes[c][b] = data.ChordOR(&prior, &fillChord)

				if veto.ActiveCount() > 0 {
					vetoCube := vetoSeedCube(c)
					queryCtx.Cubes[vetoCube][b] = data.ChordOR(&queryCtx.Cubes[vetoCube][b], &veto)
				}
			} else {
				queryCtx.Cubes[c][b] = data.ChordOR(&queryCtx.Cubes[c][b], &fillChord)
			}

			filled++
		}
	}

	return filled
}

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Simplifies generation loops using Toroidal Eigenmodes
and 5-plane Parallel MultiChord searches.
*/
type Machine struct {
	loader     *Loader
	primefield *store.PrimeField
	bestFill   bestFillFn
	stopCh     chan struct{}
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		bestFill:   kernel.BestFill,
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

func (machine *Machine) Start() error {
	machine.stopCh = make(chan struct{})

	for range machine.loader.Generate() {
		// Loader now intrinsically pipes topological sequences into the PrimeField
	}

	return nil
}

/*
Stop terminates the Machine and signaling any background processes to finish.
*/
func (machine *Machine) Stop() {
	if machine.stopCh != nil {
		close(machine.stopCh)
		machine.stopCh = nil
	}
}

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	out := make(chan byte)

	go func() {
		defer close(out)

		if machine.primefield.N == 0 {
			return
		}

		chords := make([]data.Chord, len(prompt))
		copy(chords, prompt)

		// Reuse a single MortonCoder for all decode operations
		coder := tokenizer.NewMortonCoder()

		// Spin-Up Phase: Process prompt to build angular momentum
		var zNext uint32
		var byteVal byte
		recent := make([]recentSeed, 0, 12)

		for _, chord := range chords {
			if key := machine.loader.Store().ReverseLookup(chord); key > 0 {
				_, _, byteVal = coder.Decode(key)
				out <- byteVal
			}

			// Feed the prompt through the Sequencer to build momentum context
			pos := zNext
			reset, evs := machine.loader.tokenizer.Sequencer().Analyze(int(zNext), chord)
			recent = pushRecentSeed(recent, recentSeed{pos: pos, chord: chord, events: evs}, 12)

			if reset {
				zNext = 0
			} else {
				zNext++
			}
		}

		// Initial State tracking for geodesic trajectory
		momentum, lastEvents := machine.primefield.Momentum()
		_, phiPhaseThresh := machine.loader.tokenizer.Sequencer().Phase()
		phiDecay := machine.loader.tokenizer.Sequencer().Phi()

		// Freewheel Phase: Predict forward using momentum
		// Calculate starting topological offset from the ingested prompt
		startIdx := len(chords)
		zNext = uint32(startIdx)

		for range maxGenerationSteps {
			// Natural EOS: generation stops when kinetic rotational energy dissipates
			if momentum < phiPhaseThresh {
				break
			}

			// Apply geodesic extrapolation: Move the mathematical query context forward
			// based on the trajectory defined by the last causal topological events
			var queryCtx geometry.IcosahedralManifold
			seedQueryContext(&queryCtx, recent)
			applyEventsToContext(&queryCtx, lastEvents)

			anchor := data.Chord{}
			if len(chords) > 0 {
				anchor = chords[len(chords)-1]
			}

			var expectedCtx geometry.IcosahedralManifold = queryCtx
			applyEventsToContext(&expectedCtx, lastEvents)
			if expectedReality != nil {
				mergeManifold(&expectedCtx, expectedReality)
			}

			vetoBlock := seedBlock(zNext, anchor, lastEvents)
			expectedCtx.Cubes[4][vetoBlock] = data.ChordOR(&expectedCtx.Cubes[4][vetoBlock], &anchor)
			expRealPtr := unsafe.Pointer(&expectedCtx)

			// Momentum Decay: Physics-based structural friction
			momentum *= phiDecay

			// Broadcast the predicted coordinate to GPU BestFill inference
			// to find the exact historical block that occupies this extrapolated space
			dictionaryPtr, dictionaryN, dictionaryOffset := machine.primefield.SearchSnapshot()
			if dictionaryN == 0 {
				break
			}

			cubeIndex := supportSeedCube(lastEvents)
			blockIndex := seedBlock(zNext, anchor, lastEvents)

			cycleGuard := make(map[int]struct{}, maxReasoningHops)
			lastBestIdx := -1
			previousScore := -1.0
			for range maxReasoningHops {
				mask := deriveSlotMask(&queryCtx, &expectedCtx, cubeIndex, blockIndex)
				if mask.Count == 0 {
					break
				}

				bestIdx, score, err := machine.bestFill(
					dictionaryPtr,
					dictionaryN,
					unsafe.Pointer(&queryCtx),
					expRealPtr,
					0,
					unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)

				if err != nil {
					console.Error(err, "context", "BestFill generation")
					break
				}

				if previousScore >= 0 && score <= previousScore {
					break
				}
				previousScore = score

				resolvedIdx := bestIdx + dictionaryOffset
				lastBestIdx = bestIdx
				if _, seen := cycleGuard[resolvedIdx]; seen {
					break
				}
				cycleGuard[resolvedIdx] = struct{}{}

				console.Trace("BestFill Retrieved Geodesic Target", "bestIdx", bestIdx)
				matched := machine.primefield.Manifold(resolvedIdx)
				filled := integrateFill(&queryCtx, &matched, mask, machine.primefield)
				if filled == 0 {
					break
				}

				applyEventsToContext(&queryCtx, lastEvents)
			}

			nextChord := queryCtx.Cubes[cubeIndex][blockIndex]

			console.Trace("generation step",
				"z", zNext,
				"cube", cubeIndex,
				"block", blockIndex,
				"bestIdx", lastBestIdx,
				"active", nextChord.ActiveCount(),
			)

			if nextChord.ActiveCount() == 0 {
				break
			}

			// Translate generated HDC geometric pattern back into standard byte byte
			if key := machine.loader.Store().ReverseLookup(nextChord); key > 0 {
				_, _, b := coder.Decode(key)
				console.Trace("Decoded token byte", "byte", string(b), "key", key)
				out <- b
			}

			// Advance positional sequencing exactly as ingestion did
			reset, evs := machine.loader.tokenizer.Sequencer().Analyze(int(zNext), nextChord)
			recent = pushRecentSeed(recent, recentSeed{pos: zNext, chord: nextChord, events: evs}, 12)
			chords = append(chords, nextChord)
			if reset {
				zNext = 0
				lastEvents = evs
			} else {
				zNext++
			}
		}
	}()

	return out
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}

func MachineWithPrimeField(pf *store.PrimeField) machineOpts {
	return func(machine *Machine) {
		machine.primefield = pf
	}
}

func MachineWithBestFill(fn bestFillFn) machineOpts {
	return func(machine *Machine) {
		if fn != nil {
			machine.bestFill = fn
		}
	}
}

type MachineError string

const (
	ErrNoChordFound                MachineError = "no chord found"
	ErrMultiScaleCooccurrenceBuild MachineError = "failed to build multiscale cooccurrence"
)

func (e MachineError) Error() string {
	return string(e)
}
