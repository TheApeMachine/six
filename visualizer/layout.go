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
	tokenWords := int((core.Cfg.TokenBits + 63) / 64)
	programWords := int((core.Cfg.ProgramBits + 63) / 64)
	registersStart := core.Cfg.R0
	registersWordCount := max(0, core.Cfg.RegPC-registersStart)

	fields := []ValueLayoutSpan{
		{
			Name:      "tokens",
			Kind:      "tokens",
			Label:     "Tokens",
			StartWord: core.Cfg.TokenIndex,
			WordCount: tokenWords,
			Bits:      int(core.Cfg.TokenBits),
		},
		{
			Name:      "value-id",
			Kind:      "identity",
			Label:     "Value ID",
			StartWord: core.Cfg.ValueID,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "prev-id",
			Kind:      "link",
			Label:     "Prev ID",
			StartWord: core.Cfg.PreviousID,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "next-id",
			Kind:      "link",
			Label:     "Next ID",
			StartWord: core.Cfg.NextID,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "state",
			Kind:      "state",
			Label:     "State",
			StartWord: core.Cfg.StateIndex,
			WordCount: 3,
			Bits:      192,
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
			StartWord: core.Cfg.AffinityIndex,
			WordCount: int((core.Cfg.AffinityBits + 63) / 64),
			Bits:      int(core.Cfg.AffinityBits),
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
			StartWord: core.Cfg.RegPC,
			WordCount: 1,
			Bits:      64,
		},
		{
			Name:      "program",
			Kind:      "program",
			Label:     "Program",
			StartWord: core.Cfg.ProgramIndex,
			WordCount: programWords,
			Bits:      int(core.Cfg.ProgramBits),
		},
	}

	opcodes := make([]string, 16)
	for i := range opcodes {
		opcodes[i] = telemetry.TruthOpName(uint8(i))
	}

	return ValueLayout{
		Words:      primitive.Words,
		ByteSize:   primitive.ByteSize,
		TokenBits:  core.Cfg.TokenBits,
		TokenWords: tokenWords,
		Indices: ValueLayoutIndex{
			ValueID:        core.Cfg.ValueID,
			PrevID:         core.Cfg.PreviousID,
			NextID:         core.Cfg.NextID,
			State:          core.Cfg.StateIndex,
			Sequence:       core.Cfg.StateSequence,
			Accumulator:    core.Cfg.StateAccumulator,
			ExecStatus:     primitive.ExecStatusWord,
			Affinity:       core.Cfg.AffinityIndex,
			RegistersStart: registersStart,
			PC:             core.Cfg.RegPC,
			Program:        core.Cfg.ProgramIndex,
			ProgramWords:   programWords,
			ProgramSlots:   programWords * 2,
		},
		Registers: map[string]int{
			"r0": core.Cfg.R0,
			"r1": core.Cfg.R1,
			"r2": core.Cfg.R2,
			"r3": core.Cfg.R3,
			"r4": core.Cfg.R4,
			"r5": core.Cfg.R5,
			"r6": core.Cfg.R6,
			"r7": core.Cfg.R7,
			"r8": core.Cfg.R8,
			"r9": core.Cfg.R9,
			"fw": core.Cfg.FW,
			"pc": core.Cfg.RegPC,
		},
		Fields:        fields,
		OpcodeNames:   opcodes,
		ExecExitNames: []string{"none", "exhausted", "halt-opcode", "bad-program-word"},
	}
}
