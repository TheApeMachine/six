package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewResult(t *testing.T) {
	Convey("Given a sample value", t, func() {
		sample := "test-value"
		Convey("When NewResult is called", func() {
			result := NewResult(sample)
			Convey("It should return a non-nil result", func() {
				So(result, ShouldNotBeNil)
			})
			Convey("It should set Value to the sample", func() {
				So(result.Value, ShouldEqual, sample)
			})
			Convey("It should set CreatedAt to a recent time", func() {
				So(time.Since(result.CreatedAt) < time.Second, ShouldBeTrue)
			})
		})
	})
}

func BenchmarkNewResult(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewResult("test-value")
	}
}
