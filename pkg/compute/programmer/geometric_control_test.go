package programmer

import (
	"context"
	"math/bits"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestGeometricProgramProducesInBandScalarControl(t *testing.T) {
	Convey("Given a geometric scoring program authored in the DSL", t, func() {
		values, err := primitive.NewValue([]byte("geometric-control"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		defer value.Close()

		left := geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8}
		right := geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5}
		expected := left.GeometricProduct(right)
		expectedScore := geometricScore(expected)

		writeControlTestMultivector(value, kernel.ContextStartWord, left)
		writeControlTestMultivector(value, kernel.GradientStartWord, right)

		executable := NewExecutable(value, coreProgramBeamGapScore, nil)
		frames, compileErr := executable.Compile(CPU)

		So(compileErr, ShouldBeNil)
		So(len(frames), ShouldEqual, 2)
		So(frames[0].Contract, ShouldEqual, ContractGeometric)
		So(frames[0].Program[0], ShouldEqual, uint64(COMPOSE))
		So(frames[1].Contract, ShouldEqual, ContractReduceBinary)

		backend := cpu.NewBackend(context.Background())

		for index := range frames {
			frames[index].WriteIntoProgramRegion(value)
			So(backend.Execute([]unsafe.Pointer{unsafe.Pointer(&value[0])}), ShouldBeNil)
		}

		Convey("the program should leave the multivector in signals and an in-band scalar in properties", func() {
			So(readControlTestMultivector(value, kernel.SignalsStartWord), ShouldResemble, expected)
			So((*value)[kernel.PropertiesStartWord], ShouldEqual, expectedScore)
		})
	})
}

const coreProgramBeamGapScore = "context[0,8] gradient[0,8] signals[0,8] compose accumulate\n" +
	"signals[0,8] signals[0,8] properties[0,1] xor reduce\n"

func geometricScore(mv geometry.Multivector) uint64 {
	var total uint64

	for index := range mv {
		word := *(*uint64)(unsafe.Pointer(&mv[index]))
		total += uint64(bits.OnesCount64(word))
	}

	return total
}

func writeControlTestMultivector(value *primitive.Value, start int, mv geometry.Multivector) {
	if value == nil {
		return
	}

	*(*geometry.Multivector)(unsafe.Pointer(&(*value)[start])) = mv
}

func readControlTestMultivector(value *primitive.Value, start int) geometry.Multivector {
	if value == nil {
		return geometry.Multivector{}
	}

	return *(*geometry.Multivector)(unsafe.Pointer(&(*value)[start]))
}
