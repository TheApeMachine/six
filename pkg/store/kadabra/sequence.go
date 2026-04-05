package kadabra

import (
	"hash/fnv"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
SequenceRecord is the replicated DHT
value stored at Kadabra nodes.
*/
type SequenceRecord struct {
	Key       uint64
	Sequence  string
	Label     string
	Publisher NodeID
}

/*
HashSequenceRecord derives the DHT key
for one replicated sequence record.
*/
func HashSequenceRecord(
	sequence string, label string,
) uint64 {
	hasher := fnv.New64a()

	for _, b := range [][]byte{
		[]byte(label), {0}, []byte(sequence),
	} {
		_, err := hasher.Write(b)

		if errnie.Error(err) != nil {
			return 0
		}
	}

	return hasher.Sum64()
}
