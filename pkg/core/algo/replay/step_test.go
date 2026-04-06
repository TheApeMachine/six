package replay

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTryOnce(t *testing.T) {
	Convey("Given TryOnce with a short generated sequence", t, func() {
		Convey("When minRunes is larger than the rune length", func() {
			out := TryOnce(0.5, 5, 10, 0.1, Env{
				PickLabel:            func() string { return "L" },
				GenerateContinuation: func(string, float64, int) string { return "ab" },
				LabelConfidence:      func(string, string) float64 { return 100 },
				PathIsNovel:          func(string) bool { return true },
				Train:                func(string, string, float64) {},
			})

			Convey("It should return nil", func() {
				So(out, ShouldBeNil)
			})
		})
	})

	Convey("Given TryOnce with gates satisfied", t, func() {
		Convey("When confidence is strictly above the threshold", func() {
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

			Convey("It should accept and invoke Train", func() {
				So(out, ShouldNotBeNil)
				So(trained, ShouldBeTrue)
			})
		})
	})

	Convey("Given TryOnce at the confidence cutoff", t, func() {
		Convey("When LabelConfidence equals the threshold", func() {
			var trained bool

			out := TryOnce(0.5, 2, 10, 0.1, Env{
				PickLabel:            func() string { return "L" },
				GenerateContinuation: func(string, float64, int) string { return "abcd" },
				LabelConfidence:      func(string, string) float64 { return 0.1 },
				PathIsNovel:          func(string) bool { return true },
				Train: func(string, string, float64) {
					trained = true
				},
			})

			Convey("It should reject and never train", func() {
				So(out, ShouldBeNil)
				So(trained, ShouldBeFalse)
			})
		})
	})
}
