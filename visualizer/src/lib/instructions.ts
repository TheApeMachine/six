import type { DecodedInstruction } from "./programsGenerated";

/*
Truth-table opcode names mirror the 4-bit table the universal bitwise
ALU executes. The high nibble is the projective geometric algebra lane;
the kernel keeps the full byte before falling back to the low nibble,
so a geometric op cannot collapse to FALSE. Names come straight from
the compiler — keeping them in one place keeps the dashboard honest if
the table ever grows.
*/
const TRUTH_TABLE_OPS: Record<number, string> = {
	0: "FALSE",
	1: "AND",
	2: "A ∧ ¬B",
	3: "A",
	4: "¬A ∧ B",
	5: "B",
	6: "XOR",
	7: "OR",
	8: "NOR",
	9: "XNOR",
	10: "¬B",
	11: "A ∨ ¬B",
	12: "¬A",
	13: "¬A ∨ B",
	14: "NAND",
	15: "TRUE",
	16: "PGA Compose",
	32: "PGA Sandwich",
	48: "PGA Reverse",
};

const TOPOLOGY_NAMES: Record<number, string> = {
	0: "local",
	1: "pop(B)",
	2: "gossip(B)",
};

const PREDICATE_CONDS: Record<number, string> = {
	0: "<",
	1: "≤",
	2: ">",
	3: "≥",
	4: "==",
	5: "!=",
	6: "store popcnt",
	7: "any-zero",
};

const GEOMETRIC_CONTRACTS: Record<number, string> = {
	16: "Context • Gradient → Signals",
	32: "Context • Gradient • Context† → Signals",
	48: "Context† → Signals",
};

export function opcodeName(opcode: number): string {
	return TRUTH_TABLE_OPS[opcode] ?? `op 0x${opcode.toString(16)}`;
}

export function topologyName(topology: number): string {
	return TOPOLOGY_NAMES[topology] ?? `topo ${topology}`;
}

function operand(side: "A" | "B" | "DST", start: number, span: number): string {
	if (span <= 1) {
		return `${side}[${start}]`;
	}

	return `${side}[${start},${span}]`;
}

/*
formatInstruction renders one DecodedInstruction as a single line. The
shape is: "OP A-operand • B-operand → DST  // topology, modifiers". Pop
end, stage, and emit bits are surfaced as suffixes so a reader can tell
whether a row consumes the lane, stages a peer, or spawns a child
without decoding raw bits by hand.
*/
export function formatInstruction(instruction: DecodedInstruction): string {
	const geometricContract = GEOMETRIC_CONTRACTS[instruction.opcode];
	if (geometricContract) {
		return `${opcodeName(instruction.opcode)} ${geometricContract}`;
	}

	const head = `${opcodeName(instruction.opcode)} ${operand(
		"A",
		instruction.aStart,
		instruction.aSpan,
	)} • ${operand("B", instruction.bStart, instruction.bSpan)} → ${operand(
		"DST",
		instruction.dstStart,
		instruction.dstSpan,
	)}`;

	const flags: string[] = [topologyName(instruction.topology)];

	if (instruction.predicate) {
		flags.push(
			`pred ${PREDICATE_CONDS[instruction.predCond] ?? instruction.predCond} @w${instruction.predStart}`,
		);
	}

	if (instruction.stage) {
		flags.push("stage(B)");
	}

	if (instruction.emit) {
		flags.push("emit");
	}

	if (instruction.popEnd) {
		flags.push("pop-end");
	}

	if (instruction.srcAFromB) {
		flags.push("A←B");
	}

	return `${head}  // ${flags.join(", ")}`;
}
