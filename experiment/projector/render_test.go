package projector

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExportPDFWithSize(t *testing.T) {
	Convey("Given a simple chart HTML document", t, func() {
		tempDir := t.TempDir()
		htmlPath := filepath.Join(tempDir, "chart.html")
		pdfPath := filepath.Join(tempDir, "chart.pdf")
		html := "<html><body><div>graph render</div></body></html>"

		err := os.WriteFile(htmlPath, []byte(html), 0644)
		So(err, ShouldBeNil)

		Convey("It should render a PDF in sandboxed CI environments", func() {
			err = ExportPDFWithSize(htmlPath, pdfPath, 640, 480)
			So(err, ShouldBeNil)

			info, statErr := os.Stat(pdfPath)
			So(statErr, ShouldBeNil)
			So(info.Size(), ShouldBeGreaterThan, 0)
		})
	})
}
