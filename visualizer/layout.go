package visualizer

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
ValueLayout describes the current in-memory Value map as JSON so the browser
can decode live binary frames without hardcoded offsets.
*/
type ValueLayout struct {
	Words         int               `json:"words"`
	ByteSize      int               `json:"byteSize"`
	TokenBits     uint64            `json:"tokenBits"`
	TokenWords    int               `json:"tokenWords"`
	Indices       ValueLayoutIndex  `json:"indices"`
	Registers     map[string]int    `json:"registers"`
	Fields        []ValueLayoutSpan `json:"fields"`
	OpcodeNames   []string          `json:"opcodeNames"`
	ExecExitNames []string          `json:"execExitNames"`
}

/*
ValueLayoutIndex exposes the word indices needed to decode a frame.
*/
type ValueLayoutIndex struct {
	ValueID        int `json:"valueId"`
	PrevID         int `json:"prevId"`
	NextID         int `json:"nextId"`
	State          int `json:"state"`
	Sequence       int `json:"sequence"`
	Accumulator    int `json:"accumulator"`
	ExecStatus     int `json:"execStatus"`
	Affinity       int `json:"affinity"`
	RegistersStart int `json:"registersStart"`
	PC             int `json:"pc"`
	Program        int `json:"program"`
	ProgramWords   int `json:"programWords"`
	ProgramSlots   int `json:"programSlots"`
}

/*
ValueLayoutSpan is a named region in the Value frame.
*/
type ValueLayoutSpan struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	StartWord int    `json:"startWord"`
	WordCount int    `json:"wordCount"`
	Bits      int    `json:"bits"`
}

/*
BuildValueLayout snapshots the current config into a browser-friendly shape.
*/
func BuildValueLayout() ValueLayout {
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	programWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	stateWords := (core.Cfg.Value.Region.State.Index + 63) / 64
	if stateWords < 1 {
		stateWords = 1
	}
	stateStart := core.Cfg.Value.Region.State.Index
	for _, w := range []int{core.Cfg.Value.Region.State.Sequence, core.Cfg.Value.Region.State.Accumulator} {
		if w < stateStart {
			stateStart = w
		}
	}
	registersStart := core.Cfg.Value.Region.Registers.R0
	registersWordCount := max(0, core.Cfg.Value.Region.Registers.PC-registersStart)

	fields := []ValueLayoutSpan{
		{
			Name:      "tokens",
			Kind:      "tokens",
			Label:     "Tokens",
			StartWord: core.Cfg.Value.Region.Tokens.Start,
			WordCount: tokenWords,
			Bits:      int(core.Cfg.Value.Region.Tokens.Bits),
		},
		{
			Name:      "value-id",
			Kind:      "identity",
			Label:     "Value ID",
			StartWord: core.Cfg.Value.Region.ID.Start,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "prev-id",
			Kind:      "link",
			Label:     "Prev ID",
			StartWord: core.Cfg.Value.Region.Prev.Start,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "next-id",
			Kind:      "link",
			Label:     "Next ID",
			StartWord: core.Cfg.Value.Region.Next.Start,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "state",
			Kind:      "state",
			Label:     "State",
			StartWord: stateStart,
			WordCount: stateWords,
			Bits:      core.Cfg.Value.Region.State.Index,
		},
		{
			Name:      "exec-status",
			Kind:      "exec",
			Label:     "Exec Status",
			StartWord: primitive.ExecStatusWord,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "affinity",
			Kind:      "affinity",
			Label:     "Affinity",
			StartWord: core.Cfg.Value.Region.Affinity.Start,
			WordCount: int((core.Cfg.Value.Region.Affinity.Bits + 63) / 64),
			Bits:      int(core.Cfg.Value.Region.Affinity.Bits),
		},
		{
			Name:      "registers",
			Kind:      "registers",
			Label:     "Registers",
			StartWord: registersStart,
			WordCount: registersWordCount,
			Bits:      registersWordCount * 64,
		},
		{
			Name:      "pc",
			Kind:      "pc",
			Label:     "PC",
			StartWord: core.Cfg.Value.Region.Registers.PC,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "program",
			Kind:      "program",
			Label:     "Program",
			StartWord: core.Cfg.Value.Region.Program.Start,
			WordCount: programWords,
			Bits:      int(core.Cfg.Value.Region.Program.Bits),
		},
	}

	opcodes := make([]string, 16)
	for i := range opcodes {
		opcodes[i] = telemetry.TruthOpName(uint8(i))
	}

	return ValueLayout{
		Words:      primitive.Words,
		ByteSize:   primitive.ByteSize,
		TokenBits:  core.Cfg.Value.Region.Tokens.Bits,
		TokenWords: tokenWords,
		Indices: ValueLayoutIndex{
			ValueID:        core.Cfg.Value.Region.ID.Start,
			PrevID:         core.Cfg.Value.Region.Prev.Start,
			NextID:         core.Cfg.Value.Region.Next.Start,
			State:          core.Cfg.Value.Region.State.Index,
			Sequence:       core.Cfg.Value.Region.State.Sequence,
			Accumulator:    core.Cfg.Value.Region.State.Accumulator,
			ExecStatus:     primitive.ExecStatusWord,
			Affinity:       core.Cfg.Value.Region.Affinity.Start,
			RegistersStart: registersStart,
			PC:             core.Cfg.Value.Region.Registers.PC,
			Program:        core.Cfg.Value.Region.Program.Start,
			ProgramWords:   programWords,
			ProgramSlots:   programWords * 2,
		},
		Registers: map[string]int{
			"r0": core.Cfg.Value.Region.Registers.R0,
			"r1": core.Cfg.Value.Region.Registers.R1,
			"r2": core.Cfg.Value.Region.Registers.R2,
			"r3": core.Cfg.Value.Region.Registers.R3,
			"r4": core.Cfg.Value.Region.Registers.R4,
			"r5": core.Cfg.Value.Region.Registers.R5,
			"r6": core.Cfg.Value.Region.Registers.R6,
			"r7": core.Cfg.Value.Region.Registers.R7,
			"r8": core.Cfg.Value.Region.Registers.R8,
			"r9": core.Cfg.Value.Region.Registers.R9,
			"fw": core.Cfg.Value.Region.Registers.FW,
			"pc": core.Cfg.Value.Region.Registers.PC,
		},
		Fields:        fields,
		OpcodeNames:   opcodes,
		ExecExitNames: []string{"none", "exhausted", "halt-opcode", "bad-program-word"},
	}
}
