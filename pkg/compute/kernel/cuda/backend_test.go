//go:build cuda && cgo

package cuda

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAvailable(t *testing.T) {
	if Available() == 0 {
		t.Skip("CUDA backend unavailable")
	}

	Convey("Given the CUDA backend", t, func() {
		So(Available(), ShouldBeGreaterThan, 0)
	})
}
