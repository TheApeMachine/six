package kadabra

import (
	"hash/fnv"

	"github.com/theapemachine/six/pkg/errnie"
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

	for _, segment := range [][]byte{
		[]byte(record.Label), {0}, []byte(record.Value.String()),
	} {
		_, err := hasher.Write(segment)

		if errnie.Error(err) != nil {
			return 0
		}
	}

	return hasher.Sum64()
}
