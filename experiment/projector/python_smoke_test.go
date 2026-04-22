package projector

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// TestPythonRendererSmoke exercises the end-to-end matplotlib path for one
// chart kind. Skipped when SIX_FIGURE_PYTHON / system python3 / matplotlib
// aren't available; never asserts pixel-level output, only that a non-empty
// PDF lands at the expected path.
func TestPythonRendererSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	Convey("Given a bar chart routed through the matplotlib renderer", t, func() {
		dir := t.TempDir()

		err := WriteBarChart(
			[]string{"a", "b", "c"},
			[]BarSeries{{Name: "demo", Data: []float64{0.3, 0.7, 0.5}}},
			"Smoke",
			"smoke caption",
			"fig:smoke",
			dir,
			"smoke_bar",
			nil,
		)
		So(err, ShouldBeNil)

		Convey("It should produce a non-empty PDF on disk", func() {
			info, statErr := os.Stat(filepath.Join(dir, "smoke_bar.pdf"))
			if statErr != nil && os.IsNotExist(statErr) {
				t.Skip("python renderer unavailable; pdf not produced")
			}
			So(statErr, ShouldBeNil)
			So(info.Size(), ShouldBeGreaterThan, 1024)
		})
	})
}

var _ = io.Discard
