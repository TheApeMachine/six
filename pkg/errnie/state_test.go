package errnie

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestState(t *testing.T) {
	Convey("Given a fresh State", t, func() {
		state := NewState("test/pkg")

		Convey("It should not be in a failed state", func() {
			So(state.Failed(), ShouldBeFalse)
			So(state.Err(), ShouldBeNil)
		})

		Convey("When Handle is called with an error", func() {
			state.Handle(errors.New("first error"))

			Convey("It should be in a failed state", func() {
				So(state.Failed(), ShouldBeTrue)
				So(state.Err(), ShouldNotBeNil)
				So(state.Err().Error(), ShouldContainSubstring, "test/pkg")
				So(state.Err().Error(), ShouldContainSubstring, "first error")
			})

			Convey("A second Handle should not overwrite the first error", func() {
				state.Handle(errors.New("second error"))
				So(state.Err().Error(), ShouldContainSubstring, "first error")
				So(state.Err().Error(), ShouldNotContainSubstring, "second error")
			})
		})

		Convey("When Reset is called after failure", func() {
			state.Handle(errors.New("some error"))
			state.Reset()

			Convey("It should be clean again", func() {
				So(state.Failed(), ShouldBeFalse)
				So(state.Err(), ShouldBeNil)
			})
		})
	})
}

func TestGuard(t *testing.T) {
	Convey("Given a clean State", t, func() {
		state := NewState("test/guard")

		Convey("Guard should execute the function and return the result", func() {
			result := Guard(state, func() (int, error) {
				return 42, nil
			})

			So(result, ShouldEqual, 42)
			So(state.Failed(), ShouldBeFalse)
		})

		Convey("Guard should mark the state failed on error", func() {
			result := Guard(state, func() (int, error) {
				return 0, errors.New("compute failed")
			})

			So(result, ShouldEqual, 0)
			So(state.Failed(), ShouldBeTrue)
			So(state.Err().Error(), ShouldContainSubstring, "compute failed")
		})

		Convey("Guard should skip execution when state is already failed", func() {
			state.Handle(errors.New("prior failure"))
			called := false

			result := Guard(state, func() (int, error) {
				called = true
				return 99, nil
			})

			So(called, ShouldBeFalse)
			So(result, ShouldEqual, 0)
		})
	})
}

func TestGuardVoid(t *testing.T) {
	Convey("Given a clean State", t, func() {
		state := NewState("test/guardvoid")

		Convey("GuardVoid should execute when state is clean", func() {
			called := false

			GuardVoid(state, func() error {
				called = true
				return nil
			})

			So(called, ShouldBeTrue)
			So(state.Failed(), ShouldBeFalse)
		})

		Convey("GuardVoid should mark state failed on error", func() {
			GuardVoid(state, func() error {
				return errors.New("write failed")
			})

			So(state.Failed(), ShouldBeTrue)
			So(state.Err().Error(), ShouldContainSubstring, "write failed")
		})

		Convey("GuardVoid should skip execution when state is already failed", func() {
			state.Handle(errors.New("prior failure"))
			called := false

			GuardVoid(state, func() error {
				called = true
				return nil
			})

			So(called, ShouldBeFalse)
		})
	})
}

func TestGuardCascade(t *testing.T) {
	Convey("Given a State that fails on the first call", t, func() {
		state := NewState("test/cascade")
		callCount := 0

		Guard(state, func() (int, error) {
			callCount++
			return 0, errors.New("step 1 failed")
		})

		Guard(state, func() (int, error) {
			callCount++
			return 0, nil
		})

		Guard(state, func() (int, error) {
			callCount++
			return 0, nil
		})

		Convey("Only the first call should execute", func() {
			So(callCount, ShouldEqual, 1)
		})

		Convey("The state should carry the original error", func() {
			So(state.Err().Error(), ShouldContainSubstring, "step 1 failed")
		})
	})
}

func BenchmarkGuardClean(b *testing.B) {
	state := NewState("bench")
	fn := func() (int, error) { return 42, nil }

	for b.Loop() {
		state.Reset()
		Guard(state, fn)
	}
}

func BenchmarkGuardFailed(b *testing.B) {
	state := NewState("bench")
	state.Handle(errors.New("failed"))
	fn := func() (int, error) { return 42, nil }

	for b.Loop() {
		Guard(state, fn)
	}
}

func TestStateWithContext(t *testing.T) {
	Convey("Given a State with StateWithContext", t, func() {
		parent := context.Background()
		state := NewState("test/ctx", StateWithContext(parent))

		Convey("Ctx should return a non-nil context", func() {
			So(state.Ctx(), ShouldNotBeNil)
			So(state.Ctx().Err(), ShouldBeNil)
		})

		Convey("When Handle is called", func() {
			state.Handle(errors.New("node failure"))

			Convey("Ctx should be cancelled", func() {
				So(state.Ctx().Err(), ShouldNotBeNil)
				So(state.Ctx().Err(), ShouldEqual, context.Canceled)
			})
		})

		Convey("When Heal is called after Handle", func() {
			state.Handle(errors.New("prior failure"))
			state.Heal()

			Convey("Ctx should be renewed and not cancelled", func() {
				So(state.Ctx(), ShouldNotBeNil)
				So(state.Ctx().Err(), ShouldBeNil)
			})
		})
	})
}

func TestGuardCtx(t *testing.T) {
	Convey("Given a State with StateWithContext", t, func() {
		parent := context.Background()
		state := NewState("test/guardctx", StateWithContext(parent))

		Convey("GuardCtx should pass ctx to fn and return result", func() {
			result := GuardCtx(state, func(ctx context.Context) (int, error) {
				So(ctx, ShouldNotBeNil)
				return 42, nil
			})
			So(result, ShouldEqual, 42)
		})

		Convey("GuardCtx should short-circuit when ctx is cancelled", func() {
			state.Handle(errors.New("failure"))
			called := false

			result := GuardCtx(state, func(ctx context.Context) (int, error) {
				called = true
				return 99, nil
			})

			So(called, ShouldBeFalse)
			So(result, ShouldEqual, 0)
		})

		Convey("GuardCtx should short-circuit when parent ctx is already cancelled", func() {
			cancelled, cancel := context.WithCancel(parent)
			cancel()
			state := NewState("test/guardctx-cancelled", StateWithContext(cancelled))
			called := false

			result := GuardCtx(state, func(ctx context.Context) (int, error) {
				called = true
				return 99, nil
			})

			So(called, ShouldBeFalse)
			So(result, ShouldEqual, 0)
		})
	})
}

func BenchmarkGuardCtxClean(b *testing.B) {
	state := NewState("bench", StateWithContext(context.Background()))
	fn := func(ctx context.Context) (int, error) { return 42, nil }

	for b.Loop() {
		state.Reset()
		GuardCtx(state, fn)
	}
}
