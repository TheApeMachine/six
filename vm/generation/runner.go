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
			Pos: pos, ByteVal: byteVal, Chord: chord, Events: events,
		}, runner.config.RecentLimit)

		if reset {
			zNext = 0
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

		cubeIndex := SupportSeedCube(lastEvents)

		cycleGuard := make(map[int]struct{}, runner.config.MaxReasoningHops)
		retained := 0
		lastBestIdx := -1
		previousScore := -1.0

		for range runner.config.MaxReasoningHops {
			if runner.shouldStop() {
				return
			}

			mask := DeriveSlotMask(&queryCtx, &expectedCtx, cubeIndex, -1)
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
			lastBestIdx = bestIdx
			if _, seen := cycleGuard[resolvedIdx]; seen {
				break
			}
			cycleGuard[resolvedIdx] = struct{}{}

			console.Trace("BestFill Retrieved Geodesic Target", "bestIdx", bestIdx)
			matched := runner.config.PrimeField.Manifold(resolvedIdx)
			filled := IntegrateFill(&queryCtx, &matched, mask, runner.config.PrimeField)
			if filled == 0 {
				break
			}
		}

		// Output: scan all 257 faces on the support cube.
		// Since data is placed at face=byteVal (self-addressing), the
		// face index with highest activity IS the predicted byte value.
		bestFace, nextChord, ok := BestFace(&queryCtx, cubeIndex)

		console.Trace("generation step",
			"z", zNext,
			"cube", cubeIndex,
			"bestFace", bestFace,
			"bestIdx", lastBestIdx,
			"active", nextChord.ActiveCount(),
		)

		if !ok || nextChord.ActiveCount() == 0 {
			console.Info("generation exit: no active face", "z", zNext, "cube", cubeIndex)
			break
		}

		// Face 256 is the structural delimiter.
		if bestFace >= 256 {
			console.Info("generation exit: delimiter face", "z", zNext)
			break
		}

		// The face index IS the byte value. No inverse mapping needed.
		nextByte := byte(bestFace)

		if !runner.sendOrStop(out, nextByte) {
			return
		}
		lastByteVal = nextByte

		reset, events := runner.config.Sequencer.Analyze(int(zNext), nextChord)

		// Compose new event rotations — O(1) per event, zero data movement.
		for _, ev := range events {
			rot = rot.Compose(geometry.EventRotation(ev))
		}

		recent = PushRecentSeed(recent, RecentSeed{
			Pos: zNext, ByteVal: nextByte, Chord: nextChord, Events: events,
		}, runner.config.RecentLimit)
		chords = append(chords, nextChord)

		if len(events) > 0 {
			lastEvents = events
		}

		if reset {
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
