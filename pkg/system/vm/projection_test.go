package vm

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestMachineBootsAllSystems(t *testing.T) {
	gc.Convey("Given a Machine", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		gc.Convey("It should boot the tokenizer", func() {
			gc.So(machine.booter.tok.IsValid(), gc.ShouldBeTrue)
		})

		gc.Convey("It should boot the spatial index", func() {
			gc.So(machine.booter.spatialIndex.IsValid(), gc.ShouldBeTrue)
		})

		gc.Convey("It should boot the graph substrate", func() {
			gc.So(machine.booter.graph.IsValid(), gc.ShouldBeTrue)
		})

		gc.Convey("It should boot the cantilever", func() {
			gc.So(machine.booter.cantilever.IsValid(), gc.ShouldBeTrue)
		})
	})
}
