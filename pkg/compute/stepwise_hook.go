package compute

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/stepwise"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func init() {
	primitive.SetStepwiseInstallFunc(tryInstallStepwiseFirmware)
}

/*
tryInstallStepwiseFirmware writes programsStepwise.* into the program band when
Cfg carries a non-empty source for that firmware slot. Keeps primitive free of
any import of stepwise (cpu/kernel tests import primitive).
*/
func tryInstallStepwiseFirmware(value *primitive.Value, fw core.FirmwareType) bool {
	src := core.Cfg.StepwiseFirmwareSource[fw]

	if src == "" {
		return false
	}

	desc, err := stepwise.CompileDescriptors(src)

	if err != nil || len(desc) == 0 {
		return false
	}

	_ = stepwise.InstallEmbedded((*[stepwise.FrameWords]uint64)(unsafe.Pointer(value)), desc)

	return true
}
