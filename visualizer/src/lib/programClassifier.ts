/*
programClassifier identifies which in-band program a Value is currently
carrying by matching its compiled program region against the signatures
produced by cmd/cfg/config.yml. Each DSL line lowers to a
(srcA, srcB, dst, opcode) tuple with absolute word starts and spans, and
because those tuples come out of the compiler deterministically we can
match them back to the program name cheaply in the visualizer without
asking the Go side to stamp a name tag.

When multiple programs share the same first line (episodic_replay and
unsupervised_learn are genuinely identical; causal_explore / causal_hub
/ surprisal / falsification all share their tokens-vs-context opening
line) we surface the primitive's CATEGORY. The shape and colour in the
canvas key off category, so the operator sees what kind of work is
happening even when the specific program is aliased at the wire level.
*/

import type { DecodedProgramWire } from "./valueRegions";

/*
ProgramCategory groups programs with the same visual glyph. Shape and
colour are chosen per category, not per program, so families that share
an ALU primitive remain readable in the canvas.
*/
export type ProgramCategory =
	| "plumbing" // link, affinity — bootstrap rules
	| "beam" // beam_swarm_step — directional explore
	| "inference" // active_inference — multi-future simulation
	| "classify" // classify_readout — label broadcast
	| "peer_gap" // unsupervised_learn / episodic_replay — peer comparison
	| "intervene" // Pearl L2 do-operation
	| "gap_probe" // surprisal / causal_explore / causal_hub / falsification
	| "resident" // measure_field — field-level resident
	| "util" // popcount, coupling, temperature
	| "unknown";

export type Shape =
	| "square"
	| "triangle_up"
	| "triangle_down"
	| "diamond"
	| "pentagon"
	| "hourglass"
	| "asterisk"
	| "ring"
	| "bar"
	| "circle";

export interface ProgramCategoryStyle {
	category: ProgramCategory;
	label: string;
	color: [number, number, number];
	shape: Shape;
	description: string;
}

export const PROGRAM_CATEGORIES: Record<ProgramCategory, ProgramCategoryStyle> =
	{
		plumbing: {
			category: "plumbing",
			label: "plumbing",
			color: [120, 120, 150],
			shape: "circle",
			description: "link / affinity bootstrap",
		},
		beam: {
			category: "beam",
			label: "beam",
			color: [0, 255, 150],
			shape: "triangle_up",
			description: "beam_swarm_step — directional next-span probe",
		},
		inference: {
			category: "inference",
			label: "inference",
			color: [255, 150, 50],
			shape: "pentagon",
			description: "active_inference — multi-future simulation",
		},
		classify: {
			category: "classify",
			label: "classify",
			color: [100, 180, 255],
			shape: "square",
			description: "classify_readout — label broadcast",
		},
		peer_gap: {
			category: "peer_gap",
			label: "peer gap",
			color: [180, 100, 255],
			shape: "hourglass",
			description: "unsupervised_learn / episodic_replay",
		},
		intervene: {
			category: "intervene",
			label: "intervene",
			color: [255, 80, 180],
			shape: "asterisk",
			description: "Pearl L2 do(X) — severed-history perturbation",
		},
		gap_probe: {
			category: "gap_probe",
			label: "gap probe",
			color: [255, 200, 0],
			shape: "diamond",
			description: "surprisal / causal / falsification",
		},
		resident: {
			category: "resident",
			label: "resident",
			color: [0, 200, 255],
			shape: "ring",
			description: "measure_field — field-level resident",
		},
		util: {
			category: "util",
			label: "util",
			color: [160, 160, 200],
			shape: "bar",
			description: "popcount / coupling / temperature",
		},
		unknown: {
			category: "unknown",
			label: "data",
			color: [140, 140, 160],
			shape: "square",
			description: "no program installed",
		},
	};

/*
Opcodes come from pkg/compute/kernel/layout.go. Only the low four bits
are used here — high-nibble opcodes (geometric, copy-mask-merge, emit
clone) are not produced by the DSL programs we try to classify.
*/
const OP_AND = 0x01;
const OP_XOR = 0x06;
const OP_OR = 0x07;

interface ProgramSignature {
	program: string;
	category: ProgramCategory;
	srcA: { start: number; span: number };
	srcB: { start: number; span: number };
	dst: { start: number; span: number };
	opcode: number;
}

/*
Region word starts (absolute): tokens=0, program=16, signals=24,
context=32, gradient=40, properties=48, asset=72, prev=120, next=121,
id=122, affinity=123. Widths come from the Value region map.
*/
const R = {
	tokens: 0,
	program: 16,
	signals: 24,
	context: 32,
	gradient: 40,
	properties: 48,
	asset: 72,
	prev: 120,
	next: 121,
	affinity: 123,
};

/*
PROGRAM_SIGNATURES lists the first DSL line of every named program we
render distinctly. Shared first lines intentionally map several program
names to the same entry by sharing the same category — the signature is
for the primitive, the description stays fuzzy when aliased.

If the Go-side config changes these lines, the visualizer falls back to
"unknown" rather than guessing, so the canvas degrades gracefully.
*/
const PROGRAM_SIGNATURES: ProgramSignature[] = [
	{
		program: "link",
		category: "plumbing",
		srcA: { start: R.asset, span: 1 },
		srcB: { start: R.asset, span: 1 },
		dst: { start: R.prev, span: 1 },
		opcode: OP_OR,
	},
	{
		program: "link", // second line
		category: "plumbing",
		srcA: { start: R.asset + 1, span: 1 },
		srcB: { start: R.asset + 1, span: 1 },
		dst: { start: R.next, span: 1 },
		opcode: OP_OR,
	},
	{
		program: "affinity",
		category: "plumbing",
		srcA: { start: R.tokens, span: 16 },
		srcB: { start: R.tokens, span: 16 },
		dst: { start: R.affinity, span: 5 },
		opcode: OP_XOR,
	},
	{
		program: "beam_swarm_step",
		category: "beam",
		srcA: { start: R.tokens, span: 8 },
		srcB: { start: R.gradient, span: 8 },
		dst: { start: R.context, span: 8 },
		opcode: OP_XOR,
	},
	{
		program: "active_inference",
		category: "inference",
		srcA: { start: R.tokens, span: 8 },
		srcB: { start: R.gradient, span: 8 },
		dst: { start: R.asset, span: 8 },
		opcode: OP_XOR,
	},
	{
		program: "classify_readout",
		category: "classify",
		srcA: { start: R.properties, span: 1 },
		srcB: { start: R.properties, span: 1 },
		dst: { start: R.signals, span: 1 },
		opcode: OP_OR,
	},
	{
		program: "peer_gap", // unsupervised_learn & episodic_replay share this
		category: "peer_gap",
		srcA: { start: R.context, span: 8 },
		srcB: { start: R.asset + 8, span: 8 },
		dst: { start: R.signals, span: 8 },
		opcode: OP_XOR,
	},
	{
		program: "intervene",
		category: "intervene",
		srcA: { start: R.context, span: 8 },
		srcB: { start: R.asset + 16, span: 8 },
		dst: { start: R.signals, span: 8 },
		opcode: OP_XOR,
	},
	{
		program: "gap_probe", // surprisal / causal_explore / causal_hub / falsification
		category: "gap_probe",
		srcA: { start: R.tokens, span: 8 },
		srcB: { start: R.context, span: 8 },
		dst: { start: R.signals, span: 8 },
		opcode: OP_XOR,
	},
	{
		program: "measure_field",
		category: "resident",
		srcA: { start: R.asset, span: 8 },
		srcB: { start: R.asset, span: 8 },
		dst: { start: R.signals + 7, span: 1 },
		opcode: OP_OR,
	},
	{
		program: "popcount",
		category: "util",
		srcA: { start: R.affinity, span: 5 },
		srcB: { start: R.affinity, span: 5 },
		dst: { start: R.affinity + 4, span: 1 },
		opcode: OP_XOR,
	},
	{
		program: "coupling",
		category: "util",
		srcA: { start: R.tokens, span: 16 },
		srcB: { start: R.affinity, span: 5 },
		dst: { start: R.signals, span: 1 },
		opcode: OP_AND,
	},
	{
		program: "temperature",
		category: "util",
		srcA: { start: R.properties + 4, span: 1 },
		srcB: { start: R.affinity, span: 5 },
		dst: { start: R.affinity, span: 5 },
		opcode: OP_XOR,
	},
];

export interface ClassifiedProgram {
	program: string;
	category: ProgramCategory;
	style: ProgramCategoryStyle;
}

/*
classifyProgramWire returns the identified program plus the category
style used by the canvas. A nil or all-zero program region maps to
"unknown" so fresh Values that have not yet hit the ALU render as
plain data squares instead of being mislabeled.
*/
export function classifyProgramWire(
	wire: DecodedProgramWire | null,
): ClassifiedProgram {
	if (!wire) {
		return unknownProgram();
	}

	const op = wire.opcodeLow & 0x0f;
	const a = wire.srcA;
	const b = wire.srcB;
	const d = wire.dst;

	if (
		op === 0 &&
		a.start === 0 &&
		a.span === 0 &&
		b.start === 0 &&
		b.span === 0 &&
		d.start === 0 &&
		d.span === 0
	) {
		return unknownProgram();
	}

	for (const sig of PROGRAM_SIGNATURES) {
		if (
			sig.opcode === op &&
			sig.srcA.start === a.start &&
			sig.srcA.span === a.span &&
			sig.srcB.start === b.start &&
			sig.srcB.span === b.span &&
			sig.dst.start === d.start &&
			sig.dst.span === d.span
		) {
			return {
				program: sig.program,
				category: sig.category,
				style: PROGRAM_CATEGORIES[sig.category],
			};
		}
	}

	return {
		program: `op=0x${op.toString(16).padStart(2, "0")}`,
		category: "unknown",
		style: PROGRAM_CATEGORIES.unknown,
	};
}

function unknownProgram(): ClassifiedProgram {
	return {
		program: "",
		category: "unknown",
		style: PROGRAM_CATEGORIES.unknown,
	};
}

/*
ROLE_BY_CATEGORY promotes a category into one of the historical ValueRole
buckets used by the older UI (data / action / reaction / prompt) so the
rest of the store keeps working while we migrate callers to read
category directly. Classifier-unknown stays as "data" because those
Values haven't installed a program and belong with raw tokens.
*/
export const ROLE_BY_CATEGORY: Record<
	ProgramCategory,
	"data" | "action" | "reaction" | "prompt"
> = {
	plumbing: "data",
	beam: "action",
	inference: "action",
	classify: "reaction",
	peer_gap: "reaction",
	intervene: "action",
	gap_probe: "action",
	resident: "action",
	util: "data",
	unknown: "data",
};
