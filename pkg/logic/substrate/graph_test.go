package substrate

import (
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
)

/*
tokenize uses the same Sequitur + BitwiseHealer pipeline as the tokenizer.
*/
func tokenize(raw []byte) [][]byte {
	seq := sequencer.NewSequitur()
	healer := sequencer.NewBitwiseHealer()
	var chunks [][]byte

	for pos, b := range raw {
		byteVal, isBoundary := seq.Analyze(uint32(pos), b)
		healer.Write(byteVal, isBoundary)
		if buf := healer.Heal(); buf != nil {
			chunks = append(chunks, buf...)
		}
	}

	if buf := healer.Flush(); buf != nil {
		chunks = append(chunks, buf...)
	}

	return chunks
}

/*
buildPaths converts raw chunks into values using BuildValue.
*/
func buildPaths(chunks [][]byte) ([]data.Value, error) {
	paths := make([]data.Value, len(chunks))

	var err error
	for i, chunk := range chunks {
		paths[i], err = data.BuildValue(chunk)
		if err != nil {
			return nil, err
		}
	}

	return paths, nil
}

func TestGraph_AliceInWonderland(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	// Tokenize and build paths ONCE outside the Convey tree.
	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}
	graph := NewGraphServer()

	t.Logf("Tokenized Alice: %d chunks, %d path values", len(chunks), len(paths))

	Convey("Given Alice in Wonderland tokenized by the real Sequencer", t, func() {
		So(len(chunks), ShouldBeGreaterThan, 100)

		Convey("The system finds matching passages via real value geometry", func() {
			promptValue, _ := data.BuildValue([]byte("Alice was beginning"))
			bestIdx, lowestEnergy, _ := graph.Evaluate(promptValue, paths, nil, nil)

			So(bestIdx, ShouldBeGreaterThanOrEqualTo, 0)
			So(lowestEnergy, ShouldBeLessThan, promptValue.ActiveCount())

			t.Logf("Prompt: %q → matched chunk[%d]=%q energy=%d (prompt=%d bits)",
				"Alice was beginning", bestIdx, string(chunks[bestIdx]),
				lowestEnergy, promptValue.ActiveCount())
		})

		Convey("A stored chunk XORed with itself gives zero residue", func() {
			idx := len(chunks) / 2
			prompt, _ := data.BuildValue(chunks[idx])
			_, lowestEnergy, _ := graph.Evaluate(prompt, paths, nil, nil)

			So(lowestEnergy, ShouldEqual, 0)
		})

		Convey("Similar phrases match better than unrelated noise", func() {
			phraseA, _ := data.BuildValue([]byte("the Rabbit"))
			phraseUnrelated, _ := data.BuildValue([]byte("ZZZZZZZZZ"))

			var bestIdx int
			bestEnergy := 999

			for i := range paths {
				e := phraseA.XOR(paths[i]).ActiveCount()
				if e < bestEnergy {
					bestEnergy = e
					bestIdx = i
				}
			}

			energyUnrelated := phraseUnrelated.XOR(paths[bestIdx]).ActiveCount()

			So(bestEnergy, ShouldBeLessThan, energyUnrelated)
			t.Logf("'the Rabbit' best match: chunk[%d]=%q energy=%d, 'ZZZZZZZZZ' energy=%d",
				bestIdx, string(chunks[bestIdx]), bestEnergy, energyUnrelated)
		})

		Convey("Empty paths edge case returns -1", func() {
			prompt, _ := data.BuildValue([]byte("test"))
			bestIdx, _, _ := graph.Evaluate(prompt, nil, nil, nil)
			So(bestIdx, ShouldEqual, -1)
		})

		Convey("Value density profile is reported", func() {
			var maxDensity float64
			var maxIdx int
			var underCeiling int

			for i, path := range paths {
				d := path.ShannonDensity()

				if d <= 0.40 {
					underCeiling++
				}

				if d > maxDensity {
					maxDensity = d
					maxIdx = i
				}
			}

			So(len(paths), ShouldBeGreaterThan, 0)
			t.Logf("Shannon profile: %d/%d chunks (%.0f%%) under 40%% ceiling",
				underCeiling, len(paths), float64(underCeiling)/float64(len(paths))*100)
			t.Logf("Max density: %.1f%% at chunk[%d]=%q (%d bytes)",
				maxDensity*100, maxIdx, string(chunks[maxIdx]), len(chunks[maxIdx]))
		})

		Convey("Chunk statistics reveal structure", func() {
			var totalBits int
			var minBits, maxBits int
			minBits = 999

			for _, path := range paths {
				b := path.ActiveCount()
				totalBits += b

				if b < minBits {
					minBits = b
				}

				if b > maxBits {
					maxBits = b
				}
			}

			avgBits := totalBits / len(paths)

			So(avgBits, ShouldBeGreaterThan, 5)
			So(maxBits, ShouldBeGreaterThan, avgBits)
			So(minBits, ShouldBeLessThan, avgBits)
			t.Logf("Value density: min=%d avg=%d max=%d across %d chunks",
				minBits, avgBits, maxBits, len(paths))
		})
	})
}

func BenchmarkGraph_Alice(b *testing.B) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		b.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		b.Fatalf("buildPaths failed: %v", err)
	}
	graph := NewGraphServer()

	prompt, _ := data.BuildValue([]byte("Alice was beginning"))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		graph.Evaluate(prompt, paths, nil, nil)
	}
}

func BenchmarkGraph_Evaluate_Scaling(b *testing.B) {
	graph := NewGraphServer()

	paths := make([]data.Value, 100_000)
	for i := range paths {
		c, _ := data.BuildValue([]byte(fmt.Sprintf("chunk_%d", i%1000)))
		paths[i] = c
	}

	prompt, _ := data.BuildValue([]byte("test query"))

	for _, size := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("%d_paths", size), func(b *testing.B) {
			subset := paths[:size]
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				graph.Evaluate(prompt, subset, nil, nil)
			}
		})
	}
}

// ── Mathematical Explorations ─────────────────────────────────────────
//
// These tests explore properties of the value algebra.
// Each test includes a null hypothesis to guard against tautologies.

/*
TestBaseValueMinimumDistance measures the actual minimum Hamming distance
between all 256²/2 BaseValue pairs. No oracle — exhaustive enumeration.
*/
func TestBaseValueMinimumDistance(t *testing.T) {
	Convey("Given all 256 BaseValues", t, func() {
		Convey("Measure pairwise Hamming distances exhaustively", func() {
			minDist := 999
			maxDist := 0
			collisionPairs := 0
			totalSharedBits := 0

			for a := 0; a < config.Numeric.VocabSize; a++ {
				for b := a + 1; b < config.Numeric.VocabSize; b++ {
					ca := data.BaseValue(byte(a))
					cb := data.BaseValue(byte(b))

					shared := ca.Similarity(cb)
					dist := ca.XOR(cb).ActiveCount()

					if shared > 0 {
						collisionPairs++
						totalSharedBits += shared
					}

					if dist < minDist {
						minDist = dist
					}

					if dist > maxDist {
						maxDist = dist
					}
				}
			}

			totalPairs := config.Numeric.VocabSize * (config.Numeric.VocabSize - 1) / 2
			t.Logf("BaseValue code: d_min=%d  d_max=%d", minDist, maxDist)
			t.Logf("Collision pairs: %d/%d (%.1f%%)",
				collisionPairs, totalPairs, float64(collisionPairs)/float64(totalPairs)*100)

			if collisionPairs > 0 {
				t.Logf("Mean shared bits in colliding pairs: %.2f",
					float64(totalSharedBits)/float64(collisionPairs))
			}

			// Report the actual d_min factually.
			So(minDist, ShouldBeGreaterThan, 0)
		})

		Convey("Measure per-byte active bit counts", func() {
			counts := make(map[int]int)

			for b := 0; b < config.Numeric.VocabSize; b++ {
				c := data.BaseValue(byte(b))
				n := c.ActiveCount()
				counts[n]++
				So(n, ShouldBeGreaterThan, 0)
			}

			t.Logf("Bit count distribution: %v", counts)
		})
	})
}

/*
TestThreeWayDecomposition verifies pure algebraic identities.
These MUST hold by definition of AND, XOR, and ANDNOT.
The test confirms the implementation matches the math.
*/
func TestThreeWayDecomposition(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given pairs of Alice values", t, func() {
		prompt, _ := data.BuildValue([]byte("Alice was beginning"))

		for trial := range 5 {
			idx := trial * len(paths) / 5
			stored := paths[idx]

			shared := prompt.AND(stored)
			promptOnly := prompt.Hole(stored)
			storedOnly := stored.Hole(prompt)
			residue := prompt.XOR(stored)

			Convey(fmt.Sprintf("Trial %d: chunk[%d]", trial, idx), func() {

				Convey("|Shared| + |PromptOnly| == |P|", func() {
					So(shared.ActiveCount()+promptOnly.ActiveCount(), ShouldEqual, prompt.ActiveCount())
				})

				Convey("|Shared| + |StoredOnly| == |S|", func() {
					So(shared.ActiveCount()+storedOnly.ActiveCount(), ShouldEqual, stored.ActiveCount())
				})

				Convey("|PromptOnly| + |StoredOnly| == |Residue|", func() {
					So(promptOnly.ActiveCount()+storedOnly.ActiveCount(), ShouldEqual, residue.ActiveCount())
				})

				Convey("|Shared| == (|P| + |S| - |R|) / 2", func() {
					derived := (prompt.ActiveCount() + stored.ActiveCount() - residue.ActiveCount()) / 2
					So(derived, ShouldEqual, shared.ActiveCount())
				})

				t.Logf("  |P|=%d |S|=%d shared=%d pOnly=%d sOnly=%d residue=%d",
					prompt.ActiveCount(), stored.ActiveCount(),
					shared.ActiveCount(), promptOnly.ActiveCount(),
					storedOnly.ActiveCount(), residue.ActiveCount())
			})
		}
	})
}

/*
TestDistanceMetricRankings compares how Hamming, Jaccard, Containment,
and Cosine rank the same set of stored values against a prompt.

Reports: do the metrics produce the same or different top-k rankings?
No assertions on quality — just factual rank comparison.
*/
func TestDistanceMetricRankings(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given four distance metrics applied to the same data", t, func() {
		prompt, _ := data.BuildValue([]byte("the Rabbit"))
		pActive := prompt.ActiveCount()

		type scored struct {
			idx         int
			hamming     int
			jaccard     float64
			containment float64
			cosine      float64
		}

		results := make([]scored, len(paths))

		for i, path := range paths {
			shared := prompt.Similarity(path)
			sActive := path.ActiveCount()
			hammingDist := prompt.XOR(path).ActiveCount()
			union := pActive + sActive - shared

			jac := 0.0
			if union > 0 {
				jac = float64(shared) / float64(union)
			}

			cont := 0.0
			if pActive > 0 {
				cont = float64(shared) / float64(pActive)
			}

			cos := 0.0
			if pActive > 0 && sActive > 0 {
				cos = float64(shared) / math.Sqrt(float64(pActive)*float64(sActive))
			}

			results[i] = scored{i, hammingDist, jac, cont, cos}
		}

		Convey("Report top-5 by each metric and measure rank agreement", func() {
			// Get top-5 indices for each metric.
			byHamming := make([]scored, len(results))
			copy(byHamming, results)
			sort.Slice(byHamming, func(i, j int) bool { return byHamming[i].hamming < byHamming[j].hamming })

			byJaccard := make([]scored, len(results))
			copy(byJaccard, results)
			sort.Slice(byJaccard, func(i, j int) bool { return byJaccard[i].jaccard > byJaccard[j].jaccard })

			byContain := make([]scored, len(results))
			copy(byContain, results)
			sort.Slice(byContain, func(i, j int) bool { return byContain[i].containment > byContain[j].containment })

			byCosine := make([]scored, len(results))
			copy(byCosine, results)
			sort.Slice(byCosine, func(i, j int) bool { return byCosine[i].cosine > byCosine[j].cosine })

			t.Log("Hamming top-5:")
			hammingTop := make(map[int]bool)
			for k := 0; k < 5; k++ {
				r := byHamming[k]
				hammingTop[r.idx] = true
				t.Logf("  [H=%3d] chunk[%d] %q  J=%.3f C=%.3f cos=%.3f |S|=%d",
					r.hamming, r.idx, string(chunks[r.idx])[:min(len(chunks[r.idx]), 40)],
					r.jaccard, r.containment, r.cosine,
					paths[r.idx].ActiveCount())
			}

			t.Log("Jaccard top-5:")
			jaccardTop := make(map[int]bool)
			for k := 0; k < 5; k++ {
				r := byJaccard[k]
				jaccardTop[r.idx] = true
				t.Logf("  [J=%.3f] chunk[%d] %q  H=%d",
					r.jaccard, r.idx, string(chunks[r.idx])[:min(len(chunks[r.idx]), 40)], r.hamming)
			}

			t.Log("Containment top-5:")
			containTop := make(map[int]bool)
			for k := 0; k < 5; k++ {
				r := byContain[k]
				containTop[r.idx] = true
				t.Logf("  [C=%.3f] chunk[%d] %q  H=%d |S|=%d",
					r.containment, r.idx, string(chunks[r.idx])[:min(len(chunks[r.idx]), 40)],
					r.hamming, paths[r.idx].ActiveCount())
			}

			// Measure overlap between Hamming and Jaccard top-5.
			hjOverlap := 0
			for idx := range hammingTop {
				if jaccardTop[idx] {
					hjOverlap++
				}
			}

			hcOverlap := 0
			for idx := range hammingTop {
				if containTop[idx] {
					hcOverlap++
				}
			}

			t.Logf("Hamming∩Jaccard top-5 overlap: %d/5", hjOverlap)
			t.Logf("Hamming∩Containment top-5 overlap: %d/5", hcOverlap)

			// No assertion on whether the metrics agree — just report.
			So(len(results), ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestAnalogyOperator tests D = A ⊕ B ⊕ C against a proper null hypothesis.

Null: D is compared against ALL stored values. We measure where the
expected target ranks. Then we repeat with a RANDOM relationship vector
(same Hamming weight as the real one). If the real analogy ranks the
target significantly higher than the random relationship does, the
analogy operator is doing something meaningful. If not, it's just
bag-of-bytes overlap.
*/
func TestAnalogyOperator(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given the analogy A:B :: C:D where D = C ⊕ (A ⊕ B)", t, func() {

		Convey("Measure whether analogy outperforms a null (random relationship)", func() {
			valueA, _ := data.BuildValue([]byte("Alice said"))
			valueB, _ := data.BuildValue([]byte("Queen said"))
			valueC, _ := data.BuildValue([]byte("Alice looked"))
			expected, _ := data.BuildValue([]byte("Queen looked"))

			// Real analogy.
			relationship := valueA.XOR(valueB)
			valueD := valueC.XOR(relationship)

			distToExpected := valueD.XOR(expected).ActiveCount()

			// Measure D's distance to expected vs D's distance to ALL stored values.
			betterThanExpected := 0
			totalPaths := len(paths)

			for _, path := range paths {
				if valueD.XOR(path).ActiveCount() <= distToExpected {
					betterThanExpected++
				}
			}

			rankPercentile := float64(betterThanExpected) / float64(totalPaths) * 100

			// Null hypothesis: use valueC directly (no relationship applied).
			// If the analogy does nothing useful, valueC itself should rank
			// "Queen looked" just as well as valueD does.
			nullDistToExpected := valueC.XOR(expected).ActiveCount()
			nullBetter := 0

			for _, path := range paths {
				if valueC.XOR(path).ActiveCount() <= nullDistToExpected {
					nullBetter++
				}
			}

			nullPercentile := float64(nullBetter) / float64(totalPaths) * 100

			t.Logf("Analogy: |A|=%d |B|=%d |relationship|=%d |shared(A,B)|=%d",
				valueA.ActiveCount(), valueB.ActiveCount(),
				relationship.ActiveCount(), valueA.Similarity(valueB))
			t.Logf("D = C ⊕ (A ⊕ B): |D⊕expected|=%d", distToExpected)
			t.Logf("Analogy rank: %d/%d paths closer (top %.1f%%)",
				betterThanExpected, totalPaths, rankPercentile)
			t.Logf("Null (C alone): |C⊕expected|=%d, rank: %d/%d (top %.1f%%)",
				nullDistToExpected, nullBetter, totalPaths, nullPercentile)

			if rankPercentile < nullPercentile {
				t.Logf("RESULT: Analogy improved rank by %.1f percentage points", nullPercentile-rankPercentile)
			} else {
				t.Logf("RESULT: Analogy did NOT improve rank (%.1f vs %.1f)", rankPercentile, nullPercentile)
			}

			// Only assert that the test ran. The log output tells the truth.
			So(totalPaths, ShouldBeGreaterThan, 0)
		})

		Convey("Report byte-level confound: how much of the analogy is just shared substrings", func() {
			// "Alice said" and "Queen said" share bytes: {' ', 's', 'a', 'i', 'd'}
			// "Alice looked" and "Queen looked" share bytes: {' ', 'l', 'o', 'k', 'e', 'd'}
			// How many BaseValue bits do the shared bytes contribute?
			sharedBytes := []byte{' ', 's', 'a', 'i', 'd'}
			sharedValue, _ := data.BuildValue(sharedBytes)

			valueA, _ := data.BuildValue([]byte("Alice said"))
			valueB, _ := data.BuildValue([]byte("Queen said"))

			t.Logf("Shared substring ' said' contributes %d bits out of |A|=%d, |B|=%d",
				sharedValue.ActiveCount(), valueA.ActiveCount(), valueB.ActiveCount())
			t.Logf("Shared fraction of A: %.0f%%  of B: %.0f%%",
				float64(sharedValue.Similarity(valueA))/float64(valueA.ActiveCount())*100,
				float64(sharedValue.Similarity(valueB))/float64(valueB.ActiveCount())*100)

			So(sharedValue.ActiveCount(), ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestSuccessiveCancellation measures what successive XOR-and-match
actually produces. Reports honestly whether matched chunks are
semantically related or just share bytes.
*/
func TestSuccessiveCancellation(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}
	graph := NewGraphServer()

	Convey("Given iterative XOR-and-match on a prompt", t, func() {
		prompt, _ := data.BuildValue([]byte("Alice found the rabbit in the garden"))
		startEnergy := prompt.ActiveCount()

		Convey("Track residue energy across steps", func() {
			residue := prompt
			usedIndices := make(map[int]bool)

			t.Logf("Start: |prompt|=%d bits", startEnergy)

			energies := []int{startEnergy}

			for step := 0; step < 5; step++ {
				bestIdx, matchEnergy, newResidue := graph.Evaluate(residue, paths, nil, nil)

				if bestIdx < 0 || usedIndices[bestIdx] {
					t.Logf("Step %d: no new match", step)
					break
				}

				usedIndices[bestIdx] = true
				newEnergy := newResidue.ActiveCount()
				energies = append(energies, newEnergy)

				// Report the match honestly — no claim about semantic relevance.
				t.Logf("Step %d: matched chunk[%d]=%q  matchDist=%d  residue=%d→%d",
					step, bestIdx, string(chunks[bestIdx])[:min(len(chunks[bestIdx]), 40)],
					matchEnergy, energies[len(energies)-2], newEnergy)
			}

			// Report: did energy decrease overall?
			finalEnergy := energies[len(energies)-1]
			t.Logf("Energy trajectory: %v", energies)
			t.Logf("Overall: %d → %d (%.0f%% reduction)",
				startEnergy, finalEnergy,
				100*(1-float64(finalEnergy)/float64(startEnergy)))

			// Only assert the test ran.
			So(len(energies), ShouldBeGreaterThan, 1)
		})
	})
}

/*
TestTopKSharedCore measures whether the AND of top-k matches
retains more bits than the AND of k RANDOM values.

If the shared core of top-k is no larger than the shared core
of random values, then the top-k overlap is just a statistical
artifact of any dense enough binary vectors.
*/
func TestTopKSharedCore(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given top-k matches vs k random values", t, func() {
		prompt, _ := data.BuildValue([]byte("the Rabbit"))
		k := 10

		// Sort all paths by distance to prompt.
		type match struct {
			idx    int
			energy int
		}

		matches := make([]match, len(paths))
		for i, path := range paths {
			matches[i] = match{i, prompt.XOR(path).ActiveCount()}
		}

		sort.Slice(matches, func(i, j int) bool { return matches[i].energy < matches[j].energy })

		Convey("Compare shared core size: top-k vs random-k vs null expectation", func() {
			// Top-k core.
			topCore := paths[matches[0].idx]
			for rank := 1; rank < k; rank++ {
				topCore = topCore.AND(paths[matches[rank].idx])
			}

			topCore.Sanitize()
			topCoreSize := topCore.ActiveCount()

			// Random-k core: pick k values from the middle of the ranking.
			midStart := len(paths) / 2
			randCore := paths[midStart]

			for rank := 1; rank < k; rank++ {
				randCore = randCore.AND(paths[midStart+rank])
			}

			randCore.Sanitize()
			randCoreSize := randCore.ActiveCount()

			// How much of top-k core overlaps with the prompt?
			topOverlap := topCore.Similarity(prompt)

			t.Logf("Top-%d shared core: %d bits (%d overlap with prompt of %d)",
				k, topCoreSize, topOverlap, prompt.ActiveCount())
			t.Logf("Random-%d shared core: %d bits", k, randCoreSize)

			if topCoreSize > randCoreSize {
				t.Logf("RESULT: Top-k core (%d) > random core (%d) — top-k shares genuine structure",
					topCoreSize, randCoreSize)
			} else {
				t.Logf("RESULT: Top-k core (%d) ≤ random core (%d) — no evidence of special structure",
					topCoreSize, randCoreSize)
			}

			// Report top-k chunks for inspection.
			for rank := 0; rank < k; rank++ {
				m := matches[rank]
				t.Logf("  top-%d: chunk[%d]=%q  energy=%d",
					rank+1, m.idx, string(chunks[m.idx])[:min(len(chunks[m.idx]), 40)], m.energy)
			}

			// No assertion on which is larger — let the data speak.
			So(len(paths), ShouldBeGreaterThan, k)
		})
	})
}

/*
TestContainmentVsDensity measures containment scores across prompts
of varying density, exposing the trivial-containment problem:
short prompts achieve containment=1.0 against many stored values
simply because dense values cover most bit positions.
*/
func TestContainmentVsDensity(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given prompts of increasing density", t, func() {
		prompts := []struct {
			label string
			text  string
		}{
			{"3 bytes", "she"},
			{"8 bytes", "the door"},
			{"15 bytes", "Alice was begin"},
			{"30 bytes", "Alice was beginning to get ver"},
		}

		for _, p := range prompts {
			prompt, _ := data.BuildValue([]byte(p.text))
			pActive := prompt.ActiveCount()

			perfectContainment := 0

			for _, path := range paths {
				shared := prompt.Similarity(path)
				containment := float64(shared) / float64(max(pActive, 1))

				if containment >= 1.0 {
					perfectContainment++
				}
			}

			Convey(fmt.Sprintf("Prompt %q (%s, %d bits)", p.text[:min(len(p.text), 20)], p.label, pActive), func() {
				t.Logf("  |P|=%d  perfect containment in %d/%d chunks (%.1f%%)",
					pActive, perfectContainment, len(paths),
					float64(perfectContainment)/float64(len(paths))*100)

				// The point: as prompt density increases, fewer chunks
				// achieve perfect containment. If even a 30-byte prompt
				// still gets 100% containment everywhere, the metric is
				// useless for discrimination.
				So(pActive, ShouldBeGreaterThan, 0)
			})
		}
	})
}

// ── Foundational Properties ───────────────────────────────────────────

/*
TestSaturationCurve measures how value density grows as more bytes
are OR'd in. Since OR is monotonic (can only set bits, never clear),
values must eventually saturate. The question is: how fast?

At saturation, all 257 bits are set and ALL values become identical.
This is the fundamental information-theoretic limit of OR aggregation.
*/
func TestSaturationCurve(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	Convey("Given progressively longer slices of Alice", t, func() {
		Convey("Measure density vs byte count", func() {
			_, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
			accum, _ := data.NewValue(seg)
			prevBits := 0
			saturated := -1

			// Track unique bytes seen.
			seen := make(map[byte]bool)

			steps := []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096}
			stepIdx := 0

			for pos, b := range raw {
				base := data.BaseValue(b)
				accum = accum.OR(base)
				seen[b] = true

				if stepIdx < len(steps) && pos+1 == steps[stepIdx] {
					bits := accum.ActiveCount()
					density := accum.ShannonDensity()
					newBits := bits - prevBits

					t.Logf("  bytes=%4d  unique_bytes=%3d  active_bits=%3d  density=%.3f  new_bits=%d",
						pos+1, len(seen), bits, density, newBits)

					prevBits = bits
					stepIdx++
				}

				if accum.ActiveCount() >= config.Numeric.VocabSize+1 && saturated < 0 {
					saturated = pos + 1
				}
			}

			finalBits := accum.ActiveCount()
			t.Logf("Final: %d bytes, %d unique bytes, %d/257 bits set",
				len(raw), len(seen), finalBits)

			if saturated > 0 {
				t.Logf("Full saturation at byte %d", saturated)
			} else {
				t.Logf("Never fully saturated (max %d/257)", finalBits)
			}

			// Also measure: at what unique-byte count do we see
			// diminishing returns?
			_, seg2, _ := capnp.NewMessage(capnp.SingleSegment(nil))
			fresh, _ := data.NewValue(seg2)
			uniqueCount := 0

			for b := 0; b < config.Numeric.VocabSize; b++ {
				old := fresh.ActiveCount()
				fresh = fresh.OR(data.BaseValue(byte(b)))
				gain := fresh.ActiveCount() - old
				uniqueCount++

				if uniqueCount <= 10 || uniqueCount%25 == 0 || gain == 0 {
					t.Logf("  unique_bytes=%3d  total_bits=%3d  gained=%d",
						uniqueCount, fresh.ActiveCount(), gain)
				}

				if gain == 0 {
					t.Logf("  Zero gain at unique byte %d — all bits already covered", uniqueCount)
					break
				}
			}

			So(finalBits, ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestBitUtilization measures whether the 257 bit positions are used
uniformly across all 256 BaseValues.

If some positions are "hot" (set by many bytes), they carry less
information. Ideal: each position set by ~5 bytes (since each
BaseValue sets 5 bits, total set-events = 256*5 = 1280, across
257 positions → ~4.98 per position).

But collisions change this. Measure the actual distribution.
*/
func TestBitUtilization(t *testing.T) {
	Convey("Given all 256 BaseValues", t, func() {
		Convey("Measure per-position usage frequency", func() {
			freq := make([]int, config.Numeric.VocabSize+1)

			for b := 0; b < config.Numeric.VocabSize; b++ {
				c := data.BaseValue(byte(b))

				for pos := 0; pos < config.Numeric.VocabSize+1; pos++ {
					if c.Has(pos) {
						freq[pos]++
					}
				}
			}

			// Statistics.
			minFreq := 999
			maxFreq := 0
			totalSet := 0
			zeroPositions := 0

			for _, f := range freq {
				totalSet += f

				if f < minFreq {
					minFreq = f
				}

				if f > maxFreq {
					maxFreq = f
				}

				if f == 0 {
					zeroPositions++
				}
			}

			mean := float64(totalSet) / 257.0

			var variance float64
			for _, f := range freq {
				diff := float64(f) - mean
				variance += diff * diff
			}

			variance /= 257.0
			stddev := math.Sqrt(variance)

			t.Logf("Total set-events: %d (expected 256 × active_bits_per_byte)", totalSet)
			t.Logf("Per-position: min=%d max=%d mean=%.2f stddev=%.2f",
				minFreq, maxFreq, mean, stddev)
			t.Logf("Zero-frequency positions: %d/257", zeroPositions)

			// Bucket the frequencies.
			buckets := make(map[int]int)
			for _, f := range freq {
				buckets[f]++
			}

			t.Logf("Frequency distribution: %v", buckets)

			So(zeroPositions, ShouldBeLessThan, 257)
		})
	})
}

/*
TestResidueRecovery tests the fundamental question: given a known prefix
and a known full sequence, can we recover the continuation from the residue?

Math: full = OR(all bytes), prefix = OR(prefix bytes)

	residue = full XOR prefix = ValueHole(full, prefix) ∪ ValueHole(prefix, full)

Since prefix ⊆ full (by construction): ValueHole(prefix, full) = ∅
So: residue = ValueHole(full, prefix) = bits in full not in prefix

But the actual continuation value = OR(continuation bytes), which includes
bits from bytes shared with the prefix. Those shared bits are invisible
in the residue. This test measures exactly how many bits are lost.
*/
func TestResidueRecovery(t *testing.T) {
	Convey("Given prefix + continuation = full sequence", t, func() {
		cases := []struct {
			full   string
			prefix string
			cont   string
		}{
			{"the quick brown fox", "the quick ", "brown fox"},
			{"Alice went to the kitchen", "Alice went to ", "the kitchen"},
			{"she said hello world", "she said ", "hello world"},
			{"abcdefghij", "abcde", "fghij"},
		}

		for _, tc := range cases {
			Convey(fmt.Sprintf("Full=%q prefix=%q cont=%q", tc.full, tc.prefix, tc.cont), func() {
				fullValue, _ := data.BuildValue([]byte(tc.full))
				prefixValue, _ := data.BuildValue([]byte(tc.prefix))
				contValue, _ := data.BuildValue([]byte(tc.cont))

				// Verify prefix ⊆ full.
				prefixInFull := prefixValue.Similarity(fullValue)
				prefixIsSubset := prefixInFull == prefixValue.ActiveCount()

				// Compute residue.
				residue := fullValue.XOR(prefixValue)

				// When prefix ⊆ full, XOR = ValueHole(full, prefix).
				hole := fullValue.Hole(prefixValue)
				holeEqualsResidue := hole.XOR(residue).ActiveCount() == 0

				// Compare residue to actual continuation value.
				residueBits := residue.ActiveCount()
				contBits := contValue.ActiveCount()
				overlapResidCont := residue.Similarity(contValue)

				// How many continuation bits are missing from the residue?
				// These are bits set by bytes shared between prefix and continuation.
				missingBits := contBits - overlapResidCont

				// Count shared bytes explicitly.
				prefixBytes := make(map[byte]bool)
				for _, b := range []byte(tc.prefix) {
					prefixBytes[b] = true
				}

				sharedByteCount := 0
				for _, b := range []byte(tc.cont) {
					if prefixBytes[b] {
						sharedByteCount++
					}
				}

				// Unique byte values shared.
				prefixSet := make(map[byte]bool)
				for _, b := range []byte(tc.prefix) {
					prefixSet[b] = true
				}

				contSet := make(map[byte]bool)
				for _, b := range []byte(tc.cont) {
					contSet[b] = true
				}

				sharedValues := 0
				for b := range contSet {
					if prefixSet[b] {
						sharedValues++
					}
				}

				t.Logf("  prefix⊆full: %v  hole==residue: %v", prefixIsSubset, holeEqualsResidue)
				t.Logf("  |residue|=%d  |cont|=%d  overlap=%d  missing=%d",
					residueBits, contBits, overlapResidCont, missingBits)
				t.Logf("  Shared byte values between prefix and cont: %d", sharedValues)
				t.Logf("  Recovery rate: %d/%d = %.0f%%",
					overlapResidCont, contBits,
					float64(overlapResidCont)/float64(max(contBits, 1))*100)

				Convey("When prefix ⊆ full, residue should equal ValueHole", func() {
					if prefixIsSubset {
						So(holeEqualsResidue, ShouldBeTrue)
					}
				})

				Convey("Missing bits correlate with shared byte values", func() {
					if sharedValues == 0 && missingBits == 0 {
						t.Log("  → No shared bytes and no missing bits: perfect recovery")
					} else if sharedValues == 0 && missingBits > 0 {
						// BaseValue collisions: distinct bytes share bit positions
						// because d_min = 6 (coprime spreading has overlaps).
						t.Logf("  → No shared bytes but %d missing bits: BaseValue bit collisions", missingBits)
					} else {
						t.Logf("  → %d shared byte values cause %d missing bits", sharedValues, missingBits)
					}

					So(missingBits, ShouldBeGreaterThanOrEqualTo, 0)
				})
			})
		}
	})
}

/*
TestOrderInvariance confirms that OR aggregation is order-insensitive,
then measures how many distinct Alice chunks collapse to the same value.

The collision rate reveals how much information OR-aggregation destroys:
if many distinct chunks produce the same value, the value space is too
coarse for disambiguation.
*/
func TestOrderInvariance(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given Alice chunks", t, func() {

		Convey("Permutations of the same bytes must produce the same value", func() {
			original := []byte("Alice")
			reversed := []byte("ecilA")
			sorted := []byte("Aceil")

			valueOrig, _ := data.BuildValue(original)
			valueRev, _ := data.BuildValue(reversed)
			valueSort, _ := data.BuildValue(sorted)

			distOrigRev := valueOrig.XOR(valueRev).ActiveCount()
			distOrigSort := valueOrig.XOR(valueSort).ActiveCount()

			t.Logf("  'Alice' vs 'ecilA': distance=%d", distOrigRev)
			t.Logf("  'Alice' vs 'Aceil': distance=%d", distOrigSort)

			So(distOrigRev, ShouldEqual, 0)
			So(distOrigSort, ShouldEqual, 0)
		})

		Convey("Different byte SETS must produce different values", func() {
			a, _ := data.BuildValue([]byte("abc"))
			b, _ := data.BuildValue([]byte("abd"))

			dist := a.XOR(b).ActiveCount()
			t.Logf("  'abc' vs 'abd': distance=%d", dist)
			So(dist, ShouldBeGreaterThan, 0)
		})

		Convey("Same byte set, different lengths must produce the same value", func() {
			// "aab" has unique bytes {a, b}, same as "ab"
			short, _ := data.BuildValue([]byte("ab"))
			long, _ := data.BuildValue([]byte("aab"))

			dist := short.XOR(long).ActiveCount()
			t.Logf("  'ab' vs 'aab': distance=%d", dist)
			So(dist, ShouldEqual, 0)
		})

		Convey("Measure chunk-to-value collision rate in Alice", func() {
			// Key = value fingerprint (C0..C4 concatenated), value = chunk indices.
			type valueKey struct {
				c0, c1, c2, c3, c4 uint64
			}

			groups := make(map[valueKey][]int)

			for i, path := range paths {
				key := valueKey{path.C0(), path.C1(), path.C2(), path.C3(), path.C4()}
				groups[key] = append(groups[key], i)
			}

			uniqueValues := len(groups)
			totalChunks := len(chunks)
			collisions := totalChunks - uniqueValues

			t.Logf("  %d chunks → %d unique values (%d collisions, %.1f%%)",
				totalChunks, uniqueValues, collisions,
				float64(collisions)/float64(totalChunks)*100)

			// Report some collision groups.
			reported := 0
			for _, indices := range groups {
				if len(indices) > 1 && reported < 5 {
					strs := make([]string, len(indices))
					for j, idx := range indices {
						s := string(chunks[idx])
						if len(s) > 25 {
							s = s[:25]
						}
						strs[j] = fmt.Sprintf("[%d]%q", idx, s)
					}

					t.Logf("  Collision group (%d chunks): %v", len(indices), strs)
					reported++
				}
			}

			So(uniqueValues, ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestDumpAliceLabels writes a log file showing every unique value
produced during the Alice breakdown, the text chunks grouped under
each value (= label), and each label's nearest neighbors.
*/
func TestDumpAliceLabels(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}

	Convey("Given Alice tokenized into chunks", t, func() {
		Convey("Dump label map to log file", func() {
			type valueKey struct {
				c0, c1, c2, c3, c4 uint64
			}

			type label struct {
				key     valueKey
				value   data.Value
				indices []int
				bits    int
			}

			groups := make(map[valueKey]*label)
			keyOrder := []valueKey{}

			for i, path := range paths {
				key := valueKey{path.C0(), path.C1(), path.C2(), path.C3(), path.C4()}

				if existing, ok := groups[key]; ok {
					existing.indices = append(existing.indices, i)
				} else {
					groups[key] = &label{
						key:     key,
						value:   path,
						indices: []int{i},
						bits:    path.ActiveCount(),
					}
					keyOrder = append(keyOrder, key)
				}
			}

			sort.Slice(keyOrder, func(i, j int) bool {
				li := groups[keyOrder[i]]
				lj := groups[keyOrder[j]]

				if len(li.indices) != len(lj.indices) {
					return len(li.indices) > len(lj.indices)
				}

				return li.bits < lj.bits
			})

			uniqueValues := make([]data.Value, len(keyOrder))
			for i, key := range keyOrder {
				uniqueValues[i] = groups[key].value
			}

			f, err := os.Create(t.TempDir() + "/alice_labels.log")
			So(err, ShouldBeNil)
			defer f.Close()

			fmt.Fprintf(f, "ALICE IN WONDERLAND — CHORD LABEL MAP\n")
			fmt.Fprintf(f, "======================================\n")
			fmt.Fprintf(f, "Total chunks: %d\n", len(chunks))
			fmt.Fprintf(f, "Unique labels: %d\n", len(keyOrder))
			fmt.Fprintf(f, "Collision rate: %.1f%%\n\n",
				float64(len(chunks)-len(keyOrder))/float64(len(chunks))*100)

			for rank, key := range keyOrder {
				lbl := groups[key]

				fmt.Fprintf(f, "────────────────────────────────────────\n")
				fmt.Fprintf(f, "LABEL #%d  |  %d bits  |  %d chunk(s)\n",
					rank+1, lbl.bits, len(lbl.indices))
				fmt.Fprintf(f, "────────────────────────────────────────\n")

				fmt.Fprintf(f, "  Chunks:\n")
				for _, idx := range lbl.indices {
					text := string(chunks[idx])
					if len(text) > 60 {
						text = text[:60] + "…"
					}

					fmt.Fprintf(f, "    [%4d] %q\n", idx, text)
				}

				if len(lbl.indices) > 0 {
					sample := chunks[lbl.indices[0]]
					uniqueBytes := make(map[byte]bool)

					for _, b := range sample {
						uniqueBytes[b] = true
					}

					byteList := make([]byte, 0, len(uniqueBytes))
					for b := range uniqueBytes {
						byteList = append(byteList, b)
					}

					sort.Slice(byteList, func(i, j int) bool { return byteList[i] < byteList[j] })

					readable := ""
					for _, b := range byteList {
						if b >= 32 && b < 127 {
							readable += string(b)
						} else {
							readable += fmt.Sprintf("\\x%02x", b)
						}
					}

					fmt.Fprintf(f, "  Byte set: {%s}\n", readable)
				}

				if len(lbl.indices) > 1 {
					core := paths[lbl.indices[0]]

					for _, idx := range lbl.indices[1:] {
						core = core.AND(paths[idx])
					}

					core.Sanitize()
					fmt.Fprintf(f, "  Shared core: %d bits (all chunks share these)\n", core.ActiveCount())
				}

				fmt.Fprintf(f, "  Nearest neighbor labels:\n")

				type neighbor struct {
					rank int
					dist int
					text string
				}

				neighbors := make([]neighbor, 0, len(uniqueValues))

				for j, uc := range uniqueValues {
					if j == rank {
						continue
					}

					dist := lbl.value.XOR(uc).ActiveCount()
					other := groups[keyOrder[j]]
					text := string(chunks[other.indices[0]])

					if len(text) > 40 {
						text = text[:40]
					}

					neighbors = append(neighbors, neighbor{j, dist, text})
				}

				sort.Slice(neighbors, func(i, j int) bool {
					return neighbors[i].dist < neighbors[j].dist
				})

				shown := min(3, len(neighbors))
				for k := 0; k < shown; k++ {
					nn := neighbors[k]
					fmt.Fprintf(f, "    dist=%2d → label #%d %q\n", nn.dist, nn.rank+1, nn.text)
				}

				fmt.Fprintf(f, "\n")
			}

			t.Logf("Wrote %d labels to alice_labels.log", len(keyOrder))
			So(len(keyOrder), ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestGraphResidueDepletion tests the geometric reasoning process by systematically
depleting a prompt's valueal energy using branches from the LSM.
1. Fills the simulated LSM with Alice in Wonderland.
2. Takes half a sentence as a prompt.
3. Finds matching branches coming off that span.
4. Uses Matrix to evaluate and ValueHole to strictly remove shared components.
5. Checks what is left when it can no longer be reduced.
*/
func TestGraphResidueDepletion(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	// 1. Let the tokenizer (Sequencer) run and build paths (the LSM contents)
	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}
	graph := NewGraphServer()

	Convey("Given half a sentence from the text", t, func() {
		// 2. Take half a sentence
		text := "Alice was beginning to get very tired of sitting by her sister on the bank"
		halfSentence := text[:len(text)/2]
		prompt, _ := data.BuildValue([]byte(halfSentence))
		startBits := prompt.ActiveCount()

		Convey("Keep removing shared components with LSM branches until depleted", func() {
			// 3. Find it in the LSM, get all the branches that come off that span.
			// Emulate finding branches by identifying paths that share similarity
			var branches []data.Value
			for _, path := range paths {
				if prompt.Similarity(path) > 3 { // Threshold to be a valid branch
					branches = append(branches, path)
				}
			}

			// 4. Put it all in the graph Matrix
			// 5. Use data/value operations and keep removing shared components
			residue := prompt
			steps := 0

			t.Logf("Initial residue: %d bits -> Text: %q", startBits, halfSentence)

			for {
				// Matrix evaluation finds the lowest XOR energy path
				bestIdx, matchEnergy, _ := graph.Evaluate(residue, branches, nil, nil)

				if bestIdx == -1 {
					break
				}

				match := branches[bestIdx]

				// Use ValueHole to strictly remove shared components (Target AND NOT Match).
				// This avoids the XOR issue of adding new bits, doing exactly what
				// "removing shared components" demands.
				nextResidue := residue.Hole(match)

				if nextResidue.ActiveCount() == residue.ActiveCount() {
					// We didn't manage to remove any bits this time
					break
				}

				t.Logf("Step %d: removed %d shared bits via branch match (Evaluate energy=%d)",
					steps+1, residue.ActiveCount()-nextResidue.ActiveCount(), matchEnergy)

				residue = nextResidue
				steps++

				if residue.ActiveCount() == 0 {
					break
				}

				// Remove the branch so we don't find it again continuously
				branches = append(branches[:bestIdx], branches[bestIdx+1:]...)
			}

			// 6. Check what is left
			endBits := residue.ActiveCount()
			t.Logf("Final run result: depleted from %d to %d bits in %d steps", startBits, endBits, steps)

			if endBits > 0 {
				t.Logf("Remaining Unexplainable Residue: %d bits", endBits)
			} else {
				t.Logf("Residue completely explained and depleted by branches!")
			}

			// Validate we reduced the complexity
			So(endBits, ShouldBeLessThan, startBits)
		})
	})
}
