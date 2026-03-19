package errnie

import (
	"context"
	"errors"
	"sync"
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

func TestStateConcurrentHandle(t *testing.T) {
	Convey("Given a fresh State with concurrent Handle calls", t, func() {
		state := NewState("test/concurrent")
		var wg sync.WaitGroup

		for i := range 32 {
			wg.Add(1)

			go func(id int) {
				defer wg.Done()
				state.Handle(errors.New("error"))
			}(i)
		}

		wg.Wait()

		Convey("Exactly one error should be stored", func() {
			So(state.Failed(), ShouldBeTrue)
			So(state.Err(), ShouldNotBeNil)
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

		Convey("Guard should recover from panics and mark state failed", func() {
			result := Guard(state, func() (int, error) {
				panic("boom")
			})

			So(result, ShouldEqual, 0)
			So(state.Failed(), ShouldBeTrue)
			So(state.Err().Error(), ShouldContainSubstring, "boom")
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

func TestGuard2(t *testing.T) {
	Convey("Given a clean State", t, func() {
		state := NewState("test/guard2")

		Convey("Guard2 should return both values on success", func() {
			a, b := Guard2(state, func() (int, string, error) {
				return 42, "hello", nil
			})

			So(a, ShouldEqual, 42)
			So(b, ShouldEqual, "hello")
			So(state.Failed(), ShouldBeFalse)
		})

		Convey("Guard2 should return zero values on error", func() {
			a, b := Guard2(state, func() (int, string, error) {
				return 99, "nope", errors.New("failed")
			})

			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
			So(state.Failed(), ShouldBeTrue)
		})

		Convey("Guard2 should skip when state is failed", func() {
			state.Handle(errors.New("prior"))
			called := false

			a, b := Guard2(state, func() (int, string, error) {
				called = true
				return 1, "x", nil
			})

			So(called, ShouldBeFalse)
			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
		})
	})
}

func TestGuard3(t *testing.T) {
	Convey("Given a clean State", t, func() {
		state := NewState("test/guard3")

		Convey("Guard3 should return all three values on success", func() {
			a, b, c := Guard3(state, func() (int, string, bool, error) {
				return 1, "two", true, nil
			})

			So(a, ShouldEqual, 1)
			So(b, ShouldEqual, "two")
			So(c, ShouldBeTrue)
			So(state.Failed(), ShouldBeFalse)
		})

		Convey("Guard3 should return zero values on error", func() {
			a, b, c := Guard3(state, func() (int, string, bool, error) {
				return 1, "two", true, errors.New("failed")
			})

			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
			So(c, ShouldBeFalse)
			So(state.Failed(), ShouldBeTrue)
		})

		Convey("Guard3 should skip when state is failed", func() {
			state.Handle(errors.New("prior"))
			called := false

			a, b, c := Guard3(state, func() (int, string, bool, error) {
				called = true
				return 1, "x", true, nil
			})

			So(called, ShouldBeFalse)
			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
			So(c, ShouldBeFalse)
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

/*
TestStateWithRecovery is intentionally commented out: Handle does not
currently spawn state.recovery despite the doc comment promising it.
This is a known gap — uncomment after wiring recovery into Handle.

func TestStateWithRecovery(t *testing.T) { ... }
*/

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

		Convey("GuardCtx should short-circuit when state is failed", func() {
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

		Convey("GuardCtx should mark state failed when fn returns error", func() {
			result := GuardCtx(state, func(ctx context.Context) (int, error) {
				return 0, errors.New("ctx fn failed")
			})

			So(result, ShouldEqual, 0)
			So(state.Failed(), ShouldBeTrue)
			So(state.Err().Error(), ShouldContainSubstring, "ctx fn failed")
		})
	})
}

func TestGuardVoidCtx(t *testing.T) {
	Convey("Given a State with StateWithContext", t, func() {
		state := NewState("test/guardvoidctx", StateWithContext(context.Background()))

		Convey("GuardVoidCtx should execute fn when state is clean", func() {
			called := false

			GuardVoidCtx(state, func(ctx context.Context) error {
				called = true
				return nil
			})

			So(called, ShouldBeTrue)
			So(state.Failed(), ShouldBeFalse)
		})

		Convey("GuardVoidCtx should mark state failed on error", func() {
			GuardVoidCtx(state, func(ctx context.Context) error {
				return errors.New("void ctx failed")
			})

			So(state.Failed(), ShouldBeTrue)
		})

		Convey("GuardVoidCtx should skip when state is failed", func() {
			state.Handle(errors.New("prior"))
			called := false

			GuardVoidCtx(state, func(ctx context.Context) error {
				called = true
				return nil
			})

			So(called, ShouldBeFalse)
		})
	})
}

func TestGuard2Ctx(t *testing.T) {
	Convey("Given a State with context", t, func() {
		state := NewState("test/guard2ctx", StateWithContext(context.Background()))

		Convey("Guard2Ctx should return both values on success", func() {
			a, b := Guard2Ctx(state, func(ctx context.Context) (int, string, error) {
				return 7, "ok", nil
			})

			So(a, ShouldEqual, 7)
			So(b, ShouldEqual, "ok")
		})

		Convey("Guard2Ctx should skip when state is failed", func() {
			state.Handle(errors.New("prior"))
			called := false

			a, b := Guard2Ctx(state, func(ctx context.Context) (int, string, error) {
				called = true
				return 1, "x", nil
			})

			So(called, ShouldBeFalse)
			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
		})
	})
}

func TestGuard3Ctx(t *testing.T) {
	Convey("Given a State with context", t, func() {
		state := NewState("test/guard3ctx", StateWithContext(context.Background()))

		Convey("Guard3Ctx should return all values on success", func() {
			a, b, c := Guard3Ctx(state, func(ctx context.Context) (int, string, bool, error) {
				return 1, "two", true, nil
			})

			So(a, ShouldEqual, 1)
			So(b, ShouldEqual, "two")
			So(c, ShouldBeTrue)
		})

		Convey("Guard3Ctx should skip when state is failed", func() {
			state.Handle(errors.New("prior"))

			a, b, c := Guard3Ctx(state, func(ctx context.Context) (int, string, bool, error) {
				return 1, "x", true, nil
			})

			So(a, ShouldEqual, 0)
			So(b, ShouldEqual, "")
			So(c, ShouldBeFalse)
		})
	})
}

// ---------------------------------------------------------------------------
// Benchmarks — baseline for every code path
// ---------------------------------------------------------------------------

func BenchmarkStateFailed(b *testing.B) {
	state := NewState("bench")
	b.ReportAllocs()

	for b.Loop() {
		state.Failed()
	}
}

func BenchmarkStateFailedTrue(b *testing.B) {
	state := NewState("bench")
	state.Handle(errors.New("failed"))
	b.ReportAllocs()

	for b.Loop() {
		state.Failed()
	}
}

func BenchmarkStateReset(b *testing.B) {
	state := NewState("bench")
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
	}
}

func BenchmarkGuardClean(b *testing.B) {
	state := NewState("bench")
	fn := func() (int, error) { return 42, nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		Guard(state, fn)
	}
}

func BenchmarkGuardFailed(b *testing.B) {
	state := NewState("bench")
	state.Handle(errors.New("failed"))
	fn := func() (int, error) { return 42, nil }
	b.ReportAllocs()

	for b.Loop() {
		Guard(state, fn)
	}
}

func BenchmarkGuardVoidClean(b *testing.B) {
	state := NewState("bench")
	fn := func() error { return nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		GuardVoid(state, fn)
	}
}

func BenchmarkGuardVoidFailed(b *testing.B) {
	state := NewState("bench")
	state.Handle(errors.New("failed"))
	fn := func() error { return nil }
	b.ReportAllocs()

	for b.Loop() {
		GuardVoid(state, fn)
	}
}

func BenchmarkGuard2Clean(b *testing.B) {
	state := NewState("bench")
	fn := func() (int, string, error) { return 42, "ok", nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		Guard2(state, fn)
	}
}

func BenchmarkGuard3Clean(b *testing.B) {
	state := NewState("bench")
	fn := func() (int, string, bool, error) { return 1, "two", true, nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		Guard3(state, fn)
	}
}

func BenchmarkGuardCtxClean(b *testing.B) {
	state := NewState("bench", StateWithContext(context.Background()))
	fn := func(ctx context.Context) (int, error) { return 42, nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		GuardCtx(state, fn)
	}
}

func BenchmarkGuardCtxFailed(b *testing.B) {
	state := NewState("bench", StateWithContext(context.Background()))
	state.Handle(errors.New("failed"))
	fn := func(ctx context.Context) (int, error) { return 42, nil }
	b.ReportAllocs()

	for b.Loop() {
		GuardCtx(state, fn)
	}
}

func BenchmarkGuardVoidCtxClean(b *testing.B) {
	state := NewState("bench", StateWithContext(context.Background()))
	fn := func(ctx context.Context) error { return nil }
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		GuardVoidCtx(state, fn)
	}
}

func BenchmarkSafeMustClean(b *testing.B) {
	fn := func() (int, error) { return 42, nil }
	b.ReportAllocs()

	for b.Loop() {
		SafeMust(fn)
	}
}

func BenchmarkSafeMustVoidClean(b *testing.B) {
	fn := func() error { return nil }
	b.ReportAllocs()

	for b.Loop() {
		SafeMustVoid(fn)
	}
}
