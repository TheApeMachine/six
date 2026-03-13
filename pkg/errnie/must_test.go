package errnie

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMust(t *testing.T) {
	Convey("Given Result and Must/Try/Then operations", t, func() {
		Convey("Ok should wrap a value and not have an error", func() {
			res := Ok(42)
			So(res.Value(), ShouldEqual, 42)
			So(res.Err(), ShouldBeNil)
			val, err := res.Unwrap()
			So(val, ShouldEqual, 42)
			So(err, ShouldBeNil)
			So(res.Must(), ShouldEqual, 42)
		})

		Convey("Fail should wrap an error and have zero value", func() {
			testErr := errors.New("test error")
			res := Fail[int](testErr)
			So(res.Err(), ShouldEqual, testErr)
			val, err := res.Unwrap()
			So(val, ShouldEqual, 0)
			So(err, ShouldEqual, testErr)
			So(func() { res.Must() }, ShouldPanicWith, testErr)
		})

		Convey("Try should return Ok with nil error", func() {
			res := Try(100, nil)
			So(res.Value(), ShouldEqual, 100)
			So(res.Err(), ShouldBeNil)
		})

		Convey("Try should return Fail with error", func() {
			testErr := errors.New("test error")
			res := Try(100, testErr)
			So(res.Err(), ShouldEqual, testErr)
			So(res.Value(), ShouldEqual, 0)
		})

		Convey("Map should transform value over a successful Result", func() {
			res := Ok(10).Map(func(v int) int { return v * 2 })
			So(res.Value(), ShouldEqual, 20)
		})

		Convey("Map should bypass failed Result", func() {
			testErr := errors.New("test error")
			res := Fail[int](testErr).Map(func(v int) int { return v * 2 })
			So(res.Err(), ShouldEqual, testErr)
		})

		Convey("Then should pass through successful operations", func() {
			res := Then(Ok(10), func(v int) (string, error) {
				return "success", nil
			})
			So(res.Value(), ShouldEqual, "success")
			So(res.Err(), ShouldBeNil)
		})

		Convey("Then should chain an error if fn fails", func() {
			testErr := errors.New("then error")
			res := Then(Ok(10), func(v int) (string, error) {
				return "", testErr
			})
			So(res.Err(), ShouldEqual, testErr)
		})

		Convey("Then should short-circuit an existing error", func() {
			firstErr := errors.New("first error")
			res := Then(Fail[int](firstErr), func(v int) (string, error) {
				return "success", nil
			})
			So(res.Err(), ShouldEqual, firstErr)
		})
	})
}

func TestForEach(t *testing.T) {
	Convey("Given ForEach", t, func() {
		Convey("It should iterate completely with no errors", func() {
			count := 0
			err := ForEach(5, func(index int) error {
				count++
				return nil
			})
			So(err, ShouldBeNil)
			So(count, ShouldEqual, 5)
		})

		Convey("It should short-circuit on error", func() {
			testErr := errors.New("iteration error")
			count := 0
			err := ForEach(5, func(index int) error {
				count++
				if index == 2 {
					return testErr
				}
				return nil
			})
			So(err, ShouldEqual, testErr)
			So(count, ShouldEqual, 3) 
		})
	})
}
