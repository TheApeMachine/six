package vm

import (
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

func deterministicBestFill(_ unsafe.Pointer, _ int, _ unsafe.Pointer, _ unsafe.Pointer, _ int, _ unsafe.Pointer) (int, float64, error) {
	return 0, 1.0, nil
}

func buildIntegrationMachine(corpus [][]byte, bestFill bestFillFn) (*Machine, *store.PrimeField) {
	pf := store.NewPrimeField()

	loader := NewLoader(
		LoaderWithStore(store.NewLSMSpatialIndex(1.0)),
		LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(local.New(corpus)),
			),
		),
		LoaderWithPrimeField(pf),
	)

	opts := []machineOpts{
		MachineWithLoader(loader),
		MachineWithPrimeField(pf),
	}

	if bestFill != nil {
		opts = append(opts, MachineWithBestFill(bestFill))
	}

	return NewMachine(opts...), pf
}

func promptToChords(prompt string) []data.Chord {
	chords := make([]data.Chord, 0, len(prompt))
	for i := range len(prompt) {
		chords = append(chords, data.BaseChord(prompt[i]))
	}
	return chords
}

func collectBytes(out <-chan byte) []byte {
	res := make([]byte, 0, 32)
	for b := range out {
		res = append(res, b)
	}
	return res
}

func TestMachineIntegration_StartPromptAndSnapshot(t *testing.T) {
	machine, pf := buildIntegrationMachine([][]byte{
		[]byte("ab"),
		[]byte("ac"),
	}, deterministicBestFill)

	machine.Start()

	require.GreaterOrEqual(t, pf.N, 1)
	ptr, n, offset := pf.SearchSnapshot()
	require.NotNil(t, ptr)
	if pf.N == 1 {
		require.Equal(t, 1, n)
		require.Equal(t, 0, offset)
	} else {
		require.Equal(t, pf.N-1, n)
		require.Equal(t, 1, offset)
	}

	out := collectBytes(machine.Prompt(promptToChords("a"), nil))
	require.NotEmpty(t, out)
	require.Equal(t, byte('a'), out[0])

	active := pf.Manifold(0)
	require.Greater(t, active.Cubes[0][0].ActiveCount(), 0)
}

func TestMachineIntegration_ExpectedRealityPropagatesToBestFill(t *testing.T) {
	var sawExpectedReality atomic.Bool

	bestFill := func(_ unsafe.Pointer, _ int, _ unsafe.Pointer, expectedReality unsafe.Pointer, _ int, _ unsafe.Pointer) (int, float64, error) {
		expected := (*geometry.IcosahedralManifold)(expectedReality)
		if expected.Cubes[0][0].Has(511) {
			sawExpectedReality.Store(true)
		}

		return 0, 1.0, nil
	}

	machine, pf := buildIntegrationMachine([][]byte{
		[]byte("ab"),
		[]byte("ac"),
	}, bestFill)

	machine.Start()
	pf.SetMomentum(100.0)

	expectedReality := &geometry.IcosahedralManifold{}
	expectedReality.Cubes[0][0].Set(511)

	out := collectBytes(machine.Prompt(promptToChords("a"), expectedReality))
	require.NotEmpty(t, out)
	require.True(t, sawExpectedReality.Load())
}

func TestMachineIntegration_PromptFillsNextTokenFromCorpus(t *testing.T) {
	machine, pf := buildIntegrationMachine([][]byte{
		[]byte("ab"),
		[]byte("ab"),
	}, deterministicBestFill)

	machine.Start()
	pf.SetMomentum(100.0)

	out := collectBytes(machine.Prompt(promptToChords("a"), nil))
	require.GreaterOrEqual(t, len(out), 2)
	require.Equal(t, byte('a'), out[0])
	require.Equal(t, byte('b'), out[1])
}

func TestApplyEventsToContextTracksHeaderRotation(t *testing.T) {
	ctx := &geometry.IcosahedralManifold{}
	events := []int{
		geometry.EventDensitySpike,
		geometry.EventPhaseInversion,
		geometry.EventDensityTrough,
		geometry.EventLowVarianceFlux,
	}

	applyEventsToContext(ctx, events)

	expectedRot := uint8(0)
	for _, ev := range events {
		expectedRot = geometry.StateTransitionMatrix[expectedRot][ev]
	}

	require.Equal(t, expectedRot, ctx.Header.RotState())
}
