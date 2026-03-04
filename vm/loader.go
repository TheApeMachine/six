package vm

import (
	"github.com/theapemachine/six/numeric"
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
	bufs        map[uint32][]tokenizer.Token
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
				switch loader.holdoutType {
				case HoldoutLinear:
					start := int(float64(len(buf)) * float64(loader.holdout) / 100.0)
					for _, t := range buf[start:] {
						out <- tokenizer.Token{
							TokenID:  t.TokenID,
							SampleID: t.SampleID,
							Chord:    t.Chord,
						}
					}
				case HoldoutRandom:
					// For random masking, holdout N% of tokens randomly
					maskCount := int(float64(len(buf)) * float64(loader.holdout) / 100.0)
					// using simple deterministic hash of token ID for stability instead of math/rand
					masked := 0
					for _, t := range buf {
						if masked < maskCount && (t.TokenID%3) == 0 {
							out <- tokenizer.Token{
								TokenID:  t.TokenID,
								SampleID: t.SampleID,
								Chord:    t.Chord,
							}
							masked++
						}
					}
				}
			}
		}
	}()

	return out
}

type PromptContext struct {
	Tokens []tokenizer.Token
	Target []tokenizer.Token
}

/*
Prompts yields holdout samples as discrete slices for independent generation.
*/
func (loader *Loader) Prompts() chan PromptContext {
	out := make(chan PromptContext)

	go func() {
		defer close(out)

		// Fill bufs if empty
		if len(loader.bufs) == 0 {
			for _ = range loader.Generate() {
			}
		}

		emitted := 0
		for _, buf := range loader.bufs {
			if loader.samples > 0 && emitted >= loader.samples {
				break
			}

			var linear []tokenizer.Token
			for _, t := range buf {
				if t.Scale == numeric.FibWindows[0] {
					linear = append(linear, t)
				}
			}

			switch loader.holdoutType {
			case HoldoutLinear:
				start := int(float64(len(linear)) * float64(loader.holdout) / 100.0)
				if start > 0 && start <= len(linear) {
					out <- PromptContext{
						Tokens: linear[:start],
						Target: linear,
					}
					emitted++
				}
			case HoldoutRandom:
				// Random holdout means the prompt contains holes!
				// We keep the first 100-N% of tokens by filtering
				maskCount := int(float64(len(linear)) * float64(loader.holdout) / 100.0)
				var prompt []tokenizer.Token
				masked := 0
				for _, t := range linear {
					// Poking holes deterministically
					if masked < maskCount && (t.TokenID%3) == 0 {
						masked++
						continue
					}
					prompt = append(prompt, t)
				}
				if len(prompt) > 0 {
					out <- PromptContext{
						Tokens: prompt,
						Target: linear,
					}
					emitted++
				}
			}
		}
	}()

	return out
}

func (loader *Loader) Holdout(n int, samples int, t HoldoutType) {
	loader.holdout = n
	loader.samples = samples
	loader.holdoutType = t
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
	LoaderErrDecode LoaderError = "failed to decode chord"
)

func (e LoaderError) Error() string {
	return string(e)
}
