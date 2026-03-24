package scaling

import (
	"iter"
	"math/rand"
)

/*
SyntheticDataset generates random printable ASCII samples.
Implements data.Provider. Seeded RNG for reproducibility.
*/
type SyntheticDataset struct {
	sampleSize int
	maxSamples int
	seed       int64
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
