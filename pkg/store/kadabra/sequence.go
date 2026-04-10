package kadabra

import (
	"hash/fnv"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
SequenceRecord is the replicated DHT value stored at Kadabra nodes.
The sequence text for trie insertion and hashing is always derived from
Value.String() so routing stays aligned with primitive token layout.
*/
type SequenceRecord struct {
	Key       uint64
	Value     primitive.Value
	Affinity  [primitive.AffinityWords]uint64
	Label     string
	Publisher uint64
}

/*
Hash derives the DHT key for this sequence record from its
label and the string form of the stored Value.
*/
func (record SequenceRecord) Hash() uint64 {
	hasher := fnv.New64a()

	label := record.Label

	if len(label) > 0 {
		_, _ = hasher.Write(unsafe.Slice(unsafe.StringData(label), len(label)))
	}

	var sep [1]byte

	_, _ = hasher.Write(sep[:])

	tok := record.Value.String()

	if len(tok) > 0 {
		_, _ = hasher.Write(unsafe.Slice(unsafe.StringData(tok), len(tok)))
	}

	return hasher.Sum64()
}
