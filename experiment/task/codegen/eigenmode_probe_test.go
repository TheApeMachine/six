package codegen

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/numeric"
)

func TestEigenmodeProbe(t *testing.T) {
	Convey("Given the chord-native EigenMode from geometry", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		So(len(corpus), ShouldBeGreaterThan, 0)

		eigenTable := buildEigenPhaseTable(corpus)
		So(eigenTable, ShouldNotBeNil)

		spanLengths := numeric.FibWindows
		type spanInfo struct {
			text, role string
			phase      float64
		}
		var spans []spanInfo
		for _, fn := range corpus {
			toks := tokenize(fn)
			for _, sLen := range spanLengths {
				if len(toks) < sLen {
					continue
				}
				for start := 0; start <= len(toks)-sLen; start++ {
					span := make([]string, sLen)
					copy(span, toks[start:start+sLen])
					spanText := detokenize(span)
					role := classifyRole(spanText)
					phase, _ := eigenTable.weightedCircularMean(spanText)
					spans = append(spans, spanInfo{spanText, role, phase})
				}
			}
		}

		So(len(spans), ShouldBeGreaterThan, 0)

		Convey("When computing eigenphase and role statistics", func() {
			type roleStat struct {
			count        int
			sinSum, cosSum float64
		}
			roleStats := make(map[string]*roleStat)
			for _, s := range spans {
				rs := roleStats[s.role]
				if rs == nil {
					rs = &roleStat{}
					roleStats[s.role] = rs
				}
				rs.count++
				rs.sinSum += math.Sin(s.phase)
				rs.cosSum += math.Cos(s.phase)
			}

			roles := []string{"header", "loop", "conditional", "return", "assignment", "call"}
			var entries []EigenmodeEntry
			for _, role := range roles {
				rs := roleStats[role]
				if rs == nil {
					continue
				}
				meanPhase := math.Atan2(rs.sinSum/float64(rs.count), rs.cosSum/float64(rs.count))
				R := math.Sqrt(rs.sinSum*rs.sinSum+rs.cosSum*rs.cosSum) / float64(rs.count)
				circStd := 0.0
				if R > 1e-10 {
					circStd = math.Sqrt(-2 * math.Log(R))
				} else {
					circStd = math.Pi
				}
				entries = append(entries, EigenmodeEntry{
					Role: role, Count: rs.count,
					MeanPC1: meanPhase, MeanPC2: meanPhase * 180 / math.Pi,
					StdPC1: circStd, StdPC2: 1 - R, StdPC3: R,
				})
			}

			var separations []EigenmodeSeparation
			for i := 0; i < len(entries); i++ {
				for j := i + 1; j < len(entries); j++ {
					diff := entries[i].MeanPC1 - entries[j].MeanPC1
					angDist := math.Abs(math.Atan2(math.Sin(diff), math.Cos(diff)))
					avgStd := (entries[i].StdPC1 + entries[j].StdPC1) / 2
					ratio := 0.0
					if avgStd > 0 {
						ratio = angDist / avgStd
					}
					separations = append(separations, EigenmodeSeparation{
						RoleA: entries[i].Role, RoleB: entries[j].Role,
						Distance: angDist, AvgSpread: avgStd, Ratio: ratio,
					})
				}
			}
			wellSep := 0
			for _, sep := range separations {
				if sep.Ratio > 1.0 {
					wellSep++
				}
			}

			Convey("Eigenphase entries exist for structural roles", func() {
				So(len(entries), ShouldBeGreaterThan, 0)
				for _, e := range entries {
					So(e.Count, ShouldBeGreaterThan, 0)
					So(e.MeanPC1, ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				phaseData := make([]float64, len(entries))
				spreadData := make([]float64, len(entries))
				for i, e := range entries {
					xAxis[i] = e.Role
					phaseData[i] = e.MeanPC1
					spreadData[i] = e.StdPC1
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Mean Phase", Data: phaseData},
					{Name: "Circ Std", Data: spreadData},
				}, "Eigenmode Role Phases",
					"Per-role eigenphase mean and circular std.",
					"fig:eigenmode", "eigenmode_probe"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Role": e.Role, "Count": fmt.Sprintf("%d", e.Count),
						"Phase": fmt.Sprintf("%.4f", e.MeanPC1),
						"Degrees": fmt.Sprintf("%.1f", e.MeanPC2),
						"CircStd": fmt.Sprintf("%.4f", e.StdPC1),
					}
				}
				So(WriteTable(tableRows, "eigenmode_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "eigenmode_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = EigenmodeResult{
					TotalSpans: len(spans), Roles: entries,
					Separations: separations, WellSepCount: wellSep,
					TotalPairs: len(separations),
				}
			})

			Convey("Artifact: write eigenmode probe subsection prose", func() {
				tmpl, err := os.ReadFile("prose/eigenmode_probe.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"Roles":       entries,
					"WellSepCount": wellSep,
					"TotalPairs":  len(separations),
				}, "eigenmode_probe_prose.tex"), ShouldBeNil)
			})
		})
	})
}
