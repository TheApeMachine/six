package lsm

import "github.com/theapemachine/six/pkg/store/data"

/*
SpatialEntry stores a single edge in the radix forest.
Key is a MortonCoder-packed uint64: Pack(localDepth, symbol).
The data value is deterministic (5 bits per byte value).
Meta values accumulate from different topological contexts.
*/
type SpatialEntry struct {
	Key   uint64
	Value data.Value
	Metas []data.Value
}
