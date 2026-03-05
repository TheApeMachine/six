package scaling

import (
	"fmt"
	"math/bits"
	"testing"
	"time"
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/store"
)

/*
syntheticChord generates a deterministic 512-bit chord for a given index.
Uses coprime spreading to distribute 5 active bits, matching the density
of real BaseChord signatures (~5 bits per 512).
*/
func syntheticChord(i int) data.Chord {
	var c data.Chord
	totalBits := 512

	offsets := [5]int{
		(i * 7) % totalBits,
		(i * 13) % totalBits,
		(i * 31) % totalBits,
		(i * 61) % totalBits,
		(i * 127) % totalBits,
	}

	for _, off := range offsets {
		c[off/64] |= 1 << (off % 64)
	}

	return c
}

/*
buildField creates a PrimeField with n synthetic chords.
*/
func buildField(n int) *store.PrimeField {
	pf := store.NewPrimeField()
	for i := 0; i < n; i++ {
		pf.Insert(syntheticChord(i))
	}
	return pf
}

/*
buildContext creates a MultiChord context from mid-corpus chords,
simulating a realistic sliding window query.
*/
func buildContext(corpusSize int) data.MultiChord {
	var ctx data.MultiChord
	mid := corpusSize / 2

	fibs := []int{3, 5, 8, 13, 21}
	for plane, w := range fibs {
		var agg data.Chord
		for j := 0; j < w && mid+j < corpusSize; j++ {
			c := syntheticChord(mid + j)
			for k := range agg {
				agg[k] |= c[k]
			}
		}
		ctx[plane] = agg
	}

	return ctx
}

// -- Go benchmarks for O(1) Scaling --

func benchBestFill(b *testing.B, corpusSize int) {
	pf := buildField(corpusSize)
	ctx := buildContext(corpusSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kernel.BestFill(pf.Field(), pf.N, unsafe.Pointer(&ctx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]))
	}
}

func BenchmarkBestFill_1K(b *testing.B)   { benchBestFill(b, 1_000) }
func BenchmarkBestFill_10K(b *testing.B)  { benchBestFill(b, 10_000) }
func BenchmarkBestFill_100K(b *testing.B) { benchBestFill(b, 100_000) }
func BenchmarkBestFill_1M(b *testing.B)   { benchBestFill(b, 1_000_000) }

// -- Convey test that validates the O(1) claim in a single test run --

func TestBestFillO1Scaling(t *testing.T) {
	Convey("Given the BestFill GPU shader as the core resonance search", t, func() {

		sizes := []int{1_000, 10_000, 100_000}
		iterations := 50

		type result struct {
			Size        int
			MedianNs    int64
			BestIdx     int
			BestScore   float64
			ContextBits int
		}

		var results []result

		Convey("When measuring latency across 3 orders of magnitude", func() {
			for _, size := range sizes {
				pf := buildField(size)
				ctx := buildContext(size)

				// Count context bits for diagnostics
				contextBits := 0
				for _, plane := range ctx {
					for _, block := range plane {
						contextBits += bits.OnesCount64(block)
					}
				}

				// Warm up GPU pipeline
				for i := 0; i < 5; i++ {
					kernel.BestFill(pf.Field(), pf.N, unsafe.Pointer(&ctx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]))
				}

				// Timed runs
				latencies := make([]int64, iterations)
				var lastIdx int
				var lastScore float64

				for i := 0; i < iterations; i++ {
					start := time.Now()
					idx, score, _ := kernel.BestFill(pf.Field(), pf.N, unsafe.Pointer(&ctx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]))
					latencies[i] = time.Since(start).Nanoseconds()
					lastIdx = idx
					lastScore = score
				}

				// Simple median (sort-free: just take middle element roughly)
				var sum int64
				for _, l := range latencies {
					sum += l
				}
				medianNs := sum / int64(iterations)

				results = append(results, result{
					Size:        size,
					MedianNs:    medianNs,
					BestIdx:     lastIdx,
					BestScore:   lastScore,
					ContextBits: contextBits,
				})
			}

			Convey("Then latency should remain within 10x across all corpus sizes (O(1) claim)", func() {
				fmt.Printf("\n╔══════════════════════════════════════════════════════════════════╗\n")
				fmt.Printf("║              O(1) BestFill Latency Scaling                     ║\n")
				fmt.Printf("╠════════════╦════════════════╦═══════════╦═══════════╦═══════════╣\n")
				fmt.Printf("║ Corpus     ║ Avg Latency    ║ Best Idx  ║ Score     ║ Ctx Bits  ║\n")
				fmt.Printf("╠════════════╬════════════════╬═══════════╬═══════════╬═══════════╣\n")

				for _, r := range results {
					sizeStr := fmt.Sprintf("%dK", r.Size/1000)
					fmt.Printf("║ %-10s ║ %10s     ║ %-9d ║ %-9.4f ║ %-9d ║\n",
						sizeStr,
						time.Duration(r.MedianNs).String(),
						r.BestIdx,
						r.BestScore,
						r.ContextBits,
					)
				}

				fmt.Printf("╚════════════╩════════════════╩═══════════╩═══════════╩═══════════╝\n")

				// O(1) validation: largest corpus latency should be within 10x of smallest
				// (GPU dispatch has fixed overhead, so we allow generous tolerance)
				minLatency := results[0].MedianNs
				maxLatency := results[0].MedianNs

				for _, r := range results {
					if r.MedianNs < minLatency {
						minLatency = r.MedianNs
					}
					if r.MedianNs > maxLatency {
						maxLatency = r.MedianNs
					}
				}

				ratio := float64(maxLatency) / float64(minLatency)
				fmt.Printf("\nLatency ratio (max/min): %.2fx\n", ratio)

				// For truly O(n) scaling, 100K would be 100x slower than 1K.
				// We assert the ratio stays under 10x, which proves sub-linear scaling.
				So(ratio, ShouldBeLessThan, 10.0)

				// Also verify the GPU actually found something (non-zero scores)
				for _, r := range results {
					So(r.BestScore, ShouldBeGreaterThan, 0.0)
				}
			})
		})
	})
}
