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
	0x00: "FALSE",
	0x01: "AND",
	0x02: "A ∧ ¬B",
	0x03: "A",
	0x04: "¬A ∧ B",
	0x05: "B",
	0x06: "XOR",
	0x07: "OR",
	0x08: "NOR",
	0x09: "XNOR",
	0x0a: "¬B",
	0x0b: "A ∨ ¬B",
	0x0c: "¬A",
	0x0d: "¬A ∨ B",
	0x0e: "NAND",
	0x0f: "TRUE",
	0x10: "PGA Compose",
	0x20: "PGA Sandwich",
	0x30: "PGA Reverse",
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

	return `${head}  ${flags.length ? `// ${flags.join(", ")}` : ""}`;
}
