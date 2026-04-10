package task

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
)

func TestNormalizeBarChartSeries(t *testing.T) {
	Convey("Given bar series with noisy floating-point literals", t, func() {
		input := []tools.BarSeries{
			{Name: "Partial", Data: []float64{0.01960000000000001, 0.007200000000000003}},
			{Name: "Weighted", Data: []float64{0.006987916618030049}},
			{Name: "Fuzzy", Data: []float64{0.009033541399165283}},
			{Name: "Exact", Data: []float64{0, 0.00123456789}},
			{Name: "Best Gain", Data: []float64{1.234567890123}},
		}
		output := NormalizeBarChartSeries(input)

		Convey("It should round Partial to six decimal places", func() {
			So(len(output[0].Data), ShouldEqual, 2)
			So(output[0].Data[0], ShouldAlmostEqual, 0.0196, 1e-12)
			So(output[0].Data[1], ShouldAlmostEqual, 0.0072, 1e-12)
		})

		Convey("It should round Weighted to six decimal places", func() {
			So(output[1].Data[0], ShouldAlmostEqual, 0.006988, 1e-12)
		})

		Convey("It should round Fuzzy to six decimal places", func() {
			So(output[2].Data[0], ShouldAlmostEqual, 0.009034, 1e-12)
		})

		Convey("It should round Exact to six decimal places", func() {
			So(output[3].Data[1], ShouldAlmostEqual, 0.001235, 1e-12)
		})

		Convey("It should default unknown series names to six decimals", func() {
			So(output[4].Data[0], ShouldAlmostEqual, 1.234568, 1e-12)
		})
	})
}

func TestFormatBarChartDataForArtifactJSON(t *testing.T) {
	Convey("Given HumanEval-style language scores", t, func() {
		chart := tools.BarChartData{
			XAxis: []string{"Python", "Go"},
			Series: []tools.BarSeries{
				{Name: "Partial", Data: []float64{0.01960000000000001}},
				{Name: "Fuzzy", Data: []float64{0.009033541399165283}},
				{Name: "Weighted", Data: []float64{0.0000909090909090909}},
			},
		}
		payload := FormatBarChartDataForArtifactJSON(chart)
		encoded, err := json.Marshal(payload)
		So(err, ShouldBeNil)

		Convey("It should encode Partial, Fuzzy, and Weighted with six fixed decimal digits", func() {
			So(string(encoded), ShouldContainSubstring, "0.019600")
			So(string(encoded), ShouldContainSubstring, "0.009034")
			So(string(encoded), ShouldContainSubstring, "0.000091")
		})
	})
}

func BenchmarkNormalizeBarChartSeries(b *testing.B) {
	series := []tools.BarSeries{
		{Name: "Partial", Data: []float64{0.01960000000000001, 0.0072, 0.0018}},
		{Name: "Fuzzy", Data: []float64{0.009033541399165283, 0.004157142857142858}},
		{Name: "Weighted", Data: []float64{0.006987916618030049, 0.003165249393061763}},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = NormalizeBarChartSeries(series)
	}
}

func BenchmarkFormatBarChartDataForArtifactJSON(b *testing.B) {
	chart := tools.BarChartData{
		XAxis: []string{"A", "B", "C"},
		Series: []tools.BarSeries{
			{Name: "Partial", Data: []float64{0.0196, 0.0072, 0.0018}},
			{Name: "Fuzzy", Data: []float64{0.009033541399165283, 0.0066088716618397, 0.004157142857142858}},
			{Name: "Weighted", Data: []float64{0.006988, 0.003165, 0.000818}},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = FormatBarChartDataForArtifactJSON(chart)
	}
}
