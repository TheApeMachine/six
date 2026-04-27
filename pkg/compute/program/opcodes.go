package program

/*
DefaultTruthOpcodes are 4-bit truth-table indices. Geometric ops use the high nibble.
Layout.Opcodes from YAML overrides names that appear in both maps.
*/
var DefaultTruthOpcodes = map[string]uint64{
	"false":    0x0,
	"and":      0x1,
	"aandnotb": 0x2,
	"a":        0x3,
	"notandb":  0x4,
	"b":        0x5,
	"xor":      0x6,
	"or":       0x7,
	"nor":      0x8,
	"xnor":     0x9,
	"notb":     0xA,
	"ifbthena": 0xB,
	"nota":     0xC,
	"ifathenb": 0xD,
	"nand":     0xE,
	"true":     0xF,
	"compose":  0x10,
	"sandwich": 0x20,
	"reverse":  0x30,
}

/*
Opcodes is the default table tests use when they pass a partial Layout; Compile fills
from Layout via mergeOpcodes.
*/
var Opcodes map[string]uint64

func init() {
	Opcodes = MergeOpcodes(Layout{Opcodes: nil})
}
