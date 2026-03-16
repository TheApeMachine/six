package local

import (
	"github.com/theapemachine/six/pkg/store/data/provider"
)

/*
Dataset streams in-memory corpus bytes as RawTokens. Each sample is a []byte;
bytes are emitted with incrementing Pos per sample.
*/
type Dataset struct {
	corpus [][]byte
}

type datasetOpts func(*Dataset)

/*
New returns a Dataset over the given corpus. corpus[sampleID] is one sample's bytes.
*/
func New(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
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
			for _, symbol := range data {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   symbol,
					Pos:      pos,
				}
				pos++
			}
		}
	}()
	return out
}

func WithStrings(corpus []string) datasetOpts {
	return func(dataset *Dataset) {
		data := make([][]byte, len(corpus))

		for i, s := range corpus {
			data[i] = []byte(s)
		}

		dataset.corpus = data
	}
}

func WithBytes(corpus []byte) datasetOpts {
	return func(dataset *Dataset) {
		dataset.corpus = [][]byte{corpus}
	}
}

func WithBytesOfBytes(corpus [][]byte) datasetOpts {
	return func(dataset *Dataset) {
		dataset.corpus = corpus
	}
}


