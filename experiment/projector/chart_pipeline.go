package projector

import "io"

/*
finalizeEChartsFigure renders shared HTML layout, exports HTML+PDF, and writes
the LaTeX figure stub — the common tail of every single-canvas chart type.
*/
func finalizeEChartsFigure(
	title string,
	width, height int,
	chartScript string,
	outDir, filename string,
	caption, label string,
	out io.Writer,
) error {
	html, err := renderChartHTML(title, width, height, chartScript)

	if err != nil {
		return err
	}

	if err := renderAndExport(html, outDir, filename, width, height); err != nil {
		return err
	}

	return emitFigure(filename, caption, label, out)
}
