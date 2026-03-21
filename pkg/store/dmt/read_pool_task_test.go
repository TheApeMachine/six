package dmt

import (
	"context"
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestReadPoolTaskRead covers a non-loop task whose fn succeeds: first Read runs
fn and returns EOF; subsequent Reads return EOF without re-running fn.
*/
func TestReadPoolTaskRead(t *testing.T) {
	Convey("Given a non-loop readPoolTask with a successful fn", t, func() {
		var callCount int

		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				callCount++

				return "work-result", nil
			},
			loop: false,
		}

		Convey("When Read is called the first time", func() {
			buffer := make([]byte, 32)
			n, err := task.Read(buffer)

			Convey("Then it should return (0, io.EOF) and run fn once", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, io.EOF)
				So(callCount, ShouldEqual, 1)
			})
		})

		Convey("When Read is called again after the first drain", func() {
			_, firstErr := task.Read(make([]byte, 1))
			So(firstErr, ShouldEqual, io.EOF)

			n, err := task.Read(make([]byte, 1))

			Convey("Then it should return (0, io.EOF) without calling fn again", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, io.EOF)
				So(callCount, ShouldEqual, 1)
			})
		})
	})
}

/*
TestReadPoolTaskReadError covers a non-loop task whose fn fails: Read surfaces
the error and marks the task done so later Reads yield EOF.
*/
func TestReadPoolTaskReadError(t *testing.T) {
	Convey("Given a non-loop readPoolTask whose fn returns an error", t, func() {
		fnErr := errors.New("read pool task fn failed")
		var callCount int

		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				callCount++

				return nil, fnErr
			},
			loop: false,
		}

		Convey("When Read is called", func() {
			n, err := task.Read(nil)

			Convey("Then it should return (0, err) and mark the task done", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, fnErr)
				So(callCount, ShouldEqual, 1)
			})
		})

		Convey("When Read is called after the error", func() {
			_, firstErr := task.Read(nil)
			So(firstErr, ShouldEqual, fnErr)

			n, err := task.Read(nil)

			Convey("Then it should return (0, io.EOF)", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, io.EOF)
				So(callCount, ShouldEqual, 1)
			})
		})
	})
}

/*
TestReadPoolTaskLoopRead covers loop-mode tasks: first Read still runs fn once
and completes with EOF (same drain contract as non-loop for this type).
*/
func TestReadPoolTaskLoopRead(t *testing.T) {
	Convey("Given a loop readPoolTask with a successful fn", t, func() {
		var callCount int

		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				callCount++

				return struct{}{}, nil
			},
			loop: true,
		}

		Convey("When Read is called", func() {
			n, err := task.Read(make([]byte, 8))

			Convey("Then it should return (0, io.EOF) after fn runs", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, io.EOF)
				So(callCount, ShouldEqual, 1)
			})
		})
	})
}

/*
TestReadPoolTaskLoopReadError covers loop-mode tasks when fn returns an error.
*/
func TestReadPoolTaskLoopReadError(t *testing.T) {
	Convey("Given a loop readPoolTask whose fn returns an error", t, func() {
		fnErr := errors.New("loop task failed")

		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				return nil, fnErr
			},
			loop: true,
		}

		Convey("When Read is called", func() {
			n, err := task.Read(nil)

			Convey("Then it should return the fn error", func() {
				So(n, ShouldEqual, 0)
				So(err, ShouldEqual, fnErr)
			})
		})
	})
}

/*
TestReadPoolTaskWrite verifies Write accepts payload bytes and reports full length.
*/
func TestReadPoolTaskWrite(t *testing.T) {
	Convey("Given a readPoolTask", t, func() {
		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				return nil, nil
			},
			loop: false,
		}

		Convey("When Write is called with varying slice sizes", func() {
			small := []byte{1, 2, 3}
			nSmall, errSmall := task.Write(small)

			large := make([]byte, 4096)
			nLarge, errLarge := task.Write(large)

			Convey("Then each Write should return len(p) and nil error", func() {
				So(nSmall, ShouldEqual, len(small))
				So(errSmall, ShouldBeNil)
				So(nLarge, ShouldEqual, len(large))
				So(errLarge, ShouldBeNil)
			})
		})
	})
}

/*
TestReadPoolTaskClose verifies Close is a no-op success.
*/
func TestReadPoolTaskClose(t *testing.T) {
	Convey("Given a readPoolTask", t, func() {
		task := &readPoolTask{
			ctx: context.Background(),
			fn: func(ctx context.Context) (any, error) {
				return nil, nil
			},
			loop: false,
		}

		Convey("When Close is called", func() {
			closeErr := task.Close()

			Convey("Then it should return nil", func() {
				So(closeErr, ShouldBeNil)
			})
		})
	})
}

/*
TestReadPoolTaskFnReceivesContext ensures the task passes its constructor ctx into fn
for both loop and non-loop modes.
*/
func TestReadPoolTaskFnReceivesContext(t *testing.T) {
	type contextKey struct{}

	expected := "task-bound-context-value"
	baseCtx := context.WithValue(context.Background(), contextKey{}, expected)

	Convey("Given a non-loop readPoolTask with a decorated context", t, func() {
		var seen context.Context

		task := &readPoolTask{
			ctx: baseCtx,
			fn: func(ctx context.Context) (any, error) {
				seen = ctx

				return nil, nil
			},
			loop: false,
		}

		Convey("When Read drains the task", func() {
			_, err := task.Read(nil)
			So(err, ShouldEqual, io.EOF)

			Convey("Then fn should receive the same context instance", func() {
				So(seen, ShouldEqual, baseCtx)
				So(seen.Value(contextKey{}), ShouldEqual, expected)
			})
		})
	})

	Convey("Given a loop readPoolTask with a decorated context", t, func() {
		var seen context.Context

		task := &readPoolTask{
			ctx: baseCtx,
			fn: func(ctx context.Context) (any, error) {
				seen = ctx

				return nil, nil
			},
			loop: true,
		}

		Convey("When Read drains the task", func() {
			_, err := task.Read(nil)
			So(err, ShouldEqual, io.EOF)

			Convey("Then fn should receive the same context instance", func() {
				So(seen, ShouldEqual, baseCtx)
				So(seen.Value(contextKey{}), ShouldEqual, expected)
			})
		})
	})
}

/*
BenchmarkReadPoolTaskRead measures non-loop Read after constructing a fresh task
per iteration (matches worker one-shot drain usage).
*/
func BenchmarkReadPoolTaskRead(b *testing.B) {
	ctx := context.Background()
	fn := func(context.Context) (any, error) {
		return nil, nil
	}

	buf := make([]byte, 64)
	b.ReportAllocs()

	for b.Loop() {
		task := &readPoolTask{
			ctx:  ctx,
			fn:   fn,
			loop: false,
		}

		n, err := task.Read(buf)
		if n != 0 || err != io.EOF {
			b.Fatalf("unexpected Read: n=%d err=%v", n, err)
		}
	}
}

/*
BenchmarkReadPoolTaskWrite measures Write throughput on a long-lived task.
*/
func BenchmarkReadPoolTaskWrite(b *testing.B) {
	task := &readPoolTask{
		ctx: context.Background(),
		fn: func(context.Context) (any, error) {
			return nil, nil
		},
		loop: false,
	}

	payload := make([]byte, 256)
	b.ReportAllocs()

	for b.Loop() {
		n, err := task.Write(payload)
		if n != len(payload) || err != nil {
			b.Fatalf("unexpected Write: n=%d err=%v", n, err)
		}
	}
}
