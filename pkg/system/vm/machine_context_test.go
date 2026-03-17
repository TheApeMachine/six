package vm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestNewMachineWithoutContextDoesNotPanic verifies that NewMachine initializes
its own context and cancel function if they are not provided via options,
preventing nil pointer dereferences.
*/
func TestNewMachineWithoutContextDoesNotPanic(t *testing.T) {
	gc.Convey("Given no MachineWithContext", t, func() {
		var machine *Machine

		gc.Convey("When creating a new Machine", func() {
			gc.So(func() {
				machine = NewMachine()
			}, gc.ShouldNotPanic)

			gc.Convey("Then it should not be nil", func() {
				gc.So(machine, gc.ShouldNotBeNil)
			})

			gc.Convey("And it should have an initialized context and cancel", func() {
				gc.So(machine.ctx, gc.ShouldNotBeNil)
				gc.So(machine.cancel, gc.ShouldNotBeNil)
			})

			if machine != nil {
				machine.Close()
			}
		})
	})
}
