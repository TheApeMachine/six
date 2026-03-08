package store

import "github.com/theapemachine/six/data"

/*
Store is the interface for the spatial index.
Insert takes a Morton key and a Chord.
Lookup does key → chord (binary search across LSM levels).
ReverseLookup does chord → key (from the reverse index).
*/
type Store interface {
	Insert(key uint64, value data.Chord)
	Lookup(key uint64) data.Chord
	ReverseLookup(chord data.Chord) uint64
	SleepCycle(stopCh <-chan struct{})
}
