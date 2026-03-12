package scaling

import (
	"math/rand"

	"github.com/theapemachine/six/pkg/provider"
)

/*
SyntheticDataset generates random printable ASCII samples.
Implements provider.Dataset. Seeded RNG for reproducibility.
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
func (ds *SyntheticDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	rng := rand.New(rand.NewSource(ds.seed))

	go func() {
		defer close(out)

		for sampleID := 0; sampleID < ds.maxSamples; sampleID++ {
			for pos := 0; pos < ds.sampleSize; pos++ {
				b := byte(0x20 + rng.Intn(95))
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   b,
					Pos:      uint32(pos),
				}
			}
		}
	}()

	return out
}
