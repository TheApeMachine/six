package replay

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTryOnce(t *testing.T) {
	Convey("TryOnce returns nil when sequence too short", t, func() {
		out := TryOnce(0.5, 5, 10, 0.1, Env{
			PickLabel:            func() string { return "L" },
			GenerateContinuation: func(string, float64, int) string { return "ab" },
			LabelConfidence:      func(string, string) float64 { return 100 },
			PathIsNovel:          func(string) bool { return true },
			Train:                func(string, string, float64) {},
		})

		So(out, ShouldBeNil)
	})

	Convey("TryOnce trains when gates pass", t, func() {
		var trained bool

		out := TryOnce(0.5, 2, 10, 0.1, Env{
			PickLabel:            func() string { return "L" },
			GenerateContinuation: func(string, float64, int) string { return "abcd" },
			LabelConfidence:      func(string, string) float64 { return 50 },
			PathIsNovel:          func(string) bool { return true },
			Train: func(string, string, float64) {
				trained = true
			},
		})

		So(out, ShouldNotBeNil)
		So(trained, ShouldBeTrue)
	})
}
