/*
Package stepwise implements the fixed-step truth-table slice executor: a
SIMT-friendly contract where every run performs exactly len(program) steps with
identical control flow, and each step applies one opcode (truth-table 0x0–0xF,
extended ALU up to 0x13, or IMM) to operand words.

APIs take *[FrameWords]uint64 (128 words), layout-compatible with *primitive.Value.

Memory map (external program slice — reference and tests)

	Each element of program is one uint64 step descriptor unless bit 63 selects IMM:
	  TT descriptors: bits 0:7 opcode, 8:14 idxA, 16:22 idxB, 24:30 idxDst,
	    31 leftFromB, 32 rightFromB, bits 33:62 zero, bit 63 clear
	  IMM: bit 63 set; bits 8:23 imm16; bits 24:30 dst; bits 0:7 zero

Embedded program band (production layout, matches cmd/cfg/config.yml defaults)

	Word at program.base is a header:

	  bits 0:15   descriptor count after the header
	  bits 16:47  zero
	  bits 48:63  EmbeddedHeaderMagic (0x5A17)

	Descriptors follow at program.base+1. Legacy RISC firmware does not use this
	header; compute.Backend runs RunEmbeddedPair when DetectEmbeddedStepwise is true.

	config: programsStepwise.* strings are compiled with CompileDescriptors and
	installed in installFirmware when system.stepwiseUniversalBitwise is true
	(see primitive.installFirmware).

Operations

	RunScalar, RunPair, RunEmbedded, RunEmbeddedPair, RunHomogeneousBatch, InstallEmbedded.

See EncodeStep / DecodeStep for TT wire format; EncodeImm for immediate steps.
*/
package stepwise
