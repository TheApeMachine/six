package local

import (
	"github.com/theapemachine/six/pkg/provider"
)

/*
Dataset streams in-memory corpus bytes as RawTokens. Each sample is a []byte;
bytes are emitted with incrementing Pos per sample.
*/
type Dataset struct {
	corpus [][]byte
}

/*
New returns a Dataset over the given corpus. corpus[sampleID] is one sample's bytes.
*/
func New(corpus [][]byte) *Dataset {
	return &Dataset{corpus: corpus}
}

/*
Generate returns a channel that emits RawTokens for each byte in the corpus.
Pos resets per sample. Closes when done.
*/
func (ds *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	go func() {
		defer close(out)
		for sampleID, data := range ds.corpus {
			var pos uint32
			for _, b := range data {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   b,
					Pos:      pos,
				}
				pos++
			}
		}
	}()
	return out
}
