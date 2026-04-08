package markovtrie

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
trieEdgeKey is the trie edge string derived from Value's token region.

The view aliases Value storage like String does, but avoids copying the
UTF-8 slab into a new allocation; callers must use the key immediately and
must not retain it if the backing Value mutates.
*/
func trieEdgeKey(value primitive.Value) string {
	slab := value.TokenRegionBytes()

	if len(slab) == 0 {
		return ""
	}

	return unsafe.String(unsafe.SliceData(slab), len(slab))
}
