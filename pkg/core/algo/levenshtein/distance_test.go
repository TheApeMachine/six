package levenshtein

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDistance(t *testing.T) {
	Convey("Distance", t, func() {
		So(Distance("cab", "cab"), ShouldEqual, 0)
		So(Distance("cab", "cat"), ShouldEqual, 1)
		So(Distance("saturday", "sunday"), ShouldBeGreaterThan, 0)
	})
}

func BenchmarkDistance(b *testing.B) {
	const left = "saturday"
	const right = "sunday"

	b.ResetTimer()

	for b.Loop() {
		_ = Distance(left, right)
	}
}
