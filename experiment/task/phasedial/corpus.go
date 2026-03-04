package phasedial

import (
	"github.com/theapemachine/six/provider"
)

// Aphorisms is the standard 24-item corpus for PhaseDial validation.
var Aphorisms = []string{
	"Democracy requires individual sacrifice.",
	"Freedom is the right to tell people what they do not want to hear.",
	"Liberty means responsibility. That is why most men dread it.",
	"The price of freedom is eternal vigilance.",
	"True freedom is to be able to use any power for good.",
	"Responsibility is the price of freedom.",
	"Order is the sanity of the mind, the health of the body.",
	"Discipline is the bridge between goals and accomplishment.",
	"Authority flowing from the people is the only source of enduring power.",
	"Good order is the foundation of all things.",
	"A state which does not change is a state without the means of its conservation.",
	"Stability is the foundation of progress.",
	"The only true wisdom is in knowing you know nothing.",
	"Knowledge is power.",
	"Truth is stranger than fiction.",
	"To know oneself is the beginning of wisdom.",
	"Discipline is the pulse of the soul.",
	"A rolling stone gathers no moss.",
	"The early bird catches the worm.",
	"Haste makes waste.",
	"Silence is the master of matters.",
	"The way of nature is the way of ease.",
	"Nature does not hurry, yet everything is accomplished.",
}

// AphorismDataset implements provider.Dataset by streaming aphorism bytes.
type AphorismDataset struct {
	texts []string
}

// NewAphorismDataset returns a dataset that yields the given texts as byte streams.
func NewAphorismDataset(texts []string) *AphorismDataset {
	return &AphorismDataset{texts: texts}
}

// Generate streams (SampleID, Symbol, Pos) for each byte of each text.
func (d *AphorismDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)
	go func() {
		defer close(out)
		for sampleID, text := range d.texts {
			for pos, b := range []byte(text) {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   b,
					Pos:      uint32(pos),
				}
			}
		}
	}()
	return out
}
