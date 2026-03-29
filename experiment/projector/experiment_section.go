package projector

import (
	_ "embed"
	"strconv"
)

//go:embed experiment_section.tmpl
var ExperimentSectionTmpl string

// Exported formatting helpers so task packages can pre-render metric strings
// for ExperimentSection fields without duplicating formatting logic.

func F0(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
func F1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
func F2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func F3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
func F4(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

func Pct(v float64) string {
	return F1(v*100) + `\%`
}
