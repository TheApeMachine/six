package scaling

import (
	"io"
	"iter"
	"math/rand"
	"sync"
)

/*
SyntheticDataset generates random printable ASCII samples.
Implements data.Provider. Seeded RNG for reproducibility.
*/
type SyntheticDataset struct {
	sampleSize int
	maxSamples int
	seed       int64

	readMu       sync.Mutex
	readRNG      *rand.Rand
	readSample   int
	readPosInSmp int
	readInit     bool
}

/*
NewSyntheticDataset creates a dataset of maxSamples × sampleSize random bytes.
*/
func NewSyntheticDataset(sampleSize, maxSamples int, seed int64) *SyntheticDataset {
	return &SyntheticDataset{
		sampleSize: sampleSize,
		maxSamples: maxSamples,
		seed:       seed,
	}
}

/*
Generate emits RawTokens for all samples. Printable ASCII (0x20-0x7E).
*/
func (ds *SyntheticDataset) Generate() iter.Seq[byte] {
	rng := rand.New(rand.NewSource(ds.seed))

	return func(yield func(byte) bool) {
		for sampleID := 0; sampleID < ds.maxSamples; sampleID++ {
			for pos := 0; pos < ds.sampleSize; pos++ {
				b := byte(0x20 + rng.Intn(95))
				if !yield(b) {
					return
				}
			}
		}
	}
}

func (ds *SyntheticDataset) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if ds.maxSamples <= 0 || ds.sampleSize <= 0 {
		return 0, io.EOF
	}

	ds.readMu.Lock()
	defer ds.readMu.Unlock()

	if !ds.readInit {
		ds.readRNG = rand.New(rand.NewSource(ds.seed))
		ds.readInit = true
	}

	for n < len(p) {
		if ds.advanceReadSampleForNextByte() {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}

		p[n] = byte(0x20 + ds.readRNG.Intn(95))
		ds.readPosInSmp++
		n++
	}
	return n, nil
}

// advanceReadSampleForNextByte skips any fully consumed samples so the next
// byte will come from the current sample or, if none remain, reports EOF.
// It updates readSample and readPosInSmp. The return value is true when
// there is no next byte to read (caller should return io.EOF if no bytes
// were copied yet, otherwise end the read successfully).
func (ds *SyntheticDataset) advanceReadSampleForNextByte() bool {
	for ds.readSample < ds.maxSamples && ds.readPosInSmp >= ds.sampleSize {
		ds.readSample++
		ds.readPosInSmp = 0
	}
	return ds.readSample >= ds.maxSamples
}

func (ds *SyntheticDataset) Close() error {
	return nil
}
