package primitive

import "github.com/theapemachine/six/pkg/core"

func (value *Value) HasProgram() bool {
	if value == nil {
		return false
	}

	program := value.Get(ProgramRegion)
	for _, word := range program {
		if word != 0 {
			return true
		}
	}

	return false
}

func (value *Value) ClearProgram() {
	if value == nil {
		return
	}

	start, n := core.Cfg.Value.Region.Program.WordExtent()
	for idx := 0; idx < n; idx++ {
		value.Set(start+idx, 0)
	}
}

func (value *Value) ReadyForALU() bool {
	if value == nil || value.Status() != READY || value.SchedulingNext() == 0 {
		return false
	}

	return value.HasProgram()
}

// InstallProgram installs a packed instruction buffer directly into the Value's
// program region and sets its status to READY.
func (value *Value) InstallProgram(words []uint64) bool {
	if value == nil || len(words) == 0 {
		return false
	}

	value.WriteProgramWords(words)
	value.SetProperty(CONTINUATION, value.ID())
	value.SetStatus(READY)
	return true
}

func (value *Value) InstallFirmware(firmware core.FirmwareType) bool {
	if value == nil || core.Cfg == nil {
		return false
	}

	entry, ok := core.Cfg.Programs[firmware]
	if !ok || len(entry.Compiled()) == 0 {
		return false
	}

	return value.InstallProgram(entry.Compiled())
}
