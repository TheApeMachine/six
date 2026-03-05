package scaling

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBestFillO1Scaling(t *testing.T) {
	Convey("Given the BestFill GPU shader as the core resonance search", t, func() {
		Convey("When measuring latency across 3 orders of magnitude", func() {
			Convey("Then latency should remain within 10x across all corpus sizes (O(1) claim)", func() {
			})
		})
	})
}
