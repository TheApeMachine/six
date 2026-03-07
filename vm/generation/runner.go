package generation

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

type BestFillFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

type BestFillWithFieldFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedField *geometry.ExpectedField,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

type Sequencer interface {
	Analyze(pos int, current data.Chord) (bool, []int)
	Phase() (float64, float64)
	Phi() float64
}

type RunnerConfig struct {
	Prompt             []data.Chord
	PromptBytes        []byte // The raw byte values for each prompt chord.
	ExpectedReality    *geometry.IcosahedralManifold
	ExpectedField      *geometry.ExpectedField
	PrimeField         *store.PrimeField
	Sequencer          Sequencer
	ReverseLookup      func(chord data.Chord) uint64
	BestFill           BestFillFn
	BestFillWithField  BestFillWithFieldFn
	OnBranchPolicy     func(margin float64, retained *int)
	OnAnchorVeto       func()
	StopCh             <-chan struct{}
	MaxGenerationSteps int
	MaxReasoningHops   int
	RecentLimit        int
}

type Runner struct {
	config RunnerConfig
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	if config.PrimeField == nil {
		return nil, fmt.Errorf("generation.NewRunner: missing RunnerConfig.PrimeField")
	}
	if config.Sequencer == nil {
		return nil, fmt.Errorf("generation.NewRunner: missing RunnerConfig.Sequencer")
	}
	if config.ReverseLookup == nil {
		return nil, fmt.Errorf("generation.NewRunner: missing RunnerConfig.ReverseLookup")
	}
	if config.ExpectedField != nil && config.BestFillWithField == nil {
		return nil, fmt.Errorf("generation.NewRunner: missing RunnerConfig.BestFillWithField for ExpectedField generation")
	}
	if config.ExpectedField == nil && config.BestFill == nil {
		return nil, fmt.Errorf("generation.NewRunner: missing RunnerConfig.BestFill")
	}

	if config.MaxGenerationSteps <= 0 {
		config.MaxGenerationSteps = 256
	}
	if config.MaxReasoningHops <= 0 {
		config.MaxReasoningHops = 3
	}
	if config.RecentLimit <= 0 {
		config.RecentLimit = 12
	}

	return &Runner{config: config}, nil
}

func (runner *Runner) Run() chan byte {
	out := make(chan byte, 1)

	go func() {
		defer close(out)
		runner.run(out)
	}()

	return out
}

func (runner *Runner) run(out chan byte) {
	if runner.config.PrimeField == nil || runner.config.PrimeField.N == 0 {
		return
	}

	chords := make([]data.Chord, len(runner.config.Prompt))
	copy(chords, runner.config.Prompt)

	coder := tokenizer.NewMortonCoder()

	var zNext uint32
	recent := make([]RecentSeed, 0, runner.config.RecentLimit)
	var lastByteVal byte

	// Compose rotation state from prompt replay.
	// No physical permutations — just O(1) arithmetic per event.
	rot := geometry.IdentityRotation()

	// Replay the prompt: emit bytes and build recent-seed history.
	for i, chord := range chords {
		var byteVal byte
		if i < len(runner.config.PromptBytes) {
			byteVal = runner.config.PromptBytes[i]
		} else if runner.config.ReverseLookup != nil {
			if key := runner.config.ReverseLookup(chord); key > 0 {
				_, _, byteVal = coder.Decode(key)
			}
		}

		if !runner.sendOrStop(out, byteVal) {
			return
		}

		lastByteVal = byteVal

		pos := zNext
		reset, events := runner.config.Sequencer.Analyze(int(zNext), chord)

		// Compose event rotations into the running state.
		for _, ev := range events {
			rot = rot.Compose(geometry.EventRotation(ev))
		}

		recent = PushRecentSeed(recent, RecentSeed{
			Pos: pos, ByteVal: byteVal, Chord: chord, Events: events, Rot: rot,
		}, runner.config.RecentLimit)

		pos++
		if reset {
			zNext = 0
			pos = 0
		} else {
			zNext++
		}
	}

	momentum, corpusLastEvents := runner.config.PrimeField.Momentum()
	_, phiPhaseThresh := runner.config.Sequencer.Phase()
	phiDecay := runner.config.Sequencer.Phi()

	// Use events from the prompt replay if available, otherwise
	// fall back to the corpus's last events for cube selection.
	lastEvents := corpusLastEvents
	if len(recent) > 0 {
		lastRecentEvents := recent[len(recent)-1].Events
		if len(lastRecentEvents) > 0 {
			lastEvents = lastRecentEvents
		}
	}

	console.Info("generation init",
		"momentum", momentum,
		"phiPhaseThresh", phiPhaseThresh,
		"phiDecay", phiDecay,
		"promptLen", len(chords),
	)

	for range runner.config.MaxGenerationSteps {
		if runner.shouldStop() {
			return
		}

		if momentum < phiPhaseThresh {
			console.Info("generation exit: momentum exhausted",
				"momentum", momentum, "threshold", phiPhaseThresh)
			break
		}

		// Build query context with data at self-addressed faces (byteVal).
		// No physical permutation, no index rotation — data placement
		// matches PrimeField.Insert semantics exactly.
		var queryCtx geometry.IcosahedralManifold
		SeedQueryContext(&queryCtx, recent, rot)

		anchor := data.Chord{}
		if len(chords) > 0 {
			anchor = chords[len(chords)-1]
		}

		expectedCtx := queryCtx
		if runner.config.ExpectedReality != nil {
			MergeManifold(&expectedCtx, runner.config.ExpectedReality)
		}

		// Veto: place anchor at the last byte's face.
		// Self-addressing: face = byteVal, matching Insert semantics.
		vetoBlock := SeedBlock(lastByteVal)
		expectedCtx.Cubes[4][vetoBlock] = data.ChordOR(&expectedCtx.Cubes[4][vetoBlock], &anchor)
		if anchor.ActiveCount() > 0 && runner.config.OnAnchorVeto != nil {
			runner.config.OnAnchorVeto()
		}

		expRealPtr := unsafe.Pointer(&expectedCtx)
		momentum *= phiDecay

		dictionaryPtr, dictionaryN, dictionaryOffset := runner.config.PrimeField.SearchSnapshot()
		if dictionaryN == 0 {
			console.Info("generation exit: empty dictionary")
			break
		}

		console.Trace("dictionary snapshot length", "n", dictionaryN, "offset", dictionaryOffset)

		cubeIndex := SupportSeedCube(lastEvents)

		var priorCtx geometry.IcosahedralManifold
		MergeManifold(&priorCtx, &queryCtx)

		cycleGuard := make(map[int]struct{}, runner.config.MaxReasoningHops)
		retained := 0
		previousScore := -1.0

		var matched geometry.IcosahedralManifold

		for range runner.config.MaxReasoningHops {
			if runner.shouldStop() {
				return
			}

			// We don't restrict to just cubeIndex. We want to pull in any highly relevant face
			// across ALL data cubes, in case the next token rotated somewhere else.
			mask := DeriveSlotMask(&queryCtx, &expectedCtx, -1, -1)
			if mask.Count == 0 {
				console.Info("generation exit: empty mask", "cube", cubeIndex)
				break
			}

			var (
				bestIdx int
				score   float64
				err     error
			)

			if runner.config.ExpectedField != nil {
				if runner.config.BestFillWithField == nil {
					console.Error(fmt.Errorf("generation runner: nil RunnerConfig.BestFillWithField"), "context", "BestFill generation")
					return
				}
				bestIdx, score, err = runner.config.BestFillWithField(
					dictionaryPtr,
					dictionaryN,
					unsafe.Pointer(&queryCtx),
					expRealPtr,
					runner.config.ExpectedField,
					0,
					unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)
			} else {
				if runner.config.BestFill == nil {
					console.Error(fmt.Errorf("generation runner: nil RunnerConfig.BestFill"), "context", "BestFill generation")
					return
				}
				bestIdx, score, err = runner.config.BestFill(
					dictionaryPtr,
					dictionaryN,
					unsafe.Pointer(&queryCtx),
					expRealPtr,
					0,
					unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)
			}

			if err != nil {
				console.Error(err, "context", "BestFill generation")
				break
			}

			margin := 0.0
			if previousScore >= 0 {
				margin = score - previousScore
				if margin <= 0 {
					break
				}
				if runner.config.OnBranchPolicy != nil {
					runner.config.OnBranchPolicy(margin, &retained)
				}
			}
			previousScore = score

			resolvedIdx := bestIdx + dictionaryOffset
			if _, seen := cycleGuard[resolvedIdx]; seen {
				break
			}
			cycleGuard[resolvedIdx] = struct{}{}

			console.Trace("BestFill Retrieved Geodesic Target", "bestIdx", bestIdx)
			matched = runner.config.PrimeField.Manifold(resolvedIdx)
			filled := IntegrateFill(&queryCtx, &matched, mask, runner.config.PrimeField)
			// ADD THIS LOG:
			console.Info("IntegrateFill executed", "filled", filled, "bestIdx", resolvedIdx)
			if filled == 0 {
				break
			}
		}

		// Scan all 256 possible bytes and forward-simulate topological routing
		bestByte := byte(0)
		bestScore := -1 // use int since ActiveCount returns int
		var bestEvents []int
		var bestChord data.Chord
		var bestReset bool

		for b := 0; b < 256; b++ {
			candidateByte := byte(b)
			candidateChord := data.BaseChord(candidateByte)

			reset, evs := runner.config.Sequencer.Analyze(int(zNext), candidateChord)

			testRot := rot
			for _, e := range evs {
				testRot = testRot.Compose(geometry.EventRotation(e))
			}
			faceIdx := testRot.Forward(int(candidateByte))

			// We don't know the exact A5 rotation alignment at freeze-time, but intra-cube
			// offsets (faces) are permutation-invariant. Collapse the depth dimension!
			var mergedManifold data.Chord
			var mergedPrior data.Chord

			for c := 0; c < 5; c++ {
				mergedManifold = data.ChordOR(&mergedManifold, &matched.Cubes[c][faceIdx])
				mergedPrior = data.ChordOR(&mergedPrior, &priorCtx.Cubes[c][faceIdx])
			}

			// hole represents the 'novelty' in the manifold: what is present there,
			// but NOT present anywhere in our recent priorCtx at this face.
			hole := data.ChordHole(&mergedManifold, &mergedPrior)
			shared := data.ChordGCD(&candidateChord, &hole)
			score := shared.ActiveCount()

			if score > bestScore {
				bestScore = score
				bestByte = candidateByte
				bestEvents = evs
				bestChord = candidateChord
				bestReset = reset
			}
		}

		console.Trace("generation step",
			"z", zNext,
			"bestByte", bestByte,
			"bestScore", bestScore,
		)

		if bestScore <= 0 {
			var nonZeroCount int
			for c := 0; c < 5; c++ {
				for f := 0; f < 257; f++ {
					if matched.Cubes[c][f].ActiveCount() > 0 {
						nonZeroCount++
					}
				}
			}
			console.Info("generation exit: no active candidate", "z", zNext, "matched_faces", nonZeroCount)
			break
		}

		nextByte := bestByte
		nextChord := bestChord
		events := bestEvents

		// We must actually mutate the sequencer state to accurately follow the time series
		runner.config.Sequencer.Analyze(int(zNext), nextChord)

		if !runner.sendOrStop(out, nextByte) {
			return
		}
		lastByteVal = nextByte

		for _, e := range events {
			rot = rot.Compose(geometry.EventRotation(e))
		}

		recent = PushRecentSeed(recent, RecentSeed{
			Pos: zNext, ByteVal: nextByte, Chord: nextChord, Events: events, Rot: rot,
		}, runner.config.RecentLimit)
		chords = append(chords, nextChord)

		if len(events) > 0 {
			lastEvents = events
		}

		if bestReset {
			zNext = 0
		} else {
			zNext++
		}
	}
}

func (runner *Runner) sendOrStop(out chan byte, v byte) bool {
	select {
	case out <- v:
		return true
	default:
	}

	if runner.config.StopCh == nil {
		out <- v
		return true
	}

	select {
	case out <- v:
		return true
	case <-runner.config.StopCh:
		return false
	}
}

func (runner *Runner) shouldStop() bool {
	if runner.config.StopCh == nil {
		return false
	}

	select {
	case <-runner.config.StopCh:
		return true
	default:
		return false
	}
}
