package vm

import (
	"math/rand"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

type HoldoutType int

const (
	HoldoutLinear HoldoutType = iota
	HoldoutRandom
)

type Loader struct {
	store       store.Store
	tokenizer   *tokenizer.Universal
	holdout     int
	samples     int
	prompt      bool
	holdoutType HoldoutType
	bufs        []data.Chord
}

type loaderOpts func(*Loader)

func NewLoader(opts ...loaderOpts) *Loader {
	loader := &Loader{
		bufs: make([]data.Chord, 0),
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
Generate yields all tokens through a channel for the Machine
to ingest.
*/
func (loader *Loader) Generate() chan data.Chord {
	out := make(chan data.Chord)

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

			if loader.prompt {
				if token.Pos == 0 {
					out <-<-loader.flushPrompt()
					loader.bufs = loader.bufs[:0]
				}

				loader.bufs = append(loader.bufs, token.Chord)
			} else {
				loader.store.Insert(token.TokenID, token.Chord)
				out <- token.Chord
			}
		}
	}()

	return out
}

/*
flushPrompt flushes the current buffer to the store.
*/
func (loader *Loader) flushPrompt() chan data.Chord {
	out := make(chan data.Chord)

	go func() {
		defer close(out)

		switch loader.holdoutType {
		case HoldoutLinear:
			start := int(float64(len(loader.bufs)) * float64(loader.holdout) / 100.0)
			for _, chord := range loader.bufs[start:] {
				out <- chord
			}
		case HoldoutRandom:
			for _, chord := range loader.randomHoldout(loader.bufs) {
				out <- chord
			}
		}

		out <- data.Chord{}
	}()

	return out
}

/*
randomHoldout removes N% of tokens from the buffer randomly.
*/
func (loader *Loader) randomHoldout(buf []data.Chord) []data.Chord {
	masked := make([]data.Chord, 0)
	
	for _, chord := range buf {
		if rand.Intn(100) >= loader.holdout {
			masked = append(masked, chord)
		}
	}

	return masked
}	

func (loader *Loader) validate(token tokenizer.Token) bool {
	return token.Chord.ActiveCount() > 0
}

func (loader *Loader) Holdout(n int, t HoldoutType) {
	loader.holdout = n
	loader.holdoutType = t
	loader.prompt = true
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

func LoaderWithTokenizer(tokenizer *tokenizer.Universal) loaderOpts {
	return func(loader *Loader) {
		loader.tokenizer = tokenizer
	}
}

type LoaderError string

const (
	LoaderErrDecode LoaderError = "failed to decode chord"
	LoaderErrEmptyBuffer LoaderError = "empty buffer"
	LoaderErrEmptyPrompt LoaderError = "empty prompt"
	LoaderErrInvalidToken LoaderError = "invalid token"
)

func (e LoaderError) Error() string {
	return string(e)
}
