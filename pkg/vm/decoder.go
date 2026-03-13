package vm

import "github.com/theapemachine/six/pkg/data"

/*
Decoder converts result chords back to original byte sequences
by reversing the LSM encoding (extracting byte values from entry keys).
*/
type Decoder interface {
	Decode(chords []data.Chord) [][]byte
}
