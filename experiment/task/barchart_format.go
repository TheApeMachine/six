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

Series named Partial, Fuzzy, Weighted, and Exact use six decimal places so
artifact JSON stays stable for diffing and downstream tooling. Other names
default to six decimal places as well.
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
snapshots. Values match NormalizeBarChartSeries (fixed six-decimal floats).
*/
func FormatBarChartDataForArtifactJSON(chart tools.BarChartData) map[string]any {
	normalized := NormalizeBarChartSeries(chart.Series)
	seriesOut := make([]any, len(normalized))

	for index, record := range normalized {
		entry := map[string]any{"name": record.Name}

		switch record.Name {
		case "Partial", "Fuzzy", "Weighted":
			entry["data"] = marshalBarDataFixedDecimals(record.Data, 6)
		default:
			entry["data"] = record.Data
		}

		seriesOut[index] = entry
	}

	return map[string]any{
		"x_axis": chart.XAxis,
		"series": seriesOut,
	}
}

func marshalBarDataFixedDecimals(values []float64, decimals int) json.RawMessage {
	builder := strings.Builder{}
	builder.WriteByte('[')

	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}

		builder.WriteString(strconv.FormatFloat(value, 'f', decimals, 64))
	}

	builder.WriteByte(']')

	return json.RawMessage(builder.String())
}

func normalizeBarSeriesFloats(seriesName string, values []float64) []float64 {
	return roundSliceDecimal(values, barChartDecimalPlaces(seriesName))
}

func barChartDecimalPlaces(seriesName string) int {
	switch seriesName {
	case "Partial", "Fuzzy", "Weighted", "Exact":
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
