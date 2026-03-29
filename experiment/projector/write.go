package projector

import (
	"io"
	"os"
)

/*
WriteTable generate a LaTeX table as a paper artifact.
It will be automatically included in the final paper.
*/
func WriteTable(data any, outDir, outFile string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(outDir + "/" + outFile)
	if err != nil {
		return err
	}
	defer f.Close()
	return NewTable(
		TableWithData(data),
		TableWithOutput(f),
	).Generate()
}

func TriggerAutoBuild() {
	// AutoBuild disabled to prevent asynchronous LaTeX compilation race conditions
	// Paper compilation should be a distinct manual step using `go run main.go paper`
}

func WriteBarChart(
	xAxis []string,
	series []BarSeries,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	c := NewBarChart(
		BarChartWithAxes(xAxis, series),
		BarChartWithMeta(title, caption, label),
		BarChartWithOutput(outDir, filename),
	)

	if out != nil {
		c.SetOutput(out)
	}

	return c.GenerateToDisk()
}

func WriteLineChart(
	xAxis []string,
	series []LineSeries,
	title,
	caption,
	label,
	outDir,
	filename string,
	yMin,
	yMax float64,
	out *os.File,
) error {
	

	c := NewLineChart(
		LineChartWithAxes(xAxis, series),
		LineChartWithMeta(title, caption, label),
		LineChartWithOutput(outDir, filename),
		LineChartWithYRange(yMin, yMax),
	)

	if out != nil {
		c.SetOutput(out)
	}

	return c.Generate()
}

func WriteComboChart(
	xAxis []string,
	series []ComboSeries,
	xName,
	yName string,
	yMin,
	yMax float64,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	c := NewComboChart(
		ComboChartWithAxes(xAxis, series),
		ComboChartWithAxisLabels(xName, yName),
		ComboChartWithMeta(title, caption, label),
		ComboChartWithOutput(outDir, filename),
		ComboChartWithYRange(yMin, yMax),
	)

	if out != nil {
		c.SetOutput(out)
	}

	return c.Generate()
}

func WriteHeatMap(
	xAxis,
	yAxis []string,
	data [][]any,
	minV,
	maxV float64,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	hm := NewHeatMap(
		HeatMapWithData(xAxis, yAxis, data, minV, maxV),
		HeatMapWithMeta(title, caption, label),
		HeatMapWithOutput(outDir, filename),
	)

	if out != nil {
		hm.SetOutput(out)
	}

	return hm.Generate()
}

func WriteConfusionMatrix(
	labels []string,
	matrix [][]int,
	meanScore float64,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	cm := NewConfusionMatrix(
		ConfusionMatrixWithData(labels, matrix),
		ConfusionMatrixWithMeta(title, caption, label),
		ConfusionMatrixWithOutput(outDir, filename),
		ConfusionMatrixWithMeanScore(meanScore),
	)

	if out != nil {
		cm.SetOutput(out)
	}

	return cm.Generate()
}

func WriteMultiPanel(
	panels []MPPanel,
	width,
	height int,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	mp := NewMultiPanel(
		MultiPanelWithPanels(panels...),
		MultiPanelWithMeta(title, caption, label),
		MultiPanelWithOutput(outDir, filename),
		MultiPanelWithSize(width, height),
	)

	if out != nil {
		mp.SetOutput(out)
	}

	return mp.Generate()
}

func WriteProse(
	tmplSrc string,
	data any,
	outDir,
	outFile string,
) error {
	

	p := NewProse(
		ProseWithTemplate(tmplSrc),
		ProseWithData(data),
		ProseWithOutput(outDir, outFile),
	)

	p.SetOutput(io.Discard)

	return p.Generate()
}

func WriteImageStrip(
	rows []ImageStripRow,
	title,
	caption,
	label,
	outDir,
	filename string,
	out *os.File,
) error {
	

	is := NewImageStrip(
		ImageStripWithData(rows),
		ImageStripWithMeta(title, caption, label),
		ImageStripWithOutput(outDir, filename),
	)

	if out != nil {
		is.SetOutput(out)
	}

	return is.Generate()
}
