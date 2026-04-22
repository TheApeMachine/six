package local

import (
	"context"
	"io"
	"iter"
	"sync"

	"github.com/theapemachine/six/experiment/data"
)

/*
Dataset exposes an in-memory corpus: each row is one data.Sample (Text only).
*/
type Dataset struct {
	ctx    context.Context
	cancel context.CancelFunc
	corpus [][]byte

	readMu  sync.Mutex
	readRow int // index in corpus
	readOff int // offset within corpus[readRow]
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
Generate yields one Sample per corpus row.
*/
func (ds *Dataset) Generate() iter.Seq[data.Sample] {
	return func(yield func(data.Sample) bool) {
		for index, row := range ds.corpus {
			if !yield(data.Sample{
				SampleID: uint32(index),
				Text:     row,
			}) {
				return
			}
		}
	}
}

func (ds *Dataset) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	ds.readMu.Lock()
	defer ds.readMu.Unlock()

	for n < len(p) {
		for ds.readRow < len(ds.corpus) && ds.readOff >= len(ds.corpus[ds.readRow]) {
			ds.readRow++
			ds.readOff = 0
		}
		if ds.readRow >= len(ds.corpus) {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}

		row := ds.corpus[ds.readRow]
		copied := copy(p[n:], row[ds.readOff:])
		ds.readOff += copied
		n += copied
	}
	return n, nil
}

// Reset rewinds the read cursor to the start of the corpus so Read can scan again
// (e.g. multiple epochs). It is safe for concurrent use with Read.
func (ds *Dataset) Reset() {
	ds.readMu.Lock()
	defer ds.readMu.Unlock()
	ds.readRow = 0
	ds.readOff = 0
}

func (ds *Dataset) Close() error {
	return nil
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

