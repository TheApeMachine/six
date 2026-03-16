package validate

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRequire(t *testing.T) {
	Convey("Given a map of required dependencies", t, func() {
		Convey("When all values are non-nil", func() {
			objs := map[string]any{
				"pool": 1,
				"ctx":  "valid",
			}

			Convey("It should return nil", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})

		Convey("When a value is nil", func() {
			objs := map[string]any{
				"pool": 1,
				"ctx":  nil,
			}

			Convey("It should return error naming the missing field", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "ctx is required")
			})
		})

		Convey("When any required field is nil", func() {
			objs := map[string]any{
				"pool": nil,
				"ctx":  "valid",
			}

			Convey("It should return error for the nil field", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, " is required")
			})
		})

		Convey("When the map is empty", func() {
			objs := map[string]any{}

			Convey("It should return nil", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})

		Convey("When a value is an empty slice", func() {
			objs := map[string]any{
				"items": []int{},
			}

			Convey("It should return nil (empty slice is non-nil)", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})
	})
}

func BenchmarkRequire(b *testing.B) {
	objs := map[string]any{
		"pool":         1,
		"subscription": 2,
		"groups":       3,
		"ctx":          "valid",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Require(objs)
	}
}

func BenchmarkRequireWithNil(b *testing.B) {
	objs := map[string]any{
		"pool":  1,
		"ctx":   nil,
		"other": 3,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Require(objs)
	}
}


