package task

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	tools "github.com/theapemachine/six/experiment"
)

/*
NormalizeBarChartSeries rewrites each series slice so floating-point noise and
unreadable literals do not leak into LaTeX/PDF renders or JSON snapshots.

Series named Partial and Exact are rounded to five decimal places, Weighted to
six, Fuzzy to three significant figures (bar heights match those semantics).
Other names default to six decimal places so ad-hoc charts stay stable.
*/
func NormalizeBarChartSeries(series []tools.BarSeries) []tools.BarSeries {
	out := make([]tools.BarSeries, len(series))

	for index := range series {
		record := series[index]
		record.Data = normalizeBarSeriesFloats(record.Name, record.Data)
		out[index] = record
	}

	return out
}

/*
FormatBarChartDataForArtifactJSON returns the chart payload used inside artifact
snapshots. Values match NormalizeBarChartSeries; the Fuzzy series encodes each
point with scientific notation JSON literals for readability.
*/
func FormatBarChartDataForArtifactJSON(chart tools.BarChartData) map[string]any {
	normalized := NormalizeBarChartSeries(chart.Series)
	seriesOut := make([]any, len(normalized))

	for index, record := range normalized {
		entry := map[string]any{"name": record.Name}

		if record.Name == "Fuzzy" {
			entry["data"] = marshalFuzzyBarDataJSON(record.Data)
			seriesOut[index] = entry

			continue
		}

		entry["data"] = record.Data
		seriesOut[index] = entry
	}

	return map[string]any{
		"x_axis": chart.XAxis,
		"series": seriesOut,
	}
}

func normalizeBarSeriesFloats(seriesName string, values []float64) []float64 {
	switch seriesName {
	case "Fuzzy":
		return roundSliceSignificant(values, 3)
	default:
		return roundSliceDecimal(values, barChartDecimalPlaces(seriesName))
	}
}

func barChartDecimalPlaces(seriesName string) int {
	switch seriesName {
	case "Partial", "Exact":
		return 5
	case "Weighted":
		return 6
	default:
		return 6
	}
}

func roundSliceDecimal(values []float64, places int) []float64 {
	scale := math.Pow10(places)
	out := make([]float64, len(values))

	for index, value := range values {
		out[index] = math.Round(value*scale) / scale
	}

	return out
}

func roundSliceSignificant(values []float64, figures int) []float64 {
	out := make([]float64, len(values))

	for index, value := range values {
		out[index] = roundSignificant(value, figures)
	}

	return out
}

func roundSignificant(value float64, figures int) float64 {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}

	negative := value < 0
	magnitude := math.Abs(value)
	exponent := math.Floor(math.Log10(magnitude))
	factor := math.Pow(10, float64(figures-1)-exponent)
	rounded := math.Round(magnitude*factor) / factor

	if negative {
		return -rounded
	}

	return rounded
}

func marshalFuzzyBarDataJSON(values []float64) json.RawMessage {
	builder := strings.Builder{}
	builder.WriteByte('[')

	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}

		builder.WriteString(strconv.FormatFloat(value, 'e', 2, 64))
	}

	builder.WriteByte(']')

	return json.RawMessage(builder.String())
}
