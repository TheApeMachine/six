package vm

import (
	"context"
	"runtime"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"

	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Machine is the top-level orchestrator. It chains RPC calls in sequence:
  1. Prompter  — apply holdout masking, return processed bytes
  2. Tokenizer — tokenize bytes into chords
  3. SpatialIndex.Lookup — fetch paths for those chords
  4. Graph.Prompt — reason over the paths
  5. SpatialIndex.Decode — reconstruct bytes from result chords

Each step passes its capnp result directly to the next call. No conversion,
no helpers — the types already match across the pipeline.
*/
type Machine struct {
	ctx            context.Context
	cancel         context.CancelFunc
	workerPool     *pool.Pool
	broadcastGroup *pool.BroadcastGroup
	booter         *Booter
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{}

	for _, opt := range opts {
		opt(machine)
	}

	machine.workerPool = pool.New(
		machine.ctx,
		1,
		runtime.NumCPU(),
		&pool.Config{},
	)

	machine.broadcastGroup = pool.NewBroadcastGroup(
		"machine",
		5*time.Second,
		128,
	)

	if errnie.Try(
		"machine booting", validate.Require(map[string]any{
			"ctx":            machine.ctx,
			"cancel":         machine.cancel,
			"workerPool":     machine.workerPool,
			"broadcastGroup": machine.broadcastGroup,
		}),
	).Err() != nil {
		console.Error(ErrMachineMissingRequirements)
		return nil
	}

	machine.booter = NewBooter(
		BooterWithContext(machine.ctx),
		BooterWithPool(machine.workerPool),
		BooterWithBroadcast(machine.broadcastGroup),
	)

	return machine
}

/*
Prompt is the main entry point. Results flow directly from one RPC call
to the next — no intermediate materialisation.
*/
func (machine *Machine) Prompt(msg string) ([]byte, error) {
	ctx := machine.ctx

	// Step 1: Prompter — apply holdout masking.
	promptFuture, promptRelease := machine.booter.prompter.Generate(
		ctx, func(params input.Prompter_generate_Params) error {
			return params.SetMsg(msg)
		},
	)
	defer promptRelease()

	promptRes, err := promptFuture.Struct()
	if err != nil {
		return nil, err
	}

	promptBytes, err := promptRes.Data()
	if err != nil {
		return nil, err
	}

	// Step 2: Tokenizer — bytes → chords.
	tokFuture, tokRelease := machine.booter.tok.Generate(
		ctx, func(params tokenizer.Universal_generate_Params) error {
			return params.SetData(promptBytes)
		},
	)
	defer tokRelease()

	tokRes, err := tokFuture.Struct()
	if err != nil {
		return nil, err
	}

	chords, err := tokRes.Chords()
	if err != nil {
		return nil, err
	}

	if chords.Len() == 0 {
		return promptBytes, nil
	}

	// Step 3: SpatialIndex.Lookup — chords → paths.
	// chords is already a data.Chord_List; SetChords takes data.Chord_List. Pass directly.
	lookupFuture, lookupRelease := machine.booter.spatialIndex.Lookup(
		ctx, func(params lsm.SpatialIndex_lookup_Params) error {
			return params.SetChords(chords)
		},
	)
	defer lookupRelease()

	lookupRes, err := lookupFuture.Struct()
	if err != nil {
		return nil, err
	}

	paths, err := lookupRes.Paths()
	if err != nil {
		return nil, err
	}

	metaPaths, err := lookupRes.MetaPaths()
	if err != nil {
		return nil, err
	}

	// Step 4: Graph.Prompt — paths → result paths.
	// paths and metaPaths are capnp.PointerList; SetPaths/SetMetaPaths take capnp.PointerList. Pass directly.
	graphFuture, graphRelease := machine.booter.graph.Prompt(
		ctx, func(params substrate.Graph_prompt_Params) error {
			if err := params.SetPaths(paths); err != nil {
				return err
			}
			return params.SetMetaPaths(metaPaths)
		},
	)
	defer graphRelease()

	graphRes, err := graphFuture.Struct()
	if err != nil {
		return nil, err
	}

	resultPaths, err := graphRes.Result()
	if err != nil {
		return nil, err
	}

	// Step 5: SpatialIndex.Decode — result paths → bytes.
	// resultPaths is List(List(Chord)); Decode takes List(List(Chord)).
	decodeFuture, decodeRelease := machine.booter.spatialIndex.Decode(
		ctx, func(params lsm.SpatialIndex_decode_Params) error {
			return params.SetChords(resultPaths)
		},
	)
	defer decodeRelease()

	decodeRes, err := decodeFuture.Struct()
	if err != nil {
		return nil, err
	}

	seqList, err := decodeRes.Sequences()
	if err != nil {
		return nil, err
	}

	var out []byte
	for i := 0; i < seqList.Len(); i++ {
		seq, err := seqList.At(i)
		if err != nil {
			return nil, err
		}
		out = append(out, seq...)
	}

	return out, nil
}

/*
SetDataset loads a corpus into the machine. It reads all strings from the
dataset, ships the corpus to the Tokenizer via RPC (so the tokenizer has full
dataset context), then drives each string through Prompt to populate the
spatial index before any queries are served.
*/
func (machine *Machine) SetDataset(dataset provider.Dataset) error {
	// Reconstruct corpus strings by grouping RawTokens by SampleID.
	byID := map[uint32][]byte{}

	for tok := range dataset.Generate() {
		byID[tok.SampleID] = append(byID[tok.SampleID], tok.Symbol)
	}

	corpus := make([]string, 0, len(byID))

	for _, bytes := range byID {
		corpus = append(corpus, string(bytes))
	}

	ctx := machine.ctx

	// Send corpus to the Tokenizer via the setDataset RPC.
	setFuture, setRelease := machine.booter.tok.SetDataset(
		ctx, func(params tokenizer.Universal_setDataset_Params) error {
			seg := params.Segment()
			list, err := capnp.NewTextList(seg, int32(len(corpus)))
			if err != nil {
				return err
			}

			for i, s := range corpus {
				if err := list.Set(i, s); err != nil {
					return err
				}
			}

			return params.SetCorpus(list)
		},
	)
	defer setRelease()

	if _, err := setFuture.Struct(); err != nil {
		return err
	}

	// Ingest each corpus string to build the spatial index.
	for _, item := range corpus {
		if _, err := machine.Prompt(item); err != nil {
			return err
		}
	}

	return nil
}

/*
MachineWithContext adds a context to the Machine.
*/
func MachineWithContext(ctx context.Context) machineOpts {
	return func(machine *Machine) {
		machine.ctx, machine.cancel = context.WithCancel(ctx)
	}
}

/*
MachineError is a typed error for Machine failures.
*/
type MachineError string

const (
	ErrMachineMissingRequirements MachineError = "machine: missing requirements"
)

func (machineError MachineError) Error() string {
	return string(machineError)
}
