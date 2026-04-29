package cpu

import (
	"context"
	"fmt"
	"math"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestGeometricFrame(t *testing.T) {
	opcodes := []uint64{0x10, 0x20, 0x30}

	Convey("Given the CPU geometric lane", t, func() {
		for _, opcode := range opcodes {
			Convey(fmt.Sprintf("When opcode 0x%x executes", opcode), func() {
				reference := geometricFixture()
				actual := geometricFixture()

				So(geometricFrameGeneric(unsafe.Pointer(&reference), opcode), ShouldBeTrue)
				So(GeometricFrame(unsafe.Pointer(&actual), opcode), ShouldBeTrue)

				Convey("It should match the scalar reference signals", func() {
					for word := 32; word < 40; word++ {
						So(actual[word], ShouldEqual, reference[word])
					}
				})
			})
		}
	})

	Convey("Given a resident program with a raw compose slot", t, func() {
		reference := geometricFixture()
		actual := geometricFixture()
		actual[ProgramStartWord] = 0x10

		So(GeometricFrame(unsafe.Pointer(&reference), 0x10), ShouldBeTrue)

		backend := NewBackend(context.Background())
		defer backend.Close()

		Convey("When HypercubeGossip runs the program", func() {
			_, err := backend.HypercubeGossip(&actual, []*primitive.Value{&actual})
			So(err, ShouldBeNil)

			Convey("It should execute the geometric frame contract", func() {
				for word := 32; word < 40; word++ {
					So(actual[word], ShouldEqual, reference[word])
				}
			})
		})
	})
}

func geometricFixture() primitive.Value {
	var value primitive.Value

	for lane := 0; lane < 8; lane++ {
		left := float64(lane+1) * 0.25
		right := float64((lane+3)*(lane+5)) * 0.125

		value[40+lane] = math.Float64bits(left)
		value[48+lane] = math.Float64bits(right)
	}

	return value
}
