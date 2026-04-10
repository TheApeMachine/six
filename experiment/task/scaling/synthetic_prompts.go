package scaling

import (
	"math/rand"

	tools "github.com/theapemachine/six/experiment"
)

// syntheticSamplePrompts builds prefix/holdout pairs from the same RNG stream
// as SyntheticDataset.Generate (same seed, sample order, sampleSize).
// If suffixBytes > 0, holdout is the last suffixBytes bytes; otherwise a 50/50
// byte split is used.
func syntheticSamplePrompts(ds *SyntheticDataset, maxPrompts, suffixBytes int) (prompts []string, holdouts [][]byte) {
	if maxPrompts <= 0 || ds.maxSamples <= 0 {
		return nil, nil
	}
	n := maxPrompts
	if ds.maxSamples < n {
		n = ds.maxSamples
	}
	rng := rand.New(rand.NewSource(ds.seed))
	for i := 0; i < n; i++ {
		buf := make([]byte, ds.sampleSize)
		for j := range buf {
			buf[j] = byte(0x20 + rng.Intn(95))
		}
		s := string(buf)
		var pr, ho string
		if suffixBytes > 0 {
			pr, ho = tools.ByteSuffixLastN(s, suffixBytes)
		} else {
			pr, ho = tools.BytePrefixFraction(s, 0.5)
		}
		if pr == "" || ho == "" {
			continue
		}
		prompts = append(prompts, pr)
		holdouts = append(holdouts, []byte(ho))
	}
	return prompts, holdouts
}
