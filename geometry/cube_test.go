package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestCubeInject_UsesSparsePatchLanes(t *testing.T) {
	Convey("Given a cube and an injected payload", t, func() {
		cube := NewCube()
		payload := data.BaseChord('A')
		carrier := data.BaseChord('Z')

		cube.Inject(17, payload, carrier, IdentityRotation())

		Convey("It should write the logical face into only a small number of lanes", func() {
			written := 0
			for side := range 6 {
				for rot := range 4 {
					chord := cube.Get(side, rot, 17)
					if chord.ActiveCount() > 0 {
						written++
					}
				}
			}

			So(written, ShouldEqual, cubeInjectionLanes)
		})
	})
}

func TestCubeInjectControl_WritesFace256Sparsely(t *testing.T) {
	Convey("Given a cube and a control program", t, func() {
		cube := NewCube()
		program := data.BaseChord('P')
		carrier := data.BaseChord('C')

		cube.InjectControl(program, carrier, DefaultRotTable.Y90)

		Convey("It should stamp face 256 into only the configured control lanes", func() {
			written := 0
			for side := 0; side < 6; side++ {
				for rot := range 4 {
					gate := cube.Face256(side, rot)
					if gate.ActiveCount() > 0 {
						written++
					}
				}
			}

			So(written, ShouldEqual, cubeInjectionLanes)
		})
	})
}

func BenchmarkCubeInject(b *testing.B) {
	cube := NewCube()
	payload := data.BaseChord('Q')
	carrier := data.BaseChord('R')

	for i := 0; i < b.N; i++ {
		cube.Inject(42, payload, carrier, DefaultRotTable.Z90)
	}
}
