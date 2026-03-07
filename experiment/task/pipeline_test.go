package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/require"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
)

// slugify turns an experiment name like "Text Classification" into "text_classification"
func slugify(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}

func TestPipeline(t *testing.T) {
	experiments := []PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		classification.NewTextClassificationExperiment(),
	}

	for _, experiment := range experiments {
		Convey("Given experiment: "+experiment.Name(), t, func() {
			pipeline, err := NewPipeline(
				PipelineWithExperiment(experiment),
			)

			So(err, ShouldBeNil)
			So(pipeline, ShouldNotBeNil)

			Convey("When: "+experiment.Name()+" produces an outcome", func() {
				So(pipeline.Run(), ShouldBeNil)
				type scoreExperiment interface {
					Score() float64
				}
				if scored, ok := experiment.(scoreExperiment); ok {
					score := scored.Score()
					So(score, ShouldBeGreaterThanOrEqualTo, 0.0)
					So(score, ShouldBeLessThanOrEqualTo, 1.0)
				} else {
					So(experiment.Outcome())
				}

				Convey("It should produce the needed paper artifacts", func() {
					section := experiment.Section()
					slug := slugify(experiment.Name())

					// Summary table
					So(WriteTable(
						experiment.TableData(),
						slug+"_summary.tex",
						section,
					), ShouldBeNil)

					_, statErr := os.Stat(
						filepath.Join(PaperDir(section),
							slug+"_summary.tex"),
					)
					So(statErr, ShouldBeNil)

					// Score breakdown figure: classification → confusion matrix, codegen → bar chart.
					data := experiment.TableData()
					if len(data) > 0 {
						// Check if this experiment provides class labels (classification task).
						type classLabeler interface {
							ClassLabels() []string
						}
						if cl, ok := experiment.(classLabeler); ok {
							// ── Confusion matrix for classification tasks ──
							labels := cl.ClassLabels()
							n := len(labels)

							// Compute predicted labels via k-NN byte similarity.
							type predictor interface {
								ComputePredictions()
							}
							if pred, ok := experiment.(predictor); ok {
								pred.ComputePredictions()
							}

							// Re-fetch data (PredLabel is now populated).
							data = experiment.TableData()

							matrix := make([][]int, n)
							for i := range matrix {
								matrix[i] = make([]int, n)
							}

							for _, d := range data {
								t := d.TrueLabel
								p := d.PredLabel
								if t >= 0 && t < n && p >= 0 && p < n {
									matrix[t][p]++
								}
							}

							// Use the experiment's Score() method for the mean resonance score.
							type scoreExperiment interface {
								Score() float64
							}
							var meanScore float64
							if scored, ok := experiment.(scoreExperiment); ok {
								meanScore = scored.Score()
							}

							chartName := slug + "_scores"
							So(WriteConfusionMatrix(
								labels,
								matrix,
								meanScore,
								experiment.Name()+" — Confusion Matrix",
								fmt.Sprintf("Confusion matrix showing predicted vs.\\ true class assignments for %s.", experiment.Name()),
								"fig:"+slug+"_confusion",
								chartName,
								section,
							), ShouldBeNil)
						} else {
							// ── Bar chart for non-classification tasks ──
							// Aggregate scores per category (Name).
							type accum struct {
								exact, partial, fuzzy, weighted float64
								n                               int
							}

							groups := map[string]*accum{}
							var order []string
							for _, d := range data {
								key := d.Name
								if key == "" {
									key = slug
								}
								a, ok := groups[key]
								if !ok {
									a = &accum{}
									groups[key] = a
									order = append(order, key)
								}
								a.exact += d.Scores.Exact
								a.partial += d.Scores.Partial
								a.fuzzy += d.Scores.Fuzzy
								a.weighted += d.WeightedTotal
								a.n++
							}

							xAxis := order
							exactData := make([]float64, len(order))
							partialData := make([]float64, len(order))
							fuzzyData := make([]float64, len(order))
							weightedData := make([]float64, len(order))

							for i, key := range order {
								a := groups[key]
								exactData[i] = a.exact / float64(a.n)
								partialData[i] = a.partial / float64(a.n)
								fuzzyData[i] = a.fuzzy / float64(a.n)
								weightedData[i] = a.weighted / float64(a.n)
							}

							chartName := slug + "_scores"
							So(WriteBarChart(
								xAxis,
								[]projector.BarSeries{
									{Name: "Exact", Data: exactData},
									{Name: "Partial", Data: partialData},
									{Name: "Fuzzy", Data: fuzzyData},
									{Name: "Weighted", Data: weightedData},
								},
								experiment.Name()+" — Score Breakdown",
								fmt.Sprintf("Mean exact, partial, fuzzy, and weighted scores for %s.", experiment.Name()),
								"fig:"+slug+"_scores",
								chartName,
								section,
							), ShouldBeNil)
						}
					}
				})
			})
		})
	}
}

func TestPipelineWithScoreWeights(t *testing.T) {
	experiment := codegen.NewLanguagesExperiment()
	weights := tools.ScoreWeights{Exact: 0.2, Partial: 0.7, Fuzzy: 0.1}

	pipeline, err := NewPipeline(
		PipelineWithExperiment(experiment),
		PipelineWithScoreWeights(weights),
	)

	require.NoError(t, err)
	require.InDelta(t, weights.Exact, pipeline.scoreWgts.Exact, 1e-12)
	require.InDelta(t, weights.Partial, pipeline.scoreWgts.Partial, 1e-12)
	require.InDelta(t, weights.Fuzzy, pipeline.scoreWgts.Fuzzy, 1e-12)
}
