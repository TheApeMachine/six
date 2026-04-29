package program

import "fmt"

/*
DefaultMaskTrueWord is the frame word carrying the canonical all-ones mask
bitmap used by truth-table and predicate emits. Seventy-two matches resident
compiled defaults until Layout exposes an explicit mask slot selector.
*/
const DefaultMaskTrueWord uint64 = 72

/*
Compiler lowers canonical ProgramIR values against a layout snapshot.
It exists alongside Compile so in-value program authors do not need to emit
feed text just to get resident machine words.
TODO: Route layout through LowerIR when IR resolution needs live region metadata.
*/
type Compiler struct {
	layout Layout
}

func NewCompiler(layout Layout) *Compiler {
	return &Compiler{layout: layout}
}

func EncodeProgramIR(ir ProgramIR, layout Layout) (Compiled, error) {
	return NewCompiler(layout).EncodeIR(ir)
}

func (compiler *Compiler) EncodeIR(ir ProgramIR) (Compiled, error) {
	if compiler == nil {
		return Compiled{}, fmt.Errorf("program: compiler is nil")
	}

	ops, err := compiler.LowerIR(ir)
	if err != nil {
		return Compiled{}, err
	}

	words := make([]uint64, 16)
	for idx, op := range ops {
		if ir.Slots[idx].Empty {
			continue
		}

		if err := op.Validate(); err != nil {
			return Compiled{}, fmt.Errorf("program: slot %d: %w", idx, err)
		}

		words[idx] = op.Pack()
		if words[idx] == 0 {
			return Compiled{}, fmt.Errorf("program: slot %d packs to empty word", idx)
		}
	}

	constants := append([]ConstantInit{}, ir.Constants...)

	return Compiled{
		Words:        words,
		Constants:    constants,
		MaskTrueWord: DefaultMaskTrueWord,
	}, nil
}

func (compiler *Compiler) LowerIR(ir ProgramIR) ([]MachineOp, error) {
	if compiler == nil {
		return nil, fmt.Errorf("program: compiler is nil")
	}

	if len(ir.Slots) > 16 {
		return nil, fmt.Errorf("program: %q exceeds 16-slot sweep by %d", ir.Name, len(ir.Slots)-16)
	}

	ops := make([]MachineOp, 0, len(ir.Slots))
	for _, slot := range ir.Slots {
		if slot.Empty {
			ops = append(ops, MachineOp{})
			continue
		}

		ops = append(ops, slot.Op)
	}

	return ops, nil
}

func (op MachineOp) Pack() uint64 {
	var word uint64
	word |= op.Opcode & 0xF
	word |= (op.AStart & InstrStartMask) << InstrAStartShift
	word |= ((op.ASpan - 1) & InstrSpanMask) << InstrASpanShift
	word |= (op.BStart & InstrStartMask) << InstrBStartShift
	word |= ((op.BSpan - 1) & InstrSpanMask) << InstrBSpanShift
	word |= (op.DstStart & InstrStartMask) << InstrDstStartShift
	word |= ((op.DstSpan - 1) & InstrSpanMask) << InstrDstSpanShift
	word |= (op.MaskStart & InstrStartMask) << InstrPredStartShift

	target := uint64(0)
	if op.TargetChild || op.Emit {
		target = 2
	} else if op.TargetB {
		target = 1
	}
	word |= (target & 3) << InstrModeShift

	word |= (op.Topology & 3) << InstrTopologyShift

	if op.Predicate {
		word |= 1 << InstrPredBitShift
	}

	word |= (op.PredicateCond & 7) << InstrPredCondShift

	if op.SrcAFromB {
		word |= 1 << InstrSrcAFromBShift
	}

	if op.Stage {
		word |= 1 << InstrStageShift
	}

	if op.PopEnd {
		word |= 1 << InstrPopEndShift
	}

	return word
}

func (op MachineOp) Validate() error {
	if op.Opcode > 0xF {
		return fmt.Errorf("opcode 0x%x exceeds 4-bit field", op.Opcode)
	}

	if op.Topology > 3 {
		return fmt.Errorf("topology %d exceeds 2-bit field", op.Topology)
	}

	if op.PredicateCond > 7 {
		return fmt.Errorf("predicate condition %d exceeds 3-bit field", op.PredicateCond)
	}

	if err := validateStartSpan("A", op.AStart, op.ASpan); err != nil {
		return err
	}

	if err := validateStartSpan("B", op.BStart, op.BSpan); err != nil {
		return err
	}

	if err := validateStartSpan("dst", op.DstStart, op.DstSpan); err != nil {
		return err
	}

	if op.MaskStart > InstrStartMask {
		return fmt.Errorf("mask start %d exceeds 7-bit field", op.MaskStart)
	}

	return nil
}

func validateStartSpan(name string, start, span uint64) error {
	if start > InstrStartMask {
		return fmt.Errorf("%s start %d exceeds 7-bit field", name, start)
	}

	if span == 0 {
		return fmt.Errorf("%s span must be positive", name)
	}

	if span > InstrSpanMask+1 {
		return fmt.Errorf("%s span %d exceeds 7-bit field", name, span)
	}

	return nil
}
