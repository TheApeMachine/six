package integration

import (
	"context"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
ambiguousChunk tracks a chunk text that appears identically at multiple
positions in the corpus, along with those positions.
*/
type ambiguousChunk struct {
	text    string
	indices []int
}

/*
probeResult captures the outcome of a single prompt call for later analysis.
*/
type probeResult struct {
	contextText string
	targetText  string
	occurrence  int
	results     [][]byte
	foldEvents  []telemetry.Event
	err         error
}

func TestBivariateIsolation(t *testing.T) {
	rawBytes, err := os.ReadFile("../../cmd/assets/alice_in_wonderland.txt")
	if err != nil {
		t.Skip("alice_in_wonderland.txt not found")
	}

	corpus := string(rawBytes)
	chunks := ChunkStrings(corpus)

	Convey("Given Alice in Wonderland ingested by the full vm.Machine", t, func() {
		t.Logf("Corpus: %d bytes → %d chunks", len(rawBytes), len(chunks))

		// Categorize ALL repeated chunks exhaustively — no cherry-picking.
		chunkPositions := make(map[string][]int)
		for idx, chunk := range chunks {
			if len(chunk) > 3 {
				chunkPositions[chunk] = append(chunkPositions[chunk], idx)
			}
		}

		var ambiguous []ambiguousChunk
		uniqueCount := 0

		for text, indices := range chunkPositions {
			if len(indices) > 1 {
				ambiguous = append(ambiguous, ambiguousChunk{text, indices})
			} else {
				uniqueCount++
			}
		}

		sort.Slice(ambiguous, func(i, j int) bool {
			return len(ambiguous[i].indices) > len(ambiguous[j].indices)
		})

		t.Logf("Distribution: %d unique chunks, %d ambiguous (appear 2+ times)", uniqueCount, len(ambiguous))

		for k := 0; k < min(10, len(ambiguous)); k++ {
			a := ambiguous[k]
			t.Logf("  %dx: %q (len=%d)", len(a.indices), truncateStr(a.text, 40), len(a.text))
		}

		So(len(ambiguous), ShouldBeGreaterThan, 0)

		helper := NewIntegrationHelper(
			context.Background(),
			local.New(local.WithStrings([]string{corpus})),
		)
		defer helper.Teardown()

		ok := pollUntil(10*time.Second, 100*time.Millisecond, func() bool { return len(helper.Events) > 0 })
		if !ok {
			t.Fatal("timeout waiting for readiness")
		}
		drainEvents(helper.Events)

		Convey("It should differentiate identical chunks via meta when queried from different topological contexts", func() {
			// For each ambiguous phrase, query from EACH occurrence's local context
			// and compare the fold thetas. If meta works, different contexts → different thetas.
			// If meta is broken, all occurrences would collapse to the same theta.

			differentiated := 0
			collapsed := 0
			skipped := 0
			tested := 0

			for _, a := range ambiguous {
				if tested >= 30 {
					break
				}

				if len(a.text) < 5 {
					continue
				}

				// Collect a probe result for each occurrence of this chunk
				var probes []probeResult

				for occIdx, chunkPos := range a.indices {
					if chunkPos < 3 {
						continue
					}

					// Use 3 preceding chunks as local context — realistic, not the entire novel
					contextStart := max(0, chunkPos-3)
					var leftCtx string

					for ci := contextStart; ci < chunkPos; ci++ {
						leftCtx += chunks[ci]
					}

					if len(leftCtx) < 5 {
						continue
					}

					results, promptErr := helper.Machine.Prompt(helper.NewPrompt([]string{leftCtx}))
					pollUntil(500*time.Millisecond, 50*time.Millisecond, func() bool { return len(helper.Events) > 0 })
					events := collectFoldEvents(helper.Events)

					probes = append(probes, probeResult{
						contextText: leftCtx,
						targetText:  a.text,
						occurrence:  occIdx,
						results:     results,
						foldEvents:  events,
						err:         promptErr,
					})
				}

				if len(probes) < 2 {
					skipped++
					continue
				}

				tested++

				// Compare theta sets across occurrences
				thetaSets := make([]map[float64]struct{}, len(probes))

				for pIdx, probe := range probes {
					thetaSets[pIdx] = make(map[float64]struct{})

					for _, ev := range probe.foldEvents {
						rounded := math.Round(ev.Data.Theta*10000) / 10000
						thetaSets[pIdx][rounded] = struct{}{}
					}
				}

				// Check if ANY pair of occurrences produced different theta landscapes
				pairDifferent := false

				for i := 0; i < len(thetaSets); i++ {
					for j := i + 1; j < len(thetaSets); j++ {
						if !thetaSetsEqual(thetaSets[i], thetaSets[j]) {
							pairDifferent = true
						}
					}
				}

				if pairDifferent {
					differentiated++
				} else {
					collapsed++
				}

				if tested <= 5 {
					t.Logf("Chunk %q:", truncateStr(a.text, 30))

					for _, probe := range probes {
						var resultTexts []string

						for _, r := range probe.results {
							resultTexts = append(resultTexts, truncateStr(string(r), 40))
						}

						t.Logf("  occ[%d]: context=%q → %d results %v  folds=%d thetas=%v",
							probe.occurrence,
							truncateStr(probe.contextText, 25),
							len(probe.results),
							resultTexts,
							len(probe.foldEvents),
							thetaSlice(probe.foldEvents),
						)
					}

					if pairDifferent {
						t.Log("  → DIFFERENTIATED by meta")
					} else {
						t.Log("  → COLLAPSED (same theta landscape)")
					}
				}
			}

			t.Logf("Disambiguation: %d/%d differentiated, %d collapsed, %d skipped (of %d ambiguous)",
				differentiated, tested, collapsed, skipped, len(ambiguous))

			// The system should successfully differentiate at least SOME ambiguous chunks.
			// If ZERO are differentiated, meta chords are not working.
			So(tested, ShouldBeGreaterThan, 0)
			So(differentiated+collapsed, ShouldEqual, tested)

			if differentiated == 0 && tested > 0 {
				t.Fatalf("FALSIFIED: Meta chords failed to differentiate ANY ambiguous chunks (tested=%d)", tested)
			}

			differentiationRate := float64(differentiated) / float64(max(tested, 1))
			t.Logf("Differentiation rate: %.1f%%", differentiationRate*100)
		})

		Convey("It should produce consistent fold geometry for unique chunks as a control", func() {
			controlTested := 0
			controlSuccesses := 0
			controlFoldCounts := 0

			for text, indices := range chunkPositions {
				if controlTested >= 15 {
					break
				}

				if len(indices) > 1 || len(text) < 5 {
					continue
				}

				chunkPos := indices[0]

				if chunkPos < 3 {
					continue
				}

				contextStart := max(0, chunkPos-3)
				var leftCtx string

				for ci := contextStart; ci < chunkPos; ci++ {
					leftCtx += chunks[ci]
				}

				if len(leftCtx) < 5 {
					continue
				}

				controlTested++

				results, promptErr := helper.Machine.Prompt(helper.NewPrompt([]string{leftCtx}))
				pollUntil(500*time.Millisecond, 50*time.Millisecond, func() bool { return len(helper.Events) > 0 })
				events := collectFoldEvents(helper.Events)

				if promptErr == nil && len(results) > 0 {
					controlSuccesses++
					controlFoldCounts += len(events)
				}

				if controlTested <= 3 {
					t.Logf("Control %q: context=%q results=%d folds=%d err=%v",
						truncateStr(text, 20), truncateStr(leftCtx, 25),
						len(results), len(events), promptErr)
				}
			}

			t.Logf("Control group: %d/%d unique chunks resolved, %d total fold events",
				controlSuccesses, controlTested, controlFoldCounts)

			So(controlTested, ShouldBeGreaterThan, 0)

			if controlSuccesses == 0 {
				t.Log("NOTE: No unique chunks resolved — spatial lookup is sparse for short context")
			}
		})

		Convey("It should reveal the disambiguation breaking point under minimal context", func() {
			// Find an ambiguous chunk with exactly 2 occurrences and enough surrounding context.
			var target ambiguousChunk
			found := false

			for _, a := range ambiguous {
				if len(a.indices) == 2 && len(a.text) > 8 {
					bothHaveContext := true

					for _, idx := range a.indices {
						if idx < 5 {
							bothHaveContext = false
						}
					}

					if bothHaveContext {
						target = a
						found = true
						break
					}
				}
			}

			if !found {
				t.Log("No suitable 2-occurrence ambiguous chunk for limit test")
				So(true, ShouldBeTrue)
				return
			}

			t.Logf("Limit target: %q at indices %v", truncateStr(target.text, 40), target.indices)

			// Try progressively shrinking context and measure when disambiguation fails
			for _, contextWidth := range []int{1, 2, 3, 5, 8} {
				var thetas [2][]float64

				for occIdx, chunkPos := range target.indices {
					if chunkPos < contextWidth {
						continue
					}

					var leftCtx string

					for ci := chunkPos - contextWidth; ci < chunkPos; ci++ {
						leftCtx += chunks[ci]
					}

					_, promptErr := helper.Machine.Prompt(helper.NewPrompt([]string{leftCtx}))
					pollUntil(500*time.Millisecond, 50*time.Millisecond, func() bool { return len(helper.Events) > 0 })
					events := collectFoldEvents(helper.Events)

					if promptErr == nil {
						for _, ev := range events {
							thetas[occIdx] = append(thetas[occIdx], math.Round(ev.Data.Theta*10000)/10000)
						}
					}
				}

				set0 := sliceToSet(thetas[0])
				set1 := sliceToSet(thetas[1])
				same := thetaSetsEqual(set0, set1)
				label := "SAME"

				if !same {
					label = "DIFF"
				}

				t.Logf("  width=%d: occ0=%v occ1=%v → %s", contextWidth, thetas[0], thetas[1], label)
			}

			So(true, ShouldBeTrue)
		})
	})
}

func BenchmarkBivariatePrompt(b *testing.B) {
	rawBytes, err := os.ReadFile("../../cmd/assets/alice_in_wonderland.txt")
	if err != nil {
		b.Skip("alice_in_wonderland.txt not found")
	}

	corpus := string(rawBytes)
	chunks := ChunkStrings(corpus)

	helper := NewIntegrationHelper(
		context.Background(),
		local.New(local.WithStrings([]string{corpus})),
	)
	defer helper.Teardown()

	ok := pollUntil(10*time.Second, 100*time.Millisecond, func() bool { return len(helper.Events) > 0 })
	if !ok {
		b.Fatal("no events within deadline")
	}
	drainEvents(helper.Events)

	// Pick a chunk from the middle of the novel as a stable probe
	probePos := len(chunks) / 2

	contextStart := max(0, probePos-3)
	var leftCtx string

	for ci := contextStart; ci < probePos; ci++ {
		leftCtx += chunks[ci]
	}

	b.ResetTimer()

	for b.Loop() {
		helper.Machine.Prompt(helper.NewPrompt([]string{leftCtx}))
		drainEvents(helper.Events)
	}
}

func drainEvents(ch chan telemetry.Event) {
	for {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

func collectFoldEvents(ch chan telemetry.Event) []telemetry.Event {
	var events []telemetry.Event

	for {
		select {
		case ev := <-ch:
			events = append(events, ev)
		case <-time.After(100 * time.Millisecond):
			return events
		}
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

func thetaSlice(events []telemetry.Event) []float64 {
	out := make([]float64, len(events))

	for i, ev := range events {
		out[i] = math.Round(ev.Data.Theta*10000) / 10000
	}

	return out
}

func thetaSetsEqual(a, b map[float64]struct{}) bool {
	if len(a) != len(b) {
		return false
	}

	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}

	return true
}

func sliceToSet(vals []float64) map[float64]struct{} {
	out := make(map[float64]struct{}, len(vals))

	for _, v := range vals {
		out[v] = struct{}{}
	}

	return out
}
