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
Generate tokenizes the dataset and ingests every token into the Store
and PrimeField. Returns a channel of chords for downstream consumers
(e.g. Machine.Start drains this to completion).
*/
func (loader *Loader) Generate() chan data.Chord {
	out := make(chan data.Chord, 1024)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if !loader.validate(token) {
				console.Error(LoaderErrInvalidToken,
					"tokenID", token.TokenID,
					"activeCount", token.Chord.ActiveCount(),
				)
				return
			}

			loader.store.Insert(token.TokenID, token.Chord)
			if loader.primefield != nil {
				_, _, byteVal := loader.coder.Decode(token.TokenID)
				loader.primefield.Insert(byteVal, token.Pos, token.Chord, token.Events)
			}
			out <- token.Chord
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
