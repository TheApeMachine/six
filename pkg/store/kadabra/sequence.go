package kadabra

import (
	"hash/fnv"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
SequenceRecord is the replicated DHT value stored at Kadabra nodes.
*/
type SequenceRecord struct {
	Key       uint64
	Sequence  string
	Label     string
	Publisher uint64
}

/*
Hash derives the DHT key for this sequence record from its
sequence content and label.
*/
func (record SequenceRecord) Hash() uint64 {
	hasher := fnv.New64a()

	for _, segment := range [][]byte{
		[]byte(record.Label), {0}, []byte(record.Sequence),
	} {
		_, err := hasher.Write(segment)

		if errnie.Error(err) != nil {
			return 0
		}
	}

	return hasher.Sum64()
}
