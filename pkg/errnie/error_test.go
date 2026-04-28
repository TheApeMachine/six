package errnie

import (
	"context"
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

	Convey("Join keeps both errors observable", t, func() {
		base := errors.New("a")
		next := errors.New("b")
		err := NewErrnieError(base)

		_ = err.Join(next)

		So(errors.Is(err, base), ShouldBeTrue)
		So(errors.Is(err, next), ShouldBeTrue)
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

	Convey("HasContext returns nil for standard errors", t, func() {
		So(HasContext(errors.New("z")), ShouldBeNil)
	})

	Convey("HasContext reads ErrnieError context", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := NewErrnieError(errors.New("z")).WithContext(ctx)

		So(HasContext(err), ShouldEqual, ctx)
	})
}
