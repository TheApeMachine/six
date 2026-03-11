package geometry

import (
	"fmt"
	"sync"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
)

/*
RotationEntry binds a GFRotation prefix fingerprint to the sample and position
it was observed at. The Continuation holds everything from the next position
to the sample's end.
*/
type RotationEntry struct {
	SampleID     int
	Position     int
	Chord        data.Chord
	Continuation []data.Chord
}

/*
RotationIndex is a rotation-keyed memory. Each accumulated prefix rotation maps
to the entries that produced it. Prefix recall is O(1): compute the rotation from
the query prefix, look it up, read the continuation.
*/
type RotationIndex struct {
	mu      sync.RWMutex
	entries map[GFRotation][]RotationEntry
}

/*
NewRotationIndex allocates an empty rotation index.
*/
func NewRotationIndex() *RotationIndex {
	return &RotationIndex{
		entries: make(map[GFRotation][]RotationEntry),
	}
}

/*
Insert records that at the given accumulated rotation, byte chord was observed
at position within sampleID. Continuation is the rest of the sample from the
next position onward.
*/
func (idx *RotationIndex) Insert(rot GFRotation, entry RotationEntry) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.entries[rot] = append(idx.entries[rot], entry)

	console.Trace("rotindex.insert",
		"A", rot.A,
		"B", rot.B,
		"sample", entry.SampleID,
		"pos", entry.Position,
		"cont_len", len(entry.Continuation),
	)
}

/*
Lookup returns all entries whose accumulated prefix rotation matches exactly.
Returns nil if no match.
*/
func (idx *RotationIndex) Lookup(rot GFRotation) []RotationEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.entries[rot]
}

/*
BestContinuation finds the entry with the longest continuation matching the
query rotation, and returns that continuation. This is the primary recall path.
*/
func (idx *RotationIndex) BestContinuation(rot GFRotation) []data.Chord {
	entries := idx.Lookup(rot)

	if len(entries) == 0 {
		console.Trace("rotindex.miss",
			"A", rot.A,
			"B", rot.B,
			"index_size", idx.Size(),
		)
		return nil
	}

	best := entries[0]

	for _, entry := range entries[1:] {
		if len(entry.Continuation) > len(best.Continuation) {
			best = entry
		}
	}

	preview := ""
	for i, chord := range best.Continuation {
		if i >= 8 {
			break
		}
		preview += fmt.Sprintf("%d ", chord.BestByte())
	}

	console.Trace("rotindex.hit",
		"A", rot.A,
		"B", rot.B,
		"sample", best.SampleID,
		"pos", best.Position,
		"cont_len", len(best.Continuation),
		"preview", preview,
	)

	return best.Continuation
}

/*
Size returns the number of distinct rotation keys stored.
*/
func (idx *RotationIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.entries)
}
