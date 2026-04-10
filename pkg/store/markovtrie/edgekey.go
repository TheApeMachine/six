package markovtrie

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
trieEdgeKey is the trie edge string derived from Value's token region.

The token region already contains Morton-coded bytes (applied at Value
creation time), so the key inherits multi-dimensional locality without
any additional encoding here.
*/
func trieEdgeKey(value primitive.Value) string {
	slab := value.TokenRegionBytes()

	if len(slab) == 0 {
		return ""
	}

	return unsafe.String(unsafe.SliceData(slab), len(slab))
}
