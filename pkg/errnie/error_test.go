package errnie

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewErrnieError(t *testing.T) {
	t.Parallel()

	Convey("NewErrnieError returns nil for a nil underlying error", t, func() {
		So(NewErrnieError(nil), ShouldBeNil)
	})

	Convey("NewErrnieError wraps the cause", t, func() {
		cause := errors.New("cause")
		wrapped := NewErrnieError(cause, "k", 1)

		So(wrapped, ShouldNotBeNil)
		So(wrapped.Error(), ShouldEqual, cause.Error())
		So(errors.Unwrap(wrapped), ShouldEqual, cause)
	})
}

func TestErrnieError_Unwrap(t *testing.T) {
	t.Parallel()

	Convey("Unwrap on nil receiver returns nil", t, func() {
		var err *ErrnieError

		So(err.Unwrap(), ShouldBeNil)
	})
}

func TestErrnieError_Join(t *testing.T) {
	t.Parallel()

	Convey("Join keeps the receiver usable", t, func() {
		base := errors.New("a")
		err := NewErrnieError(base)

		_ = err.Join(errors.New("b"))

		So(errors.Unwrap(err), ShouldEqual, base)
	})
}

func TestIsReschedulable(t *testing.T) {
	t.Parallel()

	Convey("IsReschedulable is false for standard errors", t, func() {
		So(IsReschedulable(errors.New("x")), ShouldBeFalse)
	})

	Convey("IsReschedulable reads ErrnieError flag", t, func() {
		e := NewErrnieError(errors.New("y"))

		So(IsReschedulable(e), ShouldBeFalse)
	})
}

func TestHasContext(t *testing.T) {
	t.Parallel()

	Convey("HasContext is currently a placeholder", t, func() {
		So(HasContext(NewErrnieError(errors.New("z"))), ShouldBeNil)
	})
}
