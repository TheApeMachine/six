package local

import (
	"context"
	"iter"
)

/*
Dataset streams in-memory corpus bytes as RawTokens. Each sample is a []byte;
bytes are emitted with incrementing Pos per sample.
*/
type Dataset struct {
	ctx    context.Context
	cancel context.CancelFunc
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
Generate returns an iterator of RawTokens for each byte in the corpus.
Pos resets per sample.
*/
func (ds *Dataset) Generate() iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for _, data := range ds.corpus {
			for _, symbol := range data {
				if !yield(symbol) {
					return
				}
			}
		}
	}
}

func DatasetWithContext(ctx context.Context) datasetOpts {
	return func(dataset *Dataset) {
		dataset.ctx, dataset.cancel = context.WithCancel(ctx)
	}
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
