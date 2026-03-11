package vm

import (
	"fmt"
	"sync"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

/*
Loader ingests a tokenized byte stream into the Store (LSM spatial index)
and PrimeField (dense manifold array). Its only job is ingestion — prompt
construction and holdout logic live in tokenizer.Prompt.
*/
type Loader struct {
	store            store.Store
	primefield       *store.PrimeField
	substrate        *geometry.HybridSubstrate
	reverseSubstrate *geometry.HybridSubstrate
	rotIndex         *geometry.RotationIndex
	eigenmode        *geometry.EigenMode
	tokenizer        *tokenizer.Universal
	coder            *tokenizer.MortonCoder
	pool             *pool.Pool
	sequences        [][]data.Chord
}

type loaderOpts func(*Loader)

/*
NewLoader creates a Loader. Use LoaderWithStore, LoaderWithPrimeField, LoaderWithTokenizer.
*/
func NewLoader(opts ...loaderOpts) *Loader {
	loader := &Loader{
		coder:            tokenizer.NewMortonCoder(),
		substrate:        geometry.NewHybridSubstrate(),
		reverseSubstrate: geometry.NewHybridSubstrate(),
		rotIndex:         geometry.NewRotationIndex(),
		sequences:        make([][]data.Chord, 0, 64),
	}

	for _, opt := range opts {
		opt(loader)
	}

	return loader
}

/*
Store returns the underlying store abstraction for direct access.
*/
func (loader *Loader) Store() store.Store {
	return loader.store
}

/*
Tokenizer returns the Universal tokenizer. Nil if not set.
*/
func (loader *Loader) Tokenizer() *tokenizer.Universal {
	return loader.tokenizer
}

/*
Substrate returns the current geometric substrate.
*/
func (loader *Loader) Substrate() *geometry.HybridSubstrate {
	return loader.substrate
}

/*
ReverseSubstrate returns the reverse-direction substrate used for right-boundary
queries and non-autoregressive infill.
*/
func (loader *Loader) ReverseSubstrate() *geometry.HybridSubstrate {
	return loader.reverseSubstrate
}

/*
RotationIndex returns the rotation-keyed prefix memory built during loading.
*/
func (loader *Loader) RotationIndex() *geometry.RotationIndex {
	return loader.rotIndex
}

/*
Sequences returns the lexical base-chord corpus sequences captured during
loading.
*/
func (loader *Loader) Sequences() [][]data.Chord {
	out := make([][]data.Chord, len(loader.sequences))

	for idx := range loader.sequences {
		out[idx] = append([]data.Chord(nil), loader.sequences[idx]...)
	}

	return out
}

/*
LoadResult bundles the ingested chord with metadata from the sequencer,
allowing the Machine to identify structural boundaries during Start().
*/
type LoadResult struct {
	Chord      data.Chord
	Symbol     byte
	Pos        uint32
	SampleID   uint32
	IsBoundary bool
	Events     []int
}

/*
Start the loader and provide everything with the data it needs.
*/
func (loader *Loader) Start() error {
	return loader.generate()
}

/*
Generate tokenizes the dataset and ingests every token into the Store
and PrimeField. Returns a channel of LoadResults for downstream consumers
(e.g. Machine.Start drains this to completion).
*/
func (loader *Loader) generate() error {
	var (
		sequence     []data.Chord
		lexical      []data.Chord
		sampleLex    []data.Chord
		rotSampleID  int
	)

	loader.sequences = loader.sequences[:0]
	if loader.substrate != nil {
		loader.substrate.Entries = loader.substrate.Entries[:0]
	}
	if loader.reverseSubstrate != nil {
		loader.reverseSubstrate.Entries = loader.reverseSubstrate.Entries[:0]
	}

	idx := 0
	wg := sync.WaitGroup{}

	flush := func() {
		if len(sequence) == 0 {
			return
		}

		seqCopy := append([]data.Chord(nil), sequence...)
		lexCopy := append([]data.Chord(nil), lexical...)
		loader.sequences = append(loader.sequences, append([]data.Chord(nil), lexCopy...))

		sampleID := idx
		if loader.pool != nil {
			wg.Add(1)
			loader.pool.Schedule(fmt.Sprintf("loader-%d", idx), func() (any, error) {
				defer wg.Done()
				loader.buildDirectionalSubstrates(seqCopy, lexCopy, sampleID)
				return nil, nil
			})
		} else {
			loader.buildDirectionalSubstrates(seqCopy, lexCopy, sampleID)
		}

		sequence = sequence[:0]
		lexical = lexical[:0]
		idx++
	}

	flushRotationSample := func() {
		if len(sampleLex) == 0 {
			return
		}

		loader.buildRotationIndex(append([]data.Chord(nil), sampleLex...), rotSampleID)
		sampleLex = sampleLex[:0]
		rotSampleID++
	}

	for token := range loader.tokenizer.Generate() {
		effective := token.EffectiveChord()

		if token.Chord.IsStopChord() {
			flush()
			flushRotationSample()

			if loader.primefield != nil {
				_ = loader.primefield.BuildEigenModes()
				loader.eigenmode = loader.primefield.EigenMode()
			}

			if loader.Tokenizer() != nil && loader.Tokenizer().Sequencer() != nil && loader.eigenmode != nil {
				seq := loader.Tokenizer().Sequencer()
				seq.SetEigenMode(loader.eigenmode)
			}
		} else if token.Chord.IsSplitChord() {
			flush()
		}

		if token.Chord.ActiveCount() == 0 {
			continue
		}

		if loader.store != nil && !token.Chord.IsStreamMarker() {
			loader.store.Insert(token.TokenID, token.Chord)
		}

		if effective.ActiveCount() > 0 {
			sequence = append(sequence, effective)
			lexical = append(lexical, token.Chord)
		}

		if !token.Chord.IsStreamMarker() && token.Chord.ActiveCount() > 0 {
			sampleLex = append(sampleLex, token.Chord)
		}

		if loader.primefield != nil && effective.ActiveCount() > 0 {
			loader.primefield.Insert(effective)
		}
	}

	flush()
	flushRotationSample()
	wg.Wait()
	return nil
}

func (loader *Loader) buildDirectionalSubstrates(sequence, lexical []data.Chord, sampleID int) {
	loader.buildPhaseDial(loader.substrate, sequence, lexical, sampleID, false, true)

	reverseSequence := reverseChords(sequence)
	reverseLexical := reverseChords(lexical)
	loader.buildPhaseDial(loader.reverseSubstrate, reverseSequence, reverseLexical, sampleID, true, false)
}

func (loader *Loader) buildPhaseDial(
	substrate *geometry.HybridSubstrate,
	sequence []data.Chord,
	lexical []data.Chord,
	sampleID int,
	reverse bool,
	storePointers bool,
) {
	if substrate == nil || len(sequence) == 0 {
		return
	}

	if len(lexical) != len(sequence) {
		lexical = append([]data.Chord(nil), sequence...)
	}

	rot := geometry.IdentityRotation()
	var activePrefix data.Chord
	runningDial := geometry.NewPhaseDial()

	for idx := 0; idx < len(sequence); idx++ {
		if idx > 0 {
			activePrefix = data.ChordOR(&activePrefix, &sequence[idx-1])
		}

		if storePointers && loader.primefield != nil {
			loader.primefield.StorePointer(rot, activePrefix)
		}

		suffix := append([]data.Chord(nil), sequence[idx:]...)
		lexSuffix := append([]data.Chord(nil), lexical[idx:]...)

		var dial geometry.PhaseDial
		if idx > 0 {
			dial = runningDial.CopyAndNormalize()
		} else {
			dial = geometry.NewPhaseDial()
		}

		substrate.AddIndexed(activePrefix, dial, suffix, lexSuffix, sampleID, idx, reverse)

		rot = rot.Compose(geometry.RotationForChord(sequence[idx]))
		runningDial.AddChordPhase(sequence[idx], idx)
	}

	dial := runningDial.CopyAndNormalize()
	substrate.AddIndexed(
		data.Chord{},
		dial,
		append([]data.Chord(nil), sequence...),
		append([]data.Chord(nil), lexical...),
		sampleID,
		0,
		reverse,
	)
}

func reverseChords(sequence []data.Chord) []data.Chord {
	if len(sequence) == 0 {
		return nil
	}

	out := make([]data.Chord, len(sequence))

	for idx := range sequence {
		out[len(sequence)-1-idx] = sequence[idx]
	}

	return out
}

/*
buildRotationIndex populates the rotation-keyed memory from a base chord
sequence. At every position the accumulated prefix rotation is stored so
that exact prefix recall is O(1).
*/
func (loader *Loader) buildRotationIndex(baseChords []data.Chord, sampleID int) {
	if loader.rotIndex == nil || len(baseChords) == 0 {
		return
	}

	rot := geometry.IdentityRotation()

	for pos, chord := range baseChords {
		rot = rot.Compose(geometry.RotationForChord(chord))

		continuation := append([]data.Chord(nil), baseChords[pos+1:]...)

		loader.rotIndex.Insert(rot, geometry.RotationEntry{
			SampleID:     sampleID,
			Position:     pos,
			Chord:        chord,
			Continuation: continuation,
		})
	}

	console.Trace("loader.rotation_index_built",
		"sample", sampleID,
		"seq_len", len(baseChords),
		"index_size", loader.rotIndex.Size(),
	)
}

func (loader *Loader) Lookup(chords []data.Chord) []uint64 {
	var out []uint64

	for _, chord := range chords {
		if key := loader.store.ReverseLookup(chord); key > 0 {
			out = append(out, key)
		}
	}

	return out
}

/*
LoaderWithStore sets the LSM spatial index for chord-key storage.
*/
func LoaderWithStore(store store.Store) loaderOpts {
	return func(loader *Loader) {
		loader.store = store
	}
}

/*
LoaderWithPrimeField sets the PrimeField for manifold ingestion during Generate.
*/
func LoaderWithPrimeField(pf *store.PrimeField) loaderOpts {
	return func(loader *Loader) {
		loader.primefield = pf
	}
}

/*
LoaderWithTokenizer sets the Universal tokenizer. Required for Generate.
*/
func LoaderWithTokenizer(tokenizer *tokenizer.Universal) loaderOpts {
	return func(loader *Loader) {
		loader.tokenizer = tokenizer
	}
}

/*
LoaderWithPool sets the pool for parallel processing.
*/
func LoaderWithPool(pool *pool.Pool) loaderOpts {
	return func(loader *Loader) {
		loader.pool = pool
	}
}

/*
LoaderError is a typed error for Loader failures.
*/
type LoaderError string

const (
	LoaderErrDecode       LoaderError = "failed to decode chord"
	LoaderErrEmptyBuffer  LoaderError = "empty buffer"
	LoaderErrEmptyPrompt  LoaderError = "empty prompt"
	LoaderErrInvalidToken LoaderError = "invalid token"
)

/*
Error implements the error interface for LoaderError.
*/
func (e LoaderError) Error() string {
	return string(e)
}
