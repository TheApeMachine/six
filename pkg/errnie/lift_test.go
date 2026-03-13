package errnie

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestLift(t *testing.T) {
	Convey("Given Lift functions", t, func() {
		Convey("Lift without arguments", func() {
			successFn := func() (int, error) { return 42, nil }
			errorFn := func() (int, error) { return 0, errors.New("err") }

			liftedSuccess := Lift(successFn)
			res := liftedSuccess()
			So(res.Value(), ShouldEqual, 42)
			So(res.Err(), ShouldBeNil)

			liftedError := Lift(errorFn)
			resErr := liftedError()
			So(resErr.Err(), ShouldNotBeNil)
			So(resErr.Err().Error(), ShouldEqual, "err")
		})

		Convey("Lift1 with 1 argument", func() {
			successFn := func(a int) (int, error) { return a * 2, nil }
			errorFn := func(a int) (int, error) { return 0, errors.New("err") }

			liftedSuccess := Lift1(successFn)
			res := liftedSuccess(21)
			So(res.Value(), ShouldEqual, 42)
			So(res.Err(), ShouldBeNil)

			liftedError := Lift1(errorFn)
			resErr := liftedError(21)
			So(resErr.Err(), ShouldNotBeNil)
		})

		Convey("Lift2 with 2 arguments", func() {
			successFn := func(a, b int) (int, error) { return a + b, nil }
			errorFn := func(a, b int) (int, error) { return 0, errors.New("err") }

			liftedSuccess := Lift2(successFn)
			res := liftedSuccess(20, 22)
			So(res.Value(), ShouldEqual, 42)
			So(res.Err(), ShouldBeNil)

			liftedError := Lift2(errorFn)
			resErr := liftedError(20, 22)
			So(resErr.Err(), ShouldNotBeNil)
		})
	})
}
