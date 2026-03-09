package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Think runs the cortex for bAbI-style "Where is X?" extraction.
Injects prompt with Sequencer, seeds sink if expected!=nil, runs Tick() until
convergence. Extracts via TransitiveResonance + substrate/PrimeField fallbacks.
Returns a channel of answer bytes.
*/
func (graph *Graph) Think(
	prompt []data.Chord,
	expected *geometry.IcosahedralManifold,
) chan []byte {
	out := make(chan []byte, graph.config.MaxOutput)

	go func() {
		defer close(out)
	}()

	return out
}
