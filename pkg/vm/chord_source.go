package vm

import "github.com/theapemachine/six/pkg/data"

/*
ChordSource produces chord batches for Machine.Prompt. Each call to Next()
advances to the next sample; Chords() returns the chords for that sample.
process.Prompt already satisfies this interface.
*/
type ChordSource interface {
	Next() bool
	Chords() []data.Chord
	Error() error
}
