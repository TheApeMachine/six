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
	primefield  *store.PrimeField
	tokenizer   *tokenizer.Universal
	holdout     int
	samples     int
	prompt      bool
	holdoutType HoldoutType
	bufs        []tokenizer.Token
}

type loaderOpts func(*Loader)

func NewLoader(opts ...loaderOpts) *Loader {
	loader := &Loader{
		bufs: make([]tokenizer.Token, 0),
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
Generate yields prompt chords or ingest chords, depending on loader mode.
*/
func (loader *Loader) Generate() chan data.Chord {
	out := make(chan data.Chord)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if token.Pos == 0 && len(loader.bufs) > 0 {
				if loader.prompt {
					for c := range loader.flushPrompt() {
						out <- c
					}
					loader.bufs = loader.bufs[:0]
				}
			}

			if !loader.validate(token) {
				console.Error(LoaderErrInvalidToken,
					"tokenID", token.TokenID,
					"activeCount", token.Chord.ActiveCount(),
				)
				return
			}

			if loader.prompt {
				loader.bufs = append(loader.bufs, token)
				continue
			}

			loader.store.Insert(token.TokenID, token.Chord)
			if loader.primefield != nil {
				_, _, byteVal := tokenizer.NewMortonCoder().Decode(token.TokenID)
				loader.primefield.Insert(byteVal, token.Pos, token.Chord, token.Events)
			}
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
			for _, token := range loader.bufs[:start] {
				out <- token.Chord
			}
			for _, token := range loader.bufs[start:] {
				loader.store.Insert(token.TokenID, token.Chord)
				if loader.primefield != nil {
					_, _, byteVal := tokenizer.NewMortonCoder().Decode(token.TokenID)
					loader.primefield.Insert(byteVal, token.Pos, token.Chord, token.Events)
				}
			}
		case HoldoutRandom:
			for _, token := range loader.randomHoldout(loader.bufs) {
				out <- token.Chord
			}
		}

		out <- data.Chord{}
	}()

	return out
}

/*
randomHoldout removes N% of tokens from the buffer randomly and pushes the rest into store.
*/
func (loader *Loader) randomHoldout(buf []tokenizer.Token) []tokenizer.Token {
	masked := make([]tokenizer.Token, 0)

	for _, token := range buf {
		if rand.Intn(100) >= loader.holdout {
			masked = append(masked, token)
		} else {
			loader.store.Insert(token.TokenID, token.Chord)
			if loader.primefield != nil {
				_, _, byteVal := tokenizer.NewMortonCoder().Decode(token.TokenID)
				loader.primefield.Insert(byteVal, token.Pos, token.Chord, token.Events)
			}
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
