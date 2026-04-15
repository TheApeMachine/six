package vm

import (
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
LifecycleEngine is the single runtime seam for selecting the next in-Value
program and reading out generic post-execution actions.
*/
type LifecycleEngine struct {
	orchestrator *Orchestrator
	firmware     *programmer.Firmware
	finalizer    *ActionFinalizer
}

/*
NewLifecycleEngine binds boot-time firmware selection and post-ALU action
readout to one orchestrator-local runtime object.
*/
func NewLifecycleEngine(
	orchestrator *Orchestrator,
	firmware *programmer.Firmware,
	finalizer *ActionFinalizer,
) *LifecycleEngine {
	if firmware == nil {
		firmware = programmer.NewFirmware()
	}

	if finalizer == nil {
		finalizer = NewActionFinalizer(orchestrator)
	}

	return &LifecycleEngine{
		orchestrator: orchestrator,
		firmware:     firmware,
		finalizer:    finalizer,
	}
}

/*
SelectProgram returns the next boot/runtime program selected for a Value.
*/
func (engine *LifecycleEngine) SelectProgram(value *primitive.Value) string {
	if engine == nil || engine.firmware == nil || value == nil {
		return ""
	}

	return engine.firmware.Next(value)
}

/*
FinalizeValue runs generic value-scope readout after one ALU pass.
*/
func (engine *LifecycleEngine) FinalizeValue(value *primitive.Value) bool {
	if engine == nil || engine.finalizer == nil || value == nil {
		return false
	}

	return engine.finalizer.FinalizeValue(value)
}

/*
FinalizeField runs generic field-scope readout after routing updates a field.
*/
func (engine *LifecycleEngine) FinalizeField(
	scope string,
	value *primitive.Value,
	field *geometry.Field,
) bool {
	if engine == nil || engine.finalizer == nil || value == nil {
		return false
	}

	return engine.finalizer.FinalizeField(scope, value, field)
}
