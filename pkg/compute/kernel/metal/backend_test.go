package metal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAvailable(t *testing.T) {
	Convey("Given the Metal backend", t, func() {
		count := Available()
		So(count, ShouldBeGreaterThan, 0)
	})
}
