package vm

import "testing"

func TestNewMachineWithoutContextDoesNotPanic(t *testing.T) {
	var machine *Machine

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewMachine panicked without MachineWithContext: %v", r)
			}
		}()
		machine = NewMachine()
	}()

	if machine == nil {
		t.Fatalf("machine: got nil")
	}
	defer machine.Close()

	if machine.ctx == nil {
		t.Fatalf("machine context should be initialized")
	}
	if machine.cancel == nil {
		t.Fatalf("machine cancel should be initialized")
	}
}
