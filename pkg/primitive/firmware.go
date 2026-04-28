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

	// Stage MaskTrue (^0) and every reserved literal before installing
	// the program words. Without this the predicate primitive reads its
	// threshold as zero and every non-predicate instruction sees mask=0,
	// which silently suppresses every body write.
	if entry.MaskTrueWord != 0 {
		value.Set(int(entry.MaskTrueWord), ^uint64(0))
	}

	for _, init := range entry.Constants {
		value.Set(int(init.Offset), init.Value)
	}

	// Apply install-time operand substitutions. The compiler emitted
	// `B.{{addr}}` / `{{addr}}` operands as placeholders and recorded a
	// patch table; we read the live word at addr from the Value (the
	// caller is expected to have written the desired operand offset there
	// before InstallFirmware) and splice its low 7 bits into the named
	// 7-bit field of the corresponding program word. After this loop the
	// program region holds concrete operand offsets the kernel can decode
	// without any indirection logic.
	words := append([]uint64(nil), entry.Compiled()...)

	for _, sub := range entry.Substitutions {
		if sub.PC < 0 || sub.PC >= len(words) {
			continue
		}

		addr := int(sub.Addr)
		if addr < 0 || addr >= len(*value) {
			continue
		}

		operand := (*value)[addr] & 0x7F

		words[sub.PC] &^= uint64(0x7F) << sub.FieldShift
		words[sub.PC] |= operand << sub.FieldShift
	}

	return value.InstallProgram(words)
}
