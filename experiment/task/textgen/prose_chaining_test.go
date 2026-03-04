package textgen

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type ChainedSpan struct {
	Step      int
	Text      string
	Length    int
	SimScore  float64
	SourceIdx int
}

type TextChainingEntry struct {
	Desc, Prefix, FullText string
	Chain                  []ChainedSpan
	ChainLength            int
	HasPunctuation         bool
	LooksValid             bool
	SourceCount            int
}

func TestTextChaining(t *testing.T) {
	Convey("Given a prose corpus and a span memory", t, func() {
		corpus := proseCorpus()
		sm := BuildSpanMemory(corpus)
		eigenTable := buildEigenMode(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 6

		prompts := []struct{ prefix, desc string }{
			{"To be, or not", "Hamlet"},
			{"It was the best", "Two Cities"},
			{"Call me", "Moby Dick"},
			{"In a hole in the", "Hobbit"},
			{"All happy families", "Karenina"},
		}

		Convey("When chaining spans for each text prompt", func() {
			var results []TextChainingEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				anchorPhase, _ := weightedCircularMean(eigenTable, p.prefix)
				usedSpans := make(map[int]bool)
				var chain []ChainedSpan
				
				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 16 {
						ctxToks = ctxToks[len(ctxToks)-16:]
					}
					fpBoundary := BuildBoundaryFP(detokenize(ctxToks), "")
					cands := sm.RetrieveDiverse(fpBoundary, nDial, topK)

					bestIdx, bestScore := -1, -1.0
					for _, c := range cands {
						if usedSpans[c.Idx] {
							continue
						}
						if c.Score > bestScore {
							bestScore, bestIdx = c.Score, c.Idx
						}
					}
					if bestIdx < 0 {
						break
					}
					usedSpans[bestIdx] = true
					span := sm.Index[bestIdx]
					outToks = append(outToks, span.Tokens...)
					newText := span.Text
					chain = append(chain, ChainedSpan{
						Step: step + 1, Text: newText,
						Length: len(span.Tokens), SimScore: bestScore, SourceIdx: span.Source,
					})
					if (strings.Contains(newText, ".") || strings.Contains(newText, "?")) && step > 0 {
						break
					}
				}

				fullText := detokenize(outToks)
				sources := make(map[int]bool)
				for _, c := range chain {
					sources[c.SourceIdx] = true
				}
				
				hasPunctuation := strings.Contains(fullText, ".") || strings.Contains(fullText, "?") || strings.Contains(fullText, "!")

				So(fullText, ShouldNotBeEmpty)

				results = append(results, TextChainingEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					Chain: chain, ChainLength: len(chain),
					HasPunctuation: hasPunctuation,
					LooksValid:  IsGeometricallyClosed(eigenTable, fullText, anchorPhase),
					SourceCount: len(sources),
				})
			}

			Convey("All chained outputs are non-empty and mathematically evaluated", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})
		})
	})
}
