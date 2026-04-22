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

func (value *Value) InstallProgram(words []uint64, schedulingNext uint64) bool {
	if value == nil || len(words) == 0 {
		return false
	}

	value.WriteProgramWords(words)
	value.SetSchedulingNext(schedulingNext)
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

	return value.InstallProgram(entry.Compiled(), entry.ResolveSchedulingNext(value.ID()))
}
