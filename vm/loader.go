package vm

import (
	"fmt"
	"math/rand"
	"sync"

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
	store      store.Store
	primefield *store.PrimeField
	substrate  *geometry.HybridSubstrate
	eigenmode  *geometry.EigenMode
	tokenizer  *tokenizer.Universal
	coder      *tokenizer.MortonCoder
	pool       *pool.Pool
}

type loaderOpts func(*Loader)

/*
NewLoader creates a Loader. Use LoaderWithStore, LoaderWithPrimeField, LoaderWithTokenizer.
*/
func NewLoader(opts ...loaderOpts) *Loader {
	loader := &Loader{
		coder: tokenizer.NewMortonCoder(),
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
	var sequence []data.Chord

	idx := 0
	wg := sync.WaitGroup{}

	for token := range loader.tokenizer.Generate() {
		if token.Chord.ActiveCount() > 0 {
			loader.store.Insert(token.TokenID, token.Chord)

			loader.pool.Schedule(fmt.Sprintf("loader-%d", idx), func() (any, error) {
				defer wg.Done()

				if token.IsBoundary {
					loader.buildPhaseDial(sequence)
				}

				if token.Chord.ActiveCount() > 0 {
					sequence = append(sequence, token.Chord)
				}

				return nil, nil
			})

			// Build EigenModes from the ingested manifold topology.
			loader.primefield.BuildEigenModes()
			loader.eigenmode = loader.primefield.EigenMode()

			// Wire the Sequencer with the trained EigenMode.
			if loader.Tokenizer() != nil && loader.Tokenizer().Sequencer() != nil {
				seq := loader.Tokenizer().Sequencer()
				seq.SetEigenMode(loader.eigenmode)
			}

			if loader.primefield != nil {
				loader.primefield.Insert(token.Chord)
			}
		}
	}

	return nil
}

func (loader *Loader) buildPhaseDial(
	sequence []data.Chord,
) {
	// Write suffix entries for the completed sample.
	rot := geometry.IdentityRotation()

	for i := 0; i < len(sequence); i++ {
		var ptr data.Chord

		for range 5 {
			ptr.Set(rand.Intn(257))
		}

		loader.primefield.StorePointer(rot, ptr)

		suffix := make([]data.Chord, len(sequence)-i)
		copy(suffix, sequence[i:])

		loader.substrate.Add(
			ptr, geometry.NewPhaseDial(), suffix,
		)

		if i < len(sequence) {
			rot = rot.Compose(
				geometry.RotationForChord(sequence[i]),
			)
		}
	}

	dial := geometry.NewPhaseDial()
	dial = dial.EncodeFromChords(sequence)

	loader.substrate.Add(
		data.Chord{},
		dial,
		sequence,
	)

	sequence = sequence[:0]
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
