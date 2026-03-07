package projector

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ExportPDF renders an HTML file to PDF via headless Chrome.
// The viewport is set to match the chart dimensions so ECharts renders at
// exactly the right pixel size, and the @page CSS rule ensures PrintToPDF
// produces a tight crop with no extra margins.
func ExportPDF(htmlPath, pdfPath string) error {
	return ExportPDFWithSize(htmlPath, pdfPath, 0, 0)
}

// ExportPDFWithSize renders an HTML file to PDF with an explicit viewport.
// When width/height are 0 a sensible default (1200×800) is used.
func ExportPDFWithSize(htmlPath, pdfPath string, width, height int) error {
	if width <= 0 {
		width = 1200
	}
	if height <= 0 {
		height = 800
	}

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var buf []byte
	if err := chromedp.Run(ctx,
		// Set the viewport BEFORE navigating so the page layout uses
		// the correct dimensions from the first paint.
		emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false),

		chromedp.Navigate("file://"+htmlPath),
		// Give ECharts time to render (SVG renderer is synchronous but
		// the layout needs a frame).
		chromedp.Sleep(500*time.Millisecond),

		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			// Paper size in inches (96 DPI).
			paperW := pxToInches(width)
			paperH := pxToInches(height)
			buf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithPaperWidth(paperW).
				WithPaperHeight(paperH).
				WithMarginTop(0).
				WithMarginBottom(0).
				WithMarginLeft(0).
				WithMarginRight(0).
				WithScale(1).
				Do(ctx)
			return err
		}),
	); err != nil {
		return fmt.Errorf("chromedp render %s: %w", htmlPath, err)
	}

	return os.WriteFile(pdfPath, buf, 0644)
}

// pxToInches converts CSS pixels (96 DPI) to inches for the PDF paper size.
func pxToInches(px int) float64 {
	return math.Round(float64(px)/96.0*1000) / 1000
}
