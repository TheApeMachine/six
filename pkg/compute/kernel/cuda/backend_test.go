//go:build cuda && cgo

package cuda

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestAvailable(t *testing.T) {
	if Available() == 0 {
		t.Skip("CUDA backend unavailable")
	}

	Convey("Given the CUDA backend", t, func() {
		So(Available(), ShouldBeGreaterThan, 0)
	})
}

func TestBackendExecuteGeometric(t *testing.T) {
	Convey("Given a CUDA backend and a geometric opcode", t, func() {
		if Available() == 0 {
			t.Skip("CUDA backend unavailable")
		}

		backend := NewBackend(0, BackendWithObserver(nil))

		frame := primitive.AllocValue()
		So(frame, ShouldNotBeNil)

		defer primitive.FreeValue(frame)

		left := geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8}
		right := geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5}
		expected := left.GeometricProduct(right)

		frame[kernel.ProgramStartWord] = kernel.OpcodeGeometricCompose
		writeCUDATestMultivector(frame, kernel.ContextStartWord, left)
		writeCUDATestMultivector(frame, kernel.GradientStartWord, right)

		idx, ok := primitive.ArenaIndex(frame)
		So(ok, ShouldBeTrue)

		err := backend.Execute([]uint32{idx})

		So(err, ShouldBeNil)
		So(readCUDATestMultivector(frame, kernel.SignalsStartWord), ShouldResemble, expected)
	})
}

func BenchmarkBackendExecuteGeometric(b *testing.B) {
	if Available() == 0 {
		b.Skip("CUDA backend unavailable")
	}

	backend := NewBackend(0, BackendWithObserver(nil))
	frame := primitive.AllocValue()

	writeCUDATestMultivector(
		frame,
		kernel.ContextStartWord,
		geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8},
	)
	writeCUDATestMultivector(
		frame,
		kernel.GradientStartWord,
		geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5},
	)

	frame[kernel.ProgramStartWord] = kernel.OpcodeGeometricCompose
	idx, ok := primitive.ArenaIndex(frame)
	if !ok {
		b.Fatal("benchmark frame not in arena")
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = backend.Execute([]uint32{idx})
	}

	primitive.FreeValue(frame)
}

func writeCUDATestMultivector(frame *primitive.Value, start int, mv geometry.Multivector) {
	*(*geometry.Multivector)(unsafe.Pointer(&(*frame)[start])) = mv
}

func readCUDATestMultivector(frame *primitive.Value, start int) geometry.Multivector {
	return *(*geometry.Multivector)(unsafe.Pointer(&(*frame)[start]))
}
