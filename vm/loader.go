package vm

import (
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

type Loader struct {
	store     store.Store
	tokenizer *tokenizer.Universal
	holdout   int
	samples   int
	prompt    bool
	bufs      map[uint32][]tokenizer.Token
}

type loaderOpts func(*Loader)

func NewLoader(opts ...loaderOpts) *Loader {
	loader := &Loader{
		bufs: make(map[uint32][]tokenizer.Token),
	}

	for _, opt := range opts {
		opt(loader)
	}

	return loader
}

/*
Generate yields all tokens through a channel for the Machine
to ingest.
*/
func (loader *Loader) Generate() chan tokenizer.Token {
	out := make(chan tokenizer.Token)

	go func() {
		defer close(out)

		for token := range loader.tokenizer.Generate() {
			if loader.prompt {
				loader.bufs[token.SampleID] = append(loader.bufs[token.SampleID], token)
			} else {
				loader.store.Insert(token.TokenID, token.Chord)
				out <- token
			}
		}

		if loader.prompt {
			// Maintain previous Generate() fallback behaviour by flattening bufs
			// Not guaranteed sorted by sample ID, but keeps interface intact
			for _, buf := range loader.bufs {
				start := int(float64(len(buf)) * float64(loader.holdout) / 100.0)
				for _, t := range buf[start:] {
					out <- tokenizer.Token{
                        Chord: t.Chord,
                        // Not a full token, but serves backwards compatibility
                    }
				}
			}
		}
	}()

	return out
}

/*
Prompts yields holdout samples as discrete slices for independent generation.
*/
func (loader *Loader) Prompts() chan []tokenizer.Token {
	out := make(chan []tokenizer.Token)

	go func() {
		defer close(out)
		
		// Fill bufs if empty
		if len(loader.bufs) == 0 {
			for _ = range loader.Generate() { }
		}

		emitted := 0
		for _, buf := range loader.bufs {
			if loader.samples > 0 && emitted >= loader.samples {
				break
			}
			start := int(float64(len(buf)) * float64(loader.holdout) / 100.0)
			if start < len(buf) && len(buf) > 0 {
				out <- buf[start:]
				emitted++
			}
		}
	}()

	return out
}

func (loader *Loader) Holdout(n int, samples int) {
	loader.holdout = n
	loader.samples = samples
	loader.prompt = true
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
	LoaderErrDecode      LoaderError = "failed to decode chord"
)

func (e LoaderError) Error() string {
	return string(e)
}