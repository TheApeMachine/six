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
GenerateTokens yields tokenizer tokens for the ingest path while preserving
exact geometric metadata alongside each chord.
*/
func (loader *Loader) GenerateTokens() chan tokenizer.Token {
	out := make(chan tokenizer.Token)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if token.Boundary {
				if !loader.prompt {
					out <- token
				}
				continue
			}

			if !loader.validate(token) {
				console.Error(LoaderErrInvalidToken,
					"tokenID", token.TokenID,
					"activeCount", token.Chord.ActiveCount(),
				)
				return
			}

			loader.store.Insert(token.TokenID, token.Chord)
			out <- token
		}
	}()

	return out
}

/*
Generate yields prompt chords or ingest chords, depending on loader mode.
*/
func (loader *Loader) Generate() chan data.Chord {
	out := make(chan data.Chord)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if token.Boundary {
				if loader.prompt {
					for c := range loader.flushPrompt() {
						out <- c
					}
					loader.bufs = loader.bufs[:0]
				}
				continue
			}

			if !loader.validate(token) {
				console.Error(LoaderErrInvalidToken,
					"tokenID", token.TokenID,
					"activeCount", token.Chord.ActiveCount(),
				)
				return
			}

			if loader.prompt {
				loader.bufs = append(loader.bufs, token.Chord)
				continue
			}

			loader.store.Insert(token.TokenID, token.Chord)
			out <- token.Chord
		}

		if loader.prompt && len(loader.bufs) > 0 {
			for c := range loader.flushPrompt() {
				out <- c
			}
			loader.bufs = loader.bufs[:0]
		}
	}()

	return out
}

/*
flushPrompt flushes the current buffer as a prompt sequence.
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
	LoaderErrDecode       LoaderError = "failed to decode chord"
	LoaderErrEmptyBuffer  LoaderError = "empty buffer"
	LoaderErrEmptyPrompt  LoaderError = "empty prompt"
	LoaderErrInvalidToken LoaderError = "invalid token"
)

func (e LoaderError) Error() string {
	return string(e)
}
