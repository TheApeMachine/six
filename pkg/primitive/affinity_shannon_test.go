package primitive

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/bits"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

func setupAffinityTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() { *core.Cfg = original })

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 512
	core.Cfg.Value.Region.Affinity.Start = 8
	core.Cfg.Value.Region.Affinity.Bits = 512
	core.Cfg.Value.Region.ID.Start = 127
}

// randomTokenBytes fills the 64-byte token region with cryptographic randomness.
func randomTokenBytes() [64]byte {
	var buf [64]byte
	rand.Read(buf[:])
	return buf
}

// affinityPopcount returns how many bits are set across the full 8-word affinity region.
func affinityPopcount(v *Value) int {
	start := core.Cfg.Value.Region.Affinity.Start
	total := 0
	for i := 0; i < 8; i++ {
		total += bits.OnesCount64(v[start+i])
	}
	return total
}

// affinityHammingDistance returns popcount(a XOR b) across the 8-word affinity.
func affinityHammingDistance(a, b *Value) int {
	start := core.Cfg.Value.Region.Affinity.Start
	total := 0
	for i := 0; i < 8; i++ {
		total += bits.OnesCount64(a[start+i] ^ b[start+i])
	}
	return total
}

// bloomSaturation64 returns fraction of bits set in a single 64-bit Bloom filter
// after inserting n distinct high-entropy 64-byte inputs.
func bloomSaturation64(n int) float64 {
	var bloom uint64
	for i := 0; i < n; i++ {
		buf := randomTokenBytes()
		bloom |= ComputeAffinityBloom(buf[:])
	}
	return float64(bits.OnesCount64(bloom)) / 64.0
}

// lshCollisionRate computes the fraction of distinct random input pairs
// whose LSH output (word 0 of affinity) is identical.
func lshCollisionRate(n int) float64 {
	hashes := make([]uint64, n)
	start := core.Cfg.Value.Region.Affinity.Start

	for i := 0; i < n; i++ {
		buf := randomTokenBytes()
		v, _ := NewValue(buf[:])
		v.ComputeAffinityLSH()
		hashes[i] = v[start]
		v.Close()
	}

	collisions := 0
	pairs := 0
	// Sample up to 10000 random pairs.
	limit := min(n*(n-1)/2, 10000)
	for i := 0; i < n && pairs < limit; i++ {
		for j := i + 1; j < n && pairs < limit; j++ {
			pairs++
			if hashes[i] == hashes[j] {
				collisions++
			}
		}
	}

	if pairs == 0 {
		return 0
	}
	return float64(collisions) / float64(pairs)
}

func TestBloomSaturationCurve(t *testing.T) {
	setupAffinityTest(t)

	// 64-bit Bloom with 62 possible n-grams per 64-byte input (3-byte window).
	// Each n-gram sets 1 bit. Theoretical saturation: 1 - (63/64)^(62*n).
	counts := []int{1, 2, 5, 10, 20, 50, 100, 200}
	t.Log("=== 64-bit Bloom saturation (high-entropy 64-byte inputs) ===")
	t.Logf("%-10s %-12s %-12s %-s", "n_values", "empirical", "theoretical", "useful_bits")

	for _, n := range counts {
		empirical := bloomSaturation64(n)
		// Each input produces ~62 n-grams, each sets 1 of 64 bits.
		theoretical := 1.0 - math.Pow(63.0/64.0, float64(62*n))
		usefulBits := 64.0 * (1.0 - empirical) // bits still zero = discriminative capacity
		t.Logf("%-10d %-12.4f %-12.4f %-6.1f", n, empirical, theoretical, usefulBits)
	}
}

func TestLSHShannonEntropy(t *testing.T) {
	setupAffinityTest(t)

	// Generate many random Values, compute LSH, measure bit entropy.
	n := 1000
	bitOnes := make([]int, 64)
	start := core.Cfg.Value.Region.Affinity.Start

	for i := 0; i < n; i++ {
		buf := randomTokenBytes()
		v, _ := NewValue(buf[:])
		v.ComputeAffinityLSH()
		word := v[start]
		for b := 0; b < 64; b++ {
			if (word>>uint(b))&1 == 1 {
				bitOnes[b]++
			}
		}
		v.Close()
	}

	t.Log("=== LSH bit entropy (1000 random 64-byte inputs) ===")
	totalEntropy := 0.0
	for b := 0; b < 64; b++ {
		p := float64(bitOnes[b]) / float64(n)
		if p == 0 || p == 1 {
			continue
		}
		h := -p*math.Log2(p) - (1-p)*math.Log2(1-p)
		totalEntropy += h
	}
	t.Logf("Total entropy: %.2f / 64.00 bits (%.1f%%)", totalEntropy, totalEntropy/64*100)
	t.Logf("Per-bit mean p(1): %.4f (ideal: 0.5000)", func() float64 {
		sum := 0
		for _, c := range bitOnes {
			sum += c
		}
		return float64(sum) / float64(64*n)
	}())
}

func TestLSHCollisionRate(t *testing.T) {
	setupAffinityTest(t)

	t.Log("=== LSH collision rate (random 64-byte inputs) ===")
	for _, n := range []int{100, 500, 1000} {
		rate := lshCollisionRate(n)
		t.Logf("n=%d: collision rate=%.6f", n, rate)
	}
}

func TestHammingDistanceDistribution(t *testing.T) {
	setupAffinityTest(t)

	n := 500
	values := make([]*Value, n)

	for i := 0; i < n; i++ {
		buf := randomTokenBytes()
		v, _ := NewValue(buf[:])
		v.ComputeAffinityLSH()
		values[i] = v
	}

	defer func() {
		for _, v := range values {
			v.Close()
		}
	}()

	distances := make([]int, 0, 10000)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n && len(distances) < 10000; j++ {
			distances = append(distances, affinityHammingDistance(values[i], values[j]))
		}
	}

	sum := 0
	minD, maxD := 512, 0
	for _, d := range distances {
		sum += d
		if d < minD {
			minD = d
		}
		if d > maxD {
			maxD = d
		}
	}
	meanVal := float64(sum) / float64(len(distances))

	variance := 0.0
	for _, d := range distances {
		diff := float64(d) - meanVal
		variance += diff * diff
	}
	stddevVal := math.Sqrt(variance / float64(len(distances)))

	t.Log("=== Hamming distance distribution (512-bit affinity, 8 independent projections) ===")
	t.Logf("Pairs sampled: %d", len(distances))
	t.Logf("Mean: %.2f / 512 (ideal for random: 256.0)", meanVal)
	t.Logf("Stddev: %.2f", stddevVal)
	t.Logf("Min: %d, Max: %d", minD, maxD)
}

func TestAffinityCapacity(t *testing.T) {
	setupAffinityTest(t)

	// Core question: how many distinct high-entropy 64-byte sequences can
	// a 512-bit affinity space distinguish before pairwise Hamming distances
	// become indistinguishable from random?
	//
	// For k independent hash bits, the probability of a random collision
	// (Hamming distance = 0) between two items is 2^(-k).
	// For 64 effective bits (current LSH): 2^(-64) ≈ 5.4e-20 per pair.
	// Birthday bound for 50% collision: ~2^32 ≈ 4 billion items.
	//
	// But the real limit is DISCRIMINATION: can we still tell similar
	// items apart from different items? This is the Shannon limit question.

	t.Log("=== Affinity capacity analysis ===")

	// Measure: how does mean Hamming distance between SIMILAR inputs compare
	// to RANDOM inputs? If they converge, we've hit capacity.

	// Generate "similar" pairs: same prefix, different suffix.
	nPairs := 200
	similarDist := make([]int, 0, nPairs)
	randomDist := make([]int, 0, nPairs)
	start := core.Cfg.Value.Region.Affinity.Start

	for i := 0; i < nPairs; i++ {
		// Similar: share first 48 bytes, differ in last 16.
		base := randomTokenBytes()
		a, _ := NewValue(base[:])
		a.ComputeAffinityLSH()

		var modified [64]byte
		copy(modified[:], base[:])
		rand.Read(modified[48:]) // change last 16 bytes
		b, _ := NewValue(modified[:])
		b.ComputeAffinityLSH()

		similarDist = append(similarDist, hammingWord(a[start], b[start]))

		// Random: completely independent.
		c := randomTokenBytes()
		cv, _ := NewValue(c[:])
		cv.ComputeAffinityLSH()

		randomDist = append(randomDist, hammingWord(a[start], cv[start]))

		a.Close()
		b.Close()
		cv.Close()
	}

	simMean := mean(similarDist)
	randMean := mean(randomDist)
	separation := randMean - simMean

	t.Logf("Similar pairs (48/64 bytes shared): mean Hamming = %.2f / 64", simMean)
	t.Logf("Random pairs:                       mean Hamming = %.2f / 64", randMean)
	t.Logf("Separation:                         %.2f bits", separation)
	t.Logf("")

	if separation < 2.0 {
		t.Logf("WARNING: LSH cannot distinguish 75%%-similar from random at 64 bits.")
		t.Logf("         Need more independent projections or a different hash.")
	} else {
		t.Logf("LSH provides %.1f bits of separation — sufficient for routing.", separation)
	}

	// Theoretical capacity: with k effective bits and separation s,
	// we can distinguish ~2^(s) levels of similarity.
	t.Logf("")
	t.Logf("With 8 independent 64-bit projections (512 bits):")
	t.Logf("  Expected separation: ~%.1f bits", separation*8)
	t.Logf("  Similarity levels: ~2^%.0f", separation*8)
}

func hammingWord(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func mean(vals []int) float64 {
	sum := 0
	for _, v := range vals {
		sum += v
	}
	return float64(sum) / float64(len(vals))
}

// TestMultiProjectionLSH tests the real ComputeAffinityLSH which now fills
// all 8 affinity words with independent projections.
func TestMultiProjectionLSH(t *testing.T) {
	setupAffinityTest(t)

	nPairs := 500
	similarDist := make([]int, 0, nPairs)
	randomDist := make([]int, 0, nPairs)

	for i := 0; i < nPairs; i++ {
		base := randomTokenBytes()
		a, _ := NewValue(base[:])
		a.ComputeAffinityLSH()

		// Similar: 75% shared.
		var modified [64]byte
		copy(modified[:], base[:])
		rand.Read(modified[48:])
		b, _ := NewValue(modified[:])
		b.ComputeAffinityLSH()

		similarDist = append(similarDist, affinityHammingDistance(a, b))

		// Random.
		c := randomTokenBytes()
		cv, _ := NewValue(c[:])
		cv.ComputeAffinityLSH()
		randomDist = append(randomDist, affinityHammingDistance(a, cv))

		a.Close()
		b.Close()
		cv.Close()
	}

	simMean := mean(similarDist)
	randMean := mean(randomDist)
	separation := randMean - simMean

	simStd := stddev(similarDist, simMean)
	randStd := stddev(randomDist, randMean)

	pooledStd := math.Sqrt((simStd*simStd + randStd*randStd) / 2)
	cohensD := separation / pooledStd

	t.Log("=== 8-projection LSH (512-bit affinity, 64-byte tokens) ===")
	t.Logf("Similar pairs (75%% shared): mean Hamming = %.2f ± %.2f / 512", simMean, simStd)
	t.Logf("Random pairs:               mean Hamming = %.2f ± %.2f / 512", randMean, randStd)
	t.Logf("Separation:                 %.2f bits", separation)
	t.Logf("Cohen's d:                  %.2f (>0.8 = large effect)", cohensD)
	t.Logf("")

	if pooledStd > 0 {
		z := separation / randStd
		t.Logf("Z-score (separation/random_stddev): %.2f", z)
		t.Logf("Estimated false positive rate: ~%.2e", math.Erfc(z/math.Sqrt(2))/2)
	}

	if separation > 0 {
		effectiveBits := math.Log2(512 / separation * float64(len(similarDist)))
		t.Logf("Effective discriminative bits: ~%.0f", effectiveBits)
	}
}

func stddev(vals []int, m float64) float64 {
	variance := 0.0
	for _, v := range vals {
		diff := float64(v) - m
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(vals)))
}

// BenchmarkComputeAffinityLSH measures single-projection cost.
func BenchmarkComputeAffinityLSH(b *testing.B) {
	setupAffinityTest(b)

	buf := randomTokenBytes()
	v, _ := NewValue(buf[:])
	defer v.Close()

	b.ResetTimer()
	for b.Loop() {
		v.ComputeAffinityLSH()
	}
}

// BenchmarkComputeAffinityBloom measures Bloom filter cost.
func BenchmarkComputeAffinityBloom(b *testing.B) {
	buf := randomTokenBytes()

	b.ResetTimer()
	for b.Loop() {
		_ = ComputeAffinityBloom(buf[:])
	}
}

// BenchmarkAffinityHammingDistance measures comparison cost.
func BenchmarkAffinityHammingDistance(b *testing.B) {
	setupAffinityTest(b)

	buf1 := randomTokenBytes()
	buf2 := randomTokenBytes()
	a, _ := NewValue(buf1[:])
	bv, _ := NewValue(buf2[:])
	defer a.Close()
	defer bv.Close()

	a.ComputeAffinityLSH()
	bv.ComputeAffinityLSH()

	b.ResetTimer()
	for b.Loop() {
		affinityHammingDistance(a, bv)
	}
}

func init() {
	// Ensure Cfg is initialized for tests that run without viper.
	if core.Cfg == nil {
		core.Cfg = &core.Config{}
	}

	if core.Cfg.Value.Words == 0 {
		core.Cfg.Value.Words = 128
		core.Cfg.Value.Bytes = 1024
	}
}

// Ensure unused import doesn't cause issues.
var _ = fmt.Sprintf
var _ unsafe.Pointer
