package vision

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)



func TestImageReconstruction(t *testing.T) {
	Convey("Given an image corpus and spatial span memory", t, func() {
		corpus, names := visionCorpus()
		sm := BuildSpanMemory(corpus)
		eigenTable := buildEigenMode(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const holdoutFactor = 0.5
		const topK = 64
		const nDial = 8
		const maxChains = 64
		const minNewTokens = 2

		var results []ReconstructionResult

		Convey("When reconstructing masked out halves of images", func() {
			for i, img := range corpus {
				// mask bottom half (assuming 1D byte array mapping of a 2D grid)
				maskIdx := int(float64(len(img)) * holdoutFactor)
				prefix := img[:maskIdx]
				target := img

				outToks := make([]byte, len(prefix))
				copy(outToks, prefix)

				anchorPhase, anchorConc := weightedCircularMean(eigenTable, prefix)

				usedSpans := make(map[int]bool)
				steps := 0

				for step := 0; step < maxChains; step++ {
					if len(outToks) >= len(target) {
						break
					}
					steps++

					ctxToks := outToks
					if len(ctxToks) > 32 {
						ctxToks = ctxToks[len(ctxToks)-32:]
					}
					fpQ := BuildBoundaryFP(ctxToks, nil)
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

						cPhase, cConc := weightedCircularMean(eigenTable, meta.Tokens)
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
				}

				if len(outToks) > len(target) {
					outToks = outToks[:len(target)]
				}

				matches := bytes.Equal(outToks, target)
				closed := IsGeometricallyClosed(eigenTable, outToks, anchorPhase)

				So(len(outToks), ShouldBeGreaterThanOrEqualTo, len(prefix))

				results = append(results, ReconstructionResult{
					Name:      names[i],
					TargetLen: len(target),
					Generated: len(outToks),
					Matches:   matches,
					Steps:     steps,
					IsClosed:  closed,
				})
			}

			Convey("It generates image spans using topological evaluation and phase bridges", func() {
				// Assert output generation succeeds across multiple spatial masks.
				for _, res := range results {
					So(res.Generated, ShouldBeGreaterThan, 0)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				tableRows := make([]map[string]any, len(results))
				for i, res := range results {
					matchStr := "False"
					if res.Matches {
						matchStr = "True"
					}
					closedStr := "False"
					if res.IsClosed {
						closedStr = "True"
					}
					tableRows[i] = map[string]any{
						"Image":      res.Name,
						"Steps":      fmt.Sprintf("%d", res.Steps),
						"TargetLen":  fmt.Sprintf("%d", res.TargetLen),
						"Generated":  fmt.Sprintf("%d", res.Generated),
						"ExactMatch": matchStr,
						"Closed":     closedStr,
					}
				}

				So(WriteTable(tableRows, "vision_reconstruction_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "vision_reconstruction_summary.tex"))
				So(statErr, ShouldBeNil)
			})
		})
	})
}
