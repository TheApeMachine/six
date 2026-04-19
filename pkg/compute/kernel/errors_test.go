package kernel

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewKernelError(t *testing.T) {
	t.Parallel()

	Convey("NewKernelError records subsystem, type, op, and unwrap target", t, func() {
		cause := errors.New("root")
		err := NewKernelError("metal", KernelErrDispatchFailed, cause, "BitwiseOr", 4)

		So(err, ShouldNotBeNil)
		So(err.Subsystem, ShouldEqual, "metal")
		So(err.Op, ShouldEqual, "BitwiseOr")
		So(err.N, ShouldEqual, 4)
		So(err.Type, ShouldEqual, KernelErrDispatchFailed)
		So(errors.Is(err, cause), ShouldBeTrue)
		So(errors.Unwrap(err), ShouldEqual, cause)
	})
}

func TestKernelError_Error(t *testing.T) {
	t.Parallel()

	Convey("Given a nil KernelError", t, func() {
		var err *KernelError

		So(err.Error(), ShouldEqual, "")
	})

	Convey("Given N > 0", t, func() {
		err := NewKernelError("cpu", KernelErrNilPointer, nil, "Execute", 8)

		So(err.Error(), ShouldContainSubstring, "cpu")
		So(err.Error(), ShouldContainSubstring, "Execute")
		So(err.Error(), ShouldContainSubstring, "n=8")
	})

	Convey("Given N == 0", t, func() {
		err := NewKernelError("cuda", KernelErrUnavailable, nil, "Init", 0)

		So(err.Error(), ShouldNotContainSubstring, "n=")
	})
}

func TestKernelError_Unwrap(t *testing.T) {
	t.Parallel()

	Convey("Unwrap on nil receiver returns nil", t, func() {
		var err *KernelError

		So(err.Unwrap(), ShouldBeNil)
	})
}
