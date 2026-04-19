package kernel

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNormalizeObserver(t *testing.T) {
	t.Parallel()

	Convey("NormalizeObserver substitutes NoopObserver for nil", t, func() {
		o := NormalizeObserver(nil)

		So(o, ShouldNotBeNil)
		o.Trace("noop")
		o.Error("noop", errors.New("e"))
	})

	Convey("NormalizeObserver preserves a concrete observer", t, func() {
		calls := atomic.Int32{}
		inner := ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				calls.Add(1)
			},
		}

		o := NormalizeObserver(inner)
		o.Trace("x")

		So(calls.Load(), ShouldEqual, 1)
	})
}

func TestNoopObserver(t *testing.T) {
	t.Parallel()

	Convey("NoopObserver Trace and Error are safe no-ops", t, func() {
		var noop NoopObserver

		noop.Trace("t")
		noop.Error("e", errors.New("err"))
	})
}

func TestObserverFuncs_Trace(t *testing.T) {
	t.Parallel()

	Convey("Given a nil TraceFn", t, func() {
		f := ObserverFuncs{}

		f.Trace("event")
	})

	Convey("Given TraceFn", t, func() {
		var gotEvent string
		var gotKV []any

		f := ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				gotEvent = event
				gotKV = keyvals
			},
		}

		f.Trace("hello", "k", "v")

		So(gotEvent, ShouldEqual, "hello")
		So(gotKV, ShouldResemble, []any{"k", "v"})
	})
}

func TestObserverFuncs_Error(t *testing.T) {
	t.Parallel()

	Convey("Given a nil ErrorFn", t, func() {
		f := ObserverFuncs{}

		f.Error("event", errors.New("x"))
	})

	Convey("Given ErrorFn", t, func() {
		var gotErr error

		f := ObserverFuncs{
			ErrorFn: func(event string, err error, keyvals ...any) {
				gotErr = err
			},
		}

		e := errors.New("boom")
		f.Error("e", e)

		So(gotErr, ShouldEqual, e)
	})
}

func TestNewAsyncObserver(t *testing.T) {
	t.Parallel()

	Convey("NewAsyncObserver uses default queue size when non-positive", t, func() {
		target := NoopObserver{}
		ao := NewAsyncObserver(target, 0)

		So(ao, ShouldNotBeNil)
		ao.Close()
	})
}

func TestAsyncObserver_Trace(t *testing.T) {
	t.Parallel()

	Convey("Trace delivers to the target after Close drains the queue", t, func() {
		var calls atomic.Int32

		target := ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				calls.Add(1)
			},
		}

		ao := NewAsyncObserver(target, 256)
		ao.Trace("evt", "a", 1)
		ao.Close()

		So(calls.Load(), ShouldEqual, 1)
	})

	Convey("Trace on nil AsyncObserver is safe", t, func() {
		var ao *AsyncObserver

		ao.Trace("x")
	})
}

func TestAsyncObserver_Error(t *testing.T) {
	t.Parallel()

	Convey("Error is ignored when err is nil", t, func() {
		calls := atomic.Int32{}
		target := ObserverFuncs{
			ErrorFn: func(event string, err error, keyvals ...any) {
				calls.Add(1)
			},
		}

		ao := NewAsyncObserver(target, 8)
		ao.Error("e", nil)
		ao.Close()

		So(calls.Load(), ShouldEqual, 0)
	})

	Convey("Error on nil AsyncObserver is safe", t, func() {
		var ao *AsyncObserver

		ao.Error("e", errors.New("x"))
	})
}

func TestAsyncObserver_Dropped(t *testing.T) {
	t.Parallel()

	Convey("Dropped returns 0 for a nil observer", t, func() {
		var ao *AsyncObserver

		So(ao.Dropped(), ShouldEqual, 0)
	})

	Convey("Events overflow when the worker is blocked and the queue is tiny", t, func() {
		started := make(chan struct{})
		block := make(chan struct{})
		var once sync.Once

		target := ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				once.Do(func() {
					close(started)
				})
				<-block
			},
		}

		ao := NewAsyncObserver(target, 1)
		ao.Trace("first")

		<-started

		for range 32 {
			ao.Trace("flood")
		}

		close(block)
		ao.Close()

		So(ao.Dropped(), ShouldBeGreaterThan, 0)
	})
}

func TestAsyncObserver_Close(t *testing.T) {
	t.Parallel()

	Convey("Close on nil is safe", t, func() {
		var ao *AsyncObserver

		ao.Close()
	})

	Convey("Close is idempotent", t, func() {
		ao := NewAsyncObserver(NoopObserver{}, 8)
		ao.Close()
		ao.Close()
	})
}

func BenchmarkAsyncObserver_Trace(b *testing.B) {
	ao := NewAsyncObserver(NoopObserver{}, 4096)
	defer ao.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		ao.Trace("bench")
	}
}
