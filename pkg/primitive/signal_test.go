package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestLongestOneRun exercises the pure bit-math helper that underlies
the Signals scanner. The helper is unexported so this test stays in
the primitive package; higher-level signal tests that need the CPU
backend live in pkg/compute/kernel/cpu to avoid a test-only import
cycle between primitive and cpu.
*/
func TestLongestOneRun(t *testing.T) {
	Convey("longestOneRun should find correct run lengths", t, func() {
		So(longestOneRun(0), ShouldEqual, 0)
		So(longestOneRun(1), ShouldEqual, 1)
		So(longestOneRun(0x3), ShouldEqual, 2)
		So(longestOneRun(0x7), ShouldEqual, 3)
		So(longestOneRun(0xFF), ShouldEqual, 8)
		So(longestOneRun(0xFF00FF), ShouldEqual, 8)
		So(longestOneRun(0xFFFF), ShouldEqual, 16)
		So(longestOneRun(0xFFFFFFFFFFFFFFFF), ShouldEqual, 64)
		So(longestOneRun(0x0F0F0F0F), ShouldEqual, 4)
	})
}
