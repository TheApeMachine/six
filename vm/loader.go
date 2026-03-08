package vm

import (
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
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
	tokenizer  *tokenizer.Universal
	coder      *tokenizer.MortonCoder
}

type loaderOpts func(*Loader)

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

func (loader *Loader) Tokenizer() *tokenizer.Universal {
	return loader.tokenizer
}

/*
ChordToByte resolves a chord to its byte identity via Lookup + Morton Decode.
*/
func (loader *Loader) ChordToByte(chord data.Chord) (byte, bool) {
	if loader.store == nil {
		return data.ChordToByte(&chord), true
	}
	keys := loader.Lookup([]data.Chord{chord})
	if len(keys) == 0 {
		return data.ChordToByte(&chord), false
	}
	_, symbol := loader.coder.Decode(keys[0])
	return symbol, true
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
Generate tokenizes the dataset and ingests every token into the Store
and PrimeField. Returns a channel of LoadResults for downstream consumers
(e.g. Machine.Start drains this to completion).
*/
func (loader *Loader) Generate() chan LoadResult {
	out := make(chan LoadResult, 1024)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if !loader.validate(token) && !token.IsBoundary {
				console.Error(LoaderErrInvalidToken,
					"tokenID", token.TokenID,
					"activeCount", token.Chord.ActiveCount(),
				)
				return
			}

			var byteVal byte
			_, byteVal = loader.coder.Decode(token.TokenID)

			if token.Chord.ActiveCount() > 0 {
				loader.store.Insert(token.TokenID, token.Chord)

				if loader.primefield != nil {
					loader.primefield.Insert(byteVal, token.Pos, token.Chord, token.Events)
				}
			}

			out <- LoadResult{
				Chord:      token.Chord,
				Symbol:     byteVal,
				Pos:        token.Pos,
				SampleID:   token.SampleID,
				IsBoundary: token.IsBoundary,
				Events:     token.Events,
			}
		}
	}()

	return out
}

func (loader *Loader) validate(token tokenizer.Token) bool {
	return token.Chord.ActiveCount() > 0
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

func LoaderWithStore(store store.Store) loaderOpts {
	return func(loader *Loader) {
		loader.store = store
	}
}

func LoaderWithPrimeField(pf *store.PrimeField) loaderOpts {
	return func(loader *Loader) {
		loader.primefield = pf
	}
}

func LoaderWithTokenizer(tokenizer *tokenizer.Universal) loaderOpts {
	return func(loader *Loader) {
		loader.tokenizer = tokenizer
	}
}

type LoaderError string

const (
	LoaderErrDecode       LoaderError = "failed to decode chord"
	LoaderErrEmptyBuffer  LoaderError = "empty buffer"
	LoaderErrEmptyPrompt  LoaderError = "empty prompt"
	LoaderErrInvalidToken LoaderError = "invalid token"
)

func (e LoaderError) Error() string {
	return string(e)
}
