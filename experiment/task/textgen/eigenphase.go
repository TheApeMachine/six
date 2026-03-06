package textgen

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

// buildEigenMode builds an EigenMode topology from a text corpus.
func buildEigenMode(corpus []string) *geometry.EigenMode {
	var fullCorpus []byte
	for _, fn := range corpus {
		fullCorpus = append(fullCorpus, []byte(fn)...)
		fullCorpus = append(fullCorpus, ' ')
	}
	chords := textToChords(string(fullCorpus))
	ei := geometry.NewEigenMode()
	if err := ei.BuildMultiScaleCooccurrence(chords); err != nil {
		return geometry.NewEigenMode()
	}
	return ei
}

// textToChords tokenizes a raw string into atomic topological chords.
func textToChords(text string) []data.Chord {
	chords := make([]data.Chord, len(text))
	for i, b := range []byte(text) {
		chords[i] = data.BaseChord(b)
	}
	return chords
}

// IsGeometricallyClosed wraps the native geometry validation for raw text.
func IsGeometricallyClosed(ei *geometry.EigenMode, code string, anchorPhase float64) bool {
	return ei.IsGeometricallyClosed(textToChords(code), anchorPhase)
}

// weightedCircularMean wraps the native Toroidal weighting function over chords.
func weightedCircularMean(ei *geometry.EigenMode, text string) (float64, float64) {
	return ei.WeightedCircularMean(textToChords(text))
}
