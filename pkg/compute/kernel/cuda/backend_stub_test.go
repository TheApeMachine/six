//go:build !cuda || !cgo

package cuda

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
)

func TestNewBackend(t *testing.T) {
	t.Parallel()

	Convey("NewBackend wires context and observer normalization", t, func() {
		traceCalls := 0
		obs := kernel.ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				traceCalls++
			},
		}

		backend := NewBackend(0, BackendWithObserver(obs))

		So(backend, ShouldNotBeNil)
		So(backend.Name(), ShouldEqual, "cuda")
		So(backend.Context(), ShouldNotBeNil)

		backend.observer.Trace("probe")

		So(traceCalls, ShouldEqual, 1)

		backend.Shutdown()
	})
}

func TestBackend_SetObserver(t *testing.T) {
	t.Parallel()

	Convey("SetObserver replaces the active observer", t, func() {
		first := 0
		second := 0

		backend := NewBackend(0, BackendWithObserver(kernel.ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				first++
			},
		}))

		backend.SetObserver(kernel.ObserverFuncs{
			TraceFn: func(event string, keyvals ...any) {
				second++
			},
		})

		backend.observer.Trace("x")

		So(first, ShouldEqual, 0)
		So(second, ShouldEqual, 1)

		backend.Shutdown()
	})
}

func TestBackend_Execute(t *testing.T) {
	t.Parallel()

	Convey("Execute returns an unavailable kernel error", t, func() {
		backend := NewBackend(0)
		defer backend.Shutdown()

		err := backend.Execute([]uint32{1, 2})

		So(err, ShouldNotBeNil)

		var ke *kernel.KernelError

		So(errors.As(err, &ke), ShouldBeTrue)
		So(ke.Type, ShouldEqual, kernel.KernelErrUnavailable)
	})
}

func TestBackend_NearestAffinity(t *testing.T) {
	t.Parallel()

	Convey("NearestAffinity returns an unavailable kernel error", t, func() {
		backend := NewBackend(0)
		defer backend.Shutdown()

		out, err := backend.NearestAffinity(nil, nil, 4)

		So(out, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})
}

func TestBackend_Schedule(t *testing.T) {
	t.Parallel()

	Convey("Schedule runs the job with the backend context", t, func() {
		backend := NewBackend(0)
		defer backend.Shutdown()

		var saw context.Context

		err := backend.Schedule(func(ctx context.Context) error {
			saw = ctx

			return nil
		})

		So(err, ShouldBeNil)
		So(saw, ShouldEqual, backend.Context())
	})
}

func TestAvailable(t *testing.T) {
	t.Parallel()

	Convey("Available returns a non-negative GPU count", t, func() {
		n := Available()

		So(n, ShouldBeGreaterThanOrEqualTo, 0)
	})
}

func TestNewCUDAKernelError(t *testing.T) {
	t.Parallel()

	Convey("NewCUDAKernelError tags the cuda subsystem", t, func() {
		err := NewCUDAKernelError(kernel.KernelErrInitFailed, errors.New("x"), "Boot", 2)

		So(err.Subsystem, ShouldEqual, "cuda")
		So(err.Op, ShouldEqual, "Boot")
		So(err.N, ShouldEqual, 2)
	})
}
