package projector

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBrowserAllocatorArgs(t *testing.T) {
	Convey("Given the projector renderer browser flags", t, func() {
		args := browserAllocatorArgs()

		Convey("It should include the sandbox-disabling flags required by headless CI", func() {
			So(args, ShouldContain, "no-sandbox")
			So(args, ShouldContain, "disable-setuid-sandbox")
		})
	})
}
