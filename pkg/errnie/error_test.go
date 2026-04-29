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

	Convey("Given distinct standard errors chained through ErrnieError.Join", t, func() {
		base := errors.New("a")
		next := errors.New("b")
		err := NewErrnieError(base)

		Convey("It should expose both tails through errors.Join wiring", func() {
			_ = err.Join(next)

			So(errors.Is(err, base), ShouldBeTrue)
			So(errors.Is(err, next), ShouldBeTrue)
		})
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

	Convey("Given a standard error", t, func() {
		Convey("It should report nil context bindings", func() {
			So(HasContext(errors.New("z")), ShouldBeNil)
		})
	})

	Convey("Given an ErrnieError annotated with canceled context metadata", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := NewErrnieError(errors.New("z")).WithContext(ctx)

		Convey("It should expose the identical context pointer captured by WithContext", func() {
			So(HasContext(err), ShouldEqual, ctx)
		})
	})
}

func BenchmarkJoin(b *testing.B) {
	base := errors.New("a")
	next := errors.New("b")

	b.ReportAllocs()
	b.ResetTimer()

	for benchmarkIdx := 0; benchmarkIdx < b.N; benchmarkIdx++ {
		err := NewErrnieError(base)
		err.Join(next)

		if !(errors.Is(err, base) && errors.Is(err, next)) {
			b.Fatalf("join drift benchIdx=%d", benchmarkIdx)
		}
	}
}

func BenchmarkHasContext(b *testing.B) {
	plainErr := errors.New("plain")
	ctxBackground, cancel := context.WithCancel(context.Background())
	cancel()
	errnieErr := NewErrnieError(errors.New("trace")).WithContext(ctxBackground)

	b.ReportAllocs()
	b.ResetTimer()

	for benchmarkIdx := 0; benchmarkIdx < b.N; benchmarkIdx++ {
		if HasContext(plainErr) != nil {
			b.Fatal("unexpected context on plain error")
		}

		if HasContext(errnieErr) != ctxBackground {
			b.Fatalf("context mismatch benchIdx=%d", benchmarkIdx)
		}
	}
}
