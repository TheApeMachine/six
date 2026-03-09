package geometry

/*
SubstrateOpcode is the Go-side mirror of the opcode bands used in FINALDEMO.txt.
The opcode is derived entirely from GF(257) geometry, not from an external program.
*/
type SubstrateOpcode string

const (
	OpcodeRotateX SubstrateOpcode = "ROTATE_X"
	OpcodeRotateY SubstrateOpcode = "ROTATE_Y"
	OpcodeRotateZ SubstrateOpcode = "ROTATE_Z"
	OpcodeAlign   SubstrateOpcode = "ALIGN"
	OpcodeSearch  SubstrateOpcode = "SEARCH"
	OpcodeSync    SubstrateOpcode = "SYNC"
	OpcodeFork    SubstrateOpcode = "FORK"
	OpcodeCompose SubstrateOpcode = "COMPOSE"
)

const (
	OpcodeBandRotate = "rotate"
	OpcodeBandStable = "stable"
	OpcodeBandGrowth = "growth"
)

/*
Band classifies the opcode into the same volatile/stable/growth bands used by
the FINALDEMO substrate visualization.
*/
func (opcode SubstrateOpcode) Band() string {
	switch opcode {
	case OpcodeRotateX, OpcodeRotateY, OpcodeRotateZ:
		return OpcodeBandRotate
	case OpcodeFork, OpcodeCompose:
		return OpcodeBandGrowth
	default:
		return OpcodeBandStable
	}
}

/*
DeriveSubstrateOpcode maps two GF(257) states into a substrate opcode.
The face256 registers are summed modulo 257 and bucketed into the same ranges
as the FINALDEMO proof of concept.
*/
func DeriveSubstrateOpcode(source, target GFRotation) SubstrateOpcode {
	register := (source.Face256Source() + target.Face256Source()) % CubeFaces

	switch {
	case register < 32:
		return OpcodeRotateX
	case register < 64:
		return OpcodeRotateY
	case register < 96:
		return OpcodeRotateZ
	case register < 128:
		return OpcodeAlign
	case register < 160:
		return OpcodeSearch
	case register < 192:
		return OpcodeSync
	case register < 220:
		return OpcodeFork
	default:
		return OpcodeCompose
	}
}
