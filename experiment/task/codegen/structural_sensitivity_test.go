package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestStructuralSensitivity(t *testing.T) {
	Convey("Given the Python corpus structural sensitivity probe", t, func() {
		corpus := pythonCorpus()
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		prompts := []struct {
			prefix, correct, funcName string
		}{
			{"def factorial(n):", "if n <= 1:\n        return 1\n    return n * factorial(n - 1)", "factorial"},
			{"def binary_search(lst, target):", "low, high = 0, len(lst) - 1\n    while low <= high:", "binary_search"},
			{"def filter_list(fn, lst):", "result = []\n    for x in lst:\n        if fn(x):", "filter_list"},
			{"def find_max(lst):", "if not lst:\n        return None\n    best = lst[0]", "find_max"},
			{"def dfs(graph, start):", "visited = set()\n    stack = [start]", "dfs"},
		}

		// find corpus function for this name
		findFull := func(name string) string {
			for _, fn := range corpus {
				if strings.Contains(fn, "def "+name+"(") {
					return fn
				}
			}
			return ""
		}

		Convey("When probing structural variants (comment, noise, correct)", func() {
			var entries []StructSensEntry
			for _, p := range prompts {
				full := findFull(p.funcName)
				if full == "" {
					continue
				}
				fpFull := geometry.NewPhaseDial().Encode(full)
				fpBase := geometry.NewPhaseDial().Encode(p.prefix)

				comment := p.prefix + "\n    # compute result"
				noise := p.prefix + "\n    import sys"
				correct := p.prefix + "\n    " + p.correct

				fpComment := geometry.NewPhaseDial().Encode(comment)
				fpNoise := geometry.NewPhaseDial().Encode(noise)
				fpCorrect := geometry.NewPhaseDial().Encode(correct)

				simComment := fpComment.Similarity(fpFull)
				simNoise := fpNoise.Similarity(fpFull)
				simCorrect := fpCorrect.Similarity(fpFull)

				// Directional alignment: dot of delta-vector toward full from each variant
				dotComment := fpComment.Similarity(fpFull) - fpBase.Similarity(fpFull)
				dotNoise := fpNoise.Similarity(fpFull) - fpBase.Similarity(fpFull)
				dotCorrect := fpCorrect.Similarity(fpFull) - fpBase.Similarity(fpFull)

				// Correct should have highest sim to full
				So(simCorrect, ShouldBeGreaterThan, simComment)
				So(simCorrect, ShouldBeGreaterThan, simNoise)

				// Correct should move most toward full
				So(dotCorrect, ShouldBeGreaterThan, dotComment)
				So(dotCorrect, ShouldBeGreaterThan, dotNoise)

				entries = append(entries, StructSensEntry{
					Name: p.funcName, Prefix: p.prefix,
					SimCommentFull: simComment, SimNoiseFull: simNoise, SimCorrectFull: simCorrect,
					DirComment: dotComment, DirNoise: dotNoise, DirCorrect: dotCorrect,
				})
			}

			Convey("Correct continuation closest to full function", func() {
				for _, e := range entries {
					So(e.SimCorrectFull, ShouldBeGreaterThan, e.SimCommentFull)
					So(e.SimCorrectFull, ShouldBeGreaterThan, e.SimNoiseFull)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				commentData := make([]float64, len(entries))
				noiseData := make([]float64, len(entries))
				correctData := make([]float64, len(entries))
				for i, e := range entries {
					xAxis[i] = e.Name
					commentData[i] = e.SimCommentFull
					noiseData[i] = e.SimNoiseFull
					correctData[i] = e.SimCorrectFull
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Comment", Data: commentData},
					{Name: "Noise", Data: noiseData},
					{Name: "Correct", Data: correctData},
				}, "Structural Sensitivity",
					"Similarity to full function for three suffix types.",
					"fig:struct_sens", "structural_sensitivity"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Function":   e.Name,
						"SimComment": fmt.Sprintf("%.4f", e.SimCommentFull),
						"SimNoise":   fmt.Sprintf("%.4f", e.SimNoiseFull),
						"SimCorrect": fmt.Sprintf("%.4f", e.SimCorrectFull),
					}
				}
				So(WriteTable(tableRows, "structural_sensitivity_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "structural_sensitivity_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = StructSensResult{
					Entries: entries,
				}
			})

			Convey("Artifact: write structural sensitivity subsection prose", func() {
				tmpl, err := os.ReadFile("prose/structural_sensitivity.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"NFunctions": len(entries),
				}, "structural_sensitivity_prose.tex"), ShouldBeNil)
			})
		})
	})
}
