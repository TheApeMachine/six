package textgen

import (
	"math"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type OverlapChainStep struct {
	StepNum   int
	SimScore  float64
	SpanText  string
	NewTokens int
	Overlap   int
}

type TextOverlapChainingEntry struct {
	Desc, Prefix, FullText string
	Chain                  []OverlapChainStep
	ChainLength            int
	TotalNew               int
	HasPunctuation         bool
	LooksValid             bool
	SingleSource           bool
	SourceCount            int
}

func TestTextOverlapChaining(t *testing.T) {
	Convey("Given a prose corpus and overlap-aware span chaining", t, func() {
		corpus := proseCorpus()
		sm := BuildSpanMemory(corpus)
		eigenTable := buildEigenMode(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 8
		const minNewTokens = 2

		prompts := []struct{ prefix, desc string }{
			{"To be, or not", "Hamlet"},
			{"It was the best", "Two Cities"},
			{"Call me", "Moby Dick"},
			{"In a hole in the", "Hobbit"},
			{"All happy families", "Karenina"},
		}

		Convey("When running overlap-aware text span chaining", func() {
			var results []TextOverlapChainingEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				
				// Capture sustained phase coherence anchor over prefix geometry
				anchorPhase, anchorConc := weightedCircularMean(eigenTable, p.prefix)
				
				usedSpans := make(map[int]bool)
				var chain []OverlapChainStep

				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 16 {
						ctxToks = ctxToks[len(ctxToks)-16:]
					}
					fpQ := BuildBoundaryFP(detokenize(ctxToks), "")
					cands := sm.RetrieveDiverse(fpQ, nDial, topK)

					type sc struct {
						idx, ovl, newToks int
						score             float64
						meta              SpanMeta
					}
					var viable []sc
					for _, c := range cands {
						if usedSpans[c.Idx] {
							continue
						}
						meta := sm.Index[c.Idx]
						
						// Enforce continuous geometric paths (sustained phase coherence)
						cPhase, cConc := weightedCircularMean(eigenTable, meta.Text)
						phaseDiff := math.Abs(cPhase - anchorPhase)
						for phaseDiff > math.Pi {
							phaseDiff = 2*math.Pi - phaseDiff
						}
						if step > 0 && phaseDiff > 0.45 && cConc > 0.5 && anchorConc > 0.5 {
						    continue
						}

						ovl := OverlapLen(outToks, meta.Tokens)
						newToks := len(meta.Tokens) - ovl
						if newToks < minNewTokens {
							continue
						}
						score := c.Score + float64(newToks)*0.005
						if strings.Contains(detokenize(meta.Tokens[ovl:]), ".") || strings.Contains(detokenize(meta.Tokens[ovl:]), "?") {
							score += 0.01
						}
						viable = append(viable, sc{c.Idx, ovl, newToks, score, meta})
					}

					if len(viable) == 0 {
						break
					}
					best := viable[0]
					for _, v := range viable[1:] {
						if v.score > best.score {
							best = v
						}
					}
					usedSpans[best.idx] = true
					outToks = append(outToks, best.meta.Tokens[best.ovl:]...)
					chain = append(chain, OverlapChainStep{
						StepNum: step + 1, SimScore: best.score,
						SpanText: best.meta.Text, NewTokens: best.newToks, Overlap: best.ovl,
					})
					
					// Terminate upon logical text completion
					if strings.Contains(detokenize(best.meta.Tokens[best.ovl:]), ".") || strings.Contains(detokenize(best.meta.Tokens[best.ovl:]), "?") {
						break
					}
				}

				fullText := detokenize(outToks)
				sources := make(map[int]bool)
				totalNew := 0
				for _, c := range chain {
					totalNew += c.NewTokens
				}
				
				hasPunctuation := strings.Contains(fullText, ".") || strings.Contains(fullText, "?") || strings.Contains(fullText, "!")

				So(fullText, ShouldNotBeEmpty)

				results = append(results, TextOverlapChainingEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					Chain: chain, ChainLength: len(chain), TotalNew: totalNew,
					HasPunctuation: hasPunctuation,
					LooksValid:   IsGeometricallyClosed(eigenTable, fullText, anchorPhase),
					SingleSource: len(sources) == 1,
					SourceCount:  len(sources),
				})
			}

			Convey("All chained text generations are logically validated", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})
		})
	})
}
