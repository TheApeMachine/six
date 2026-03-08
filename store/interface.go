package store

import "github.com/theapemachine/six/data"

/*
Store is the interface for chord-key storage with LSM-style indexing.
Insert(key, value), Lookup(key)→chord, ReverseLookup(chord)→key.
SleepCycle runs background consolidation (see store/sleep.go) until stopCh closes.
*/
type Store interface {
	Insert(key uint64, value data.Chord)
	Lookup(key uint64) data.Chord
	ReverseLookup(chord data.Chord) uint64
	SleepCycle(stopCh <-chan struct{})
}
