package vm

import (
	"math/rand"
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/vm/cortex"
)

type bestFillFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Generation uses the reactive cortex graph — a volatile
working-memory network of resonating MacroCubes that vibrates
until convergence, emitting output via BestFace decoding.
*/
type Machine struct {
	loader     *Loader
	primefield *store.PrimeField
	substrate  *geometry.HybridSubstrate
	bestFill   bestFillFn
	stopCh     chan struct{}
	eigenMode  *geometry.EigenMode
	sequencer  cortex.Analyzer
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
	machine.substrate = geometry.NewHybridSubstrate()

	// Ensure the Loader has all internal dependencies.
	if machine.loader.store == nil {
		machine.loader.store = store.NewLSMSpatialIndex(1.0)
	}

	machine.loader.primefield = machine.primefield

	var sequence []data.Chord
	var bytesSeq []byte
	var currentSampleID uint32
	var lastPos uint32
	first := true

	for res := range machine.loader.Generate() {
		// Detect boundary: change in SampleID or gap in Pos
		isBoundary := !first && (res.SampleID != currentSampleID || res.Pos != lastPos+1)

		if isBoundary {
			// Write explicit pointers into face 256
			rot := geometry.IdentityRotation()
			for i := 0; i < len(bytesSeq); i++ {
				var ptr data.Chord
				for j := 0; j < 5; j++ {
					ptr.Set(rand.Intn(257))
				}

				machine.primefield.StorePointer(rot, ptr)

				suffix := make([]byte, len(bytesSeq)-i)
				copy(suffix, bytesSeq[i:])

				machine.substrate.Add(ptr, geometry.NewPhaseDial(), suffix)

				if i < len(bytesSeq) {
					rot = rot.Compose(geometry.RotationForByte(bytesSeq[i]))
				}
			}

			dial := geometry.NewPhaseDial()
			dial = dial.EncodeFromChords(sequence)

			machine.substrate.Add(
				data.Chord{},
				dial,
				[]byte("sequence"),
			)

			sequence = sequence[:0]
			bytesSeq = bytesSeq[:0]
		}

		if res.Chord.ActiveCount() > 0 {
			sequence = append(sequence, res.Chord)
			bytesSeq = append(bytesSeq, res.Symbol)
		}

		currentSampleID = res.SampleID
		lastPos = res.Pos
		first = false
	}

	// Flush trailing chords that arrived after the last boundary.
	if len(sequence) > 0 {
		rot := geometry.IdentityRotation()
		for i := 0; i < len(bytesSeq); i++ {
			var ptr data.Chord
			for j := 0; j < 5; j++ {
				ptr.Set(rand.Intn(257))
			}

			machine.primefield.StorePointer(rot, ptr)

			suffix := make([]byte, len(bytesSeq)-i)
			copy(suffix, bytesSeq[i:])

			machine.substrate.Add(ptr, geometry.NewPhaseDial(), suffix)

			if i < len(bytesSeq) {
				rot = rot.Compose(geometry.RotationForByte(bytesSeq[i]))
			}
		}

		dial := geometry.NewPhaseDial()
		dial = dial.EncodeFromChords(sequence)

		machine.substrate.Add(
			data.Chord{},
			dial,
			[]byte("sequence"),
		)
	}

	// Build EigenModes from the ingested manifold topology.
	machine.primefield.BuildEigenModes()
	machine.eigenMode = machine.primefield.EigenMode()

	// Wire the Sequencer with the trained EigenMode.
	if machine.loader.Tokenizer() != nil && machine.loader.Tokenizer().Sequencer() != nil {
		seq := machine.loader.Tokenizer().Sequencer()
		seq.SetEigenMode(machine.eigenMode)
		machine.sequencer = seq
	}

	return nil
}

/*
Stop terminates the Machine and signals any background processes to finish.
*/
func (machine *Machine) Stop() {
	if machine.stopCh != nil {
		close(machine.stopCh)
		machine.stopCh = nil
	}
}

/*
Think generates output using the volatile cortex graph — a reactive
working-memory network that reasons about the prompt by composing
rotation states and dreaming against the bedrock PrimeField.

Unlike Prompt (which does O(1) holographic suffix lookup), Think
creates a cortex graph, injects the prompt with sequencer-driven
topological events, vibrates until convergence, and extracts output
from the sink node. The question acts as a geometric transformation
that filters the accumulated premise state.
*/
func (machine *Machine) Think(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	cfg := cortex.Config{
		InitialNodes: 8,
		PrimeField:   machine.primefield,
		Substrate:    machine.substrate,
		BestFill:     kernel.BestFill,
		EigenMode:    machine.eigenMode,
		Sequencer:    machine.sequencer,
		StopCh:       machine.stopCh,
		MaxTicks:     256,
		MaxOutput:    512,
	}

	graph := cortex.New(cfg)
	return graph.Think(prompt, expectedReality)
}

/*
Prompt generates output using O(1) holographic lookup.

The prompt bytes construct a precise GF(257) rotational state.
Because affine transforms are non-commutative, this state exactly
addresses the matching position in the trie. We do a singular map
lookup and stream out the stored suffix.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	out := make(chan byte, 1024)

	go func() {
		defer close(out)

		rot := geometry.IdentityRotation()

		var promptStr []byte
		var pos uint32
		for _, p := range prompt {
			var b byte
			if machine.loader != nil {
				b, _ = machine.loader.ChordToByte(p)
			} else {
				b = data.ChordToByte(&p)
			}

			if machine.sequencer != nil {
				reset, _ := machine.sequencer.Analyze(int(pos), b)
				if reset {
					rot = geometry.IdentityRotation()
					pos = 0
				}
			}

			promptStr = append(promptStr, b)
			rot = rot.Compose(geometry.RotationForByte(b))
			pos++
		}

		filters := machine.substrate.Filters()

		var bestSuffix []byte
		if len(filters) > 0 {
			match, err := kernel.HolographicRecall(filters, machine.primefield.ManifoldsPtr(), rot)
			println("MACHINE PROMPT:", len(promptStr), string(promptStr))
			println("MATCH IDX", match.Index, "SCORE", match.Score)
			if err == nil && match.Index >= 0 && match.Score > 0 {
				bestSuffix = machine.substrate.Entries[match.Index].Readout
			}
		}

		for _, b := range bestSuffix {
			if machine.stopCh != nil {
				select {
				case <-machine.stopCh:
					return
				case out <- b:
				}
			} else {
				out <- b
			}
		}
	}()

	return out
}

func (machine *Machine) Substrate() *geometry.HybridSubstrate {
	return machine.substrate
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
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
