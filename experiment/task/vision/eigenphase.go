package vision

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/tokenizer"
)

// buildEigenMode builds an EigenMode topology from an image corpus.
func buildEigenMode(corpus [][]byte) *geometry.EigenMode {
	var fullCorpus []byte
	for _, img := range corpus {
		fullCorpus = append(fullCorpus, img...)
	}
	chords := bytesToChords(fullCorpus)
	ei := geometry.NewEigenMode()
	if err := ei.BuildMultiScaleCooccurrence(chords); err != nil {
		return geometry.NewEigenMode()
	}
	return ei
}

// bytesToChords tokenizes a byte array into atomic topological chords.
func bytesToChords(b []byte) []data.Chord {
	chords := make([]data.Chord, len(b))
	for i, v := range b {
		chords[i] = tokenizer.BaseChord(v)
	}
	return chords
}

// IsGeometricallyClosed wraps the native geometry validation for raw bytes.
func IsGeometricallyClosed(ei *geometry.EigenMode, tokens []byte, anchorPhase float64) bool {
	return ei.IsGeometricallyClosed(bytesToChords(tokens), anchorPhase)
}

// weightedCircularMean wraps the native Toroidal weighting function over chords.
func weightedCircularMean(ei *geometry.EigenMode, tokens []byte) (float64, float64) {
	return ei.WeightedCircularMean(bytesToChords(tokens))
}
