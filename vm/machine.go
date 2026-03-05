package vm

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Simplifies generation loops using Toroidal Eigenmodes
and 5-plane Parallel MultiChord searches.
*/
type Machine struct {
	loader     *Loader
	primefield *store.PrimeField
	eigen      *geometry.EigenMode
	stopCh     chan struct{}
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		eigen:      geometry.NewEigenMode(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

func (machine *Machine) Start() error {
	machine.stopCh = make(chan struct{})
	var chords []data.Chord

	for token := range machine.loader.GenerateTokens() {
		machine.primefield.InsertWithRef(token.Chord, store.GeomRef{
			TokenID:  token.TokenID,
			SampleID: token.SampleID,
			Pos:      token.Pos,
			Boundary: token.Boundary,
		})
		chords = append(chords, token.Chord)
	}

	fmt.Println("Start inserted chords:", len(chords)) // Debug!

	if err := machine.eigen.BuildMultiScaleCooccurrence(chords); err != nil {
		return console.Error(fmt.Errorf("failed to build multiscale cooccurrence: %w", err),
			"total_chords", len(chords),
			"store", machine.loader.holdoutType,
		)
	}

	// Start asynchronous continuous metabolic consolidation
	if machine.loader != nil && machine.loader.Store() != nil {
		go machine.loader.Store().SleepCycle(machine.stopCh)
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
SpanResult is the output of a single GPU MultiChord probe.
*/
type SpanResult struct {
	Index    int
	Score    float64
	Chord    geometry.IcosahedralManifold
	TokenID  uint64
	SampleID uint32
	Pos      uint32
	Symbol   byte
}

type SpanMatch struct {
	MatchIndex int
	StartIndex int
	EndIndex   int
	Score      float64
	SampleID   uint32
	StartPos   uint32
	EndPos     uint32
}

func (machine *Machine) BestSpan(prompt []data.Chord, expectedReality *geometry.IcosahedralManifold) (SpanMatch, error) {
	if len(prompt) == 0 {
		return SpanMatch{
			MatchIndex: -1,
			StartIndex: -1,
			EndIndex:   -1,
		}, nil
	}

	window := 21
	start := max(len(prompt)-window, 0)

	tempField := store.NewPrimeField()
	for _, chord := range prompt[start:] {
		tempField.Insert(chord)
	}
	activeCtx := tempField.Manifold(tempField.N - 1)
	expectedPtr := unsafe.Pointer(expectedReality)
	if expectedReality == nil {
		expectedPtr = unsafe.Pointer(&activeCtx)
	}
	dictPtr, numChords := machine.primefield.Snapshot()

	match, err := kernel.BestSpan(
		dictPtr,
		numChords,
		unsafe.Pointer(&activeCtx),
		expectedPtr,
		unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
	)
	if err != nil {
		return SpanMatch{}, err
	}
	if match.Index < 0 || match.Index >= numChords {
		return SpanMatch{}, MachineErrNotFound
	}

	replayRefs, endIdx := machine.primefield.ReplaySpan(match.Index+1, 4096)
	span := SpanMatch{
		MatchIndex: match.Index,
		StartIndex: match.Index + 1,
		EndIndex:   endIdx,
		Score:      match.Score,
	}

	if len(replayRefs) > 0 {
		span.SampleID = replayRefs[0].SampleID
		span.StartPos = replayRefs[0].Pos
		span.EndPos = replayRefs[len(replayRefs)-1].Pos
	}

	return span, nil
}

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(prompt []data.Chord, expectedReality *geometry.IcosahedralManifold) chan SpanResult {
	out := make(chan SpanResult)

	go func() {
		defer close(out)

		span, err := machine.BestSpan(prompt, expectedReality)
		if err != nil {
			console.Error(MachineErrNotFound,
				"error", err,
			)
			return
		}
		if span.MatchIndex < 0 {
			return
		}

		coder := tokenizer.NewMortonCoder()
		replayRefs, _ := machine.primefield.ReplaySpan(span.StartIndex, 4096)
		generatedText := make([]byte, 0, len(replayRefs))

		for offset, ref := range replayRefs {
			_, _, symbol := coder.Decode(ref.TokenID)
			generatedText = append(generatedText, symbol)

			replayIdx := span.StartIndex + offset
			out <- SpanResult{
				Index:    replayIdx,
				Score:    span.Score,
				Chord:    machine.primefield.Manifold(replayIdx),
				TokenID:  ref.TokenID,
				SampleID: ref.SampleID,
				Pos:      ref.Pos,
				Symbol:   symbol,
			}
		}

		console.Info("Machine Generation Completed",
			"match", span.MatchIndex,
			"start", span.StartIndex,
			"end", span.EndIndex,
			"output", string(generatedText),
		)
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

func MachineWithEigenMode(eigen *geometry.EigenMode) machineOpts {
	return func(machine *Machine) {
		machine.eigen = eigen
	}
}

type MachineError string

const (
	MachineErrNotFound MachineError = "no chord found"
)

func (e MachineError) Error() string {
	return string(e)
}
