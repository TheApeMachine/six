package programmer

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestGeometricExecutableCompileAndExecute(t *testing.T) {
	Convey("Given an executable authored with geometric DSL ops", t, func() {
		values, err := primitive.NewValue([]byte("geometric-slice"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		defer value.Close()

		left := geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8}
		right := geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5}
		expectedCompose := left.GeometricProduct(right)
		expectedSandwich := left.Sandwich(right)
		expectedReverse := left.Reverse()

		writeTestMultivector(value, kernel.ContextStartWord, left)
		writeTestMultivector(value, kernel.GradientStartWord, right)

		backend := cpu.NewBackend(context.Background())

		Convey("compose should compile through the programmer layer and execute on CPU", func() {
			executable := NewExecutable(value, "context gradient signals compose accumulate\n", nil)
			frames, compileErr := executable.Compile(CPU)

			So(compileErr, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Contract, ShouldEqual, ContractGeometric)
			So(frames[0].Program[0], ShouldEqual, uint64(COMPOSE))

			frames[0].WriteIntoProgramRegion(value)
			So(backend.Execute([]unsafe.Pointer{unsafe.Pointer(&value[0])}), ShouldBeNil)
			So(readTestMultivector(value, kernel.SignalsStartWord), ShouldResemble, expectedCompose)
		})

		Convey("sandwich should compile through the programmer layer and execute on CPU", func() {
			executable := NewExecutable(value, "context gradient signals sandwich accumulate\n", nil)
			frames, compileErr := executable.Compile(CPU)

			So(compileErr, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Contract, ShouldEqual, ContractGeometric)
			So(frames[0].Program[0], ShouldEqual, uint64(SANDWICH))

			frames[0].WriteIntoProgramRegion(value)
			So(backend.Execute([]unsafe.Pointer{unsafe.Pointer(&value[0])}), ShouldBeNil)
			So(readTestMultivector(value, kernel.SignalsStartWord), ShouldResemble, expectedSandwich)
		})

		Convey("reverse should compile through the programmer layer and execute on CPU", func() {
			executable := NewExecutable(value, "context context signals reverse accumulate\n", nil)
			frames, compileErr := executable.Compile(CPU)

			So(compileErr, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Contract, ShouldEqual, ContractGeometric)
			So(frames[0].Program[0], ShouldEqual, uint64(REVERSE))

			frames[0].WriteIntoProgramRegion(value)
			So(backend.Execute([]unsafe.Pointer{unsafe.Pointer(&value[0])}), ShouldBeNil)
			So(readTestMultivector(value, kernel.SignalsStartWord), ShouldResemble, expectedReverse)
		})
	})
}

func writeTestMultivector(value *primitive.Value, start int, mv geometry.Multivector) {
	if value == nil {
		return
	}

	*(*geometry.Multivector)(unsafe.Pointer(&(*value)[start])) = mv
}

func readTestMultivector(value *primitive.Value, start int) geometry.Multivector {
	if value == nil {
		return geometry.Multivector{}
	}

	return *(*geometry.Multivector)(unsafe.Pointer(&(*value)[start]))
}
