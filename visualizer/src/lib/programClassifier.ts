/*
programClassifier identifies which named DSL program a Value is currently
carrying by matching its program region against PROGRAM_SIGNATURES — a
table that gen.go writes out from cmd/cfg/config.yml's `programs:` block,
lowered through the same compiler the runtime uses. There is no
hand-maintained signature table here; if a program appears in the YAML
it is matchable in the visualiser, and if it does not appear it falls
through to "unknown" rather than being mis-named.

Each match is exact: same opcode, same operand tuples, same order. A
shorter prefix match is rejected so two programs that share their first
line (e.g. `affinity` and `fold_substrate`) keep their distinct identity.

Categorisation is a small table mapping program names to glyph
families. It still lives here because the category is a UI concept (what
shape and colour to draw), not a substrate concept; the substrate only
knows program names.
*/

import {
	type DecodedInstruction,
	PROGRAM_SIGNATURES,
} from "./programsGenerated";

/*
ProgramCategory groups programs with the same visual glyph. Shape and
colour are chosen per category, not per program, so families that share
an ALU primitive remain readable in the canvas.
*/
export type ProgramCategory =
	| "plumbing" // link, affinity — bootstrap rules
	| "structural" // fold_substrate — README "Signals" cancel/merge sweep
	| "beam" // beam_swarm_step — directional explore
	| "inference" // active_inference — multi-future simulation
	| "classify" // classify_readout — label broadcast
	| "peer_gap" // unsupervised_learn / episodic_replay — peer comparison
	| "consensus" // vote_swarm — XOR-accumulate peer.signals into context
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
	| "concentric"
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
		/*
		structural is the README "Signals" algorithm: an XOR-cancel sweep
		into signals[0,4] and an AND-merge sweep into signals[4,4] over
		the two halves of the token region. The post-ALU hook scans
		those for long zero/one runs and emits Association Values, so
		this category marks the Values that drive that emission.
		Triangle-down is reserved for it because the algorithm is
		downward in the lifecycle (token data → structural fingerprint).
		*/
		structural: {
			category: "structural",
			label: "structural",
			color: [255, 220, 80],
			shape: "triangle_down",
			description: "fold_substrate — cancel / merge sweep",
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
		/*
		consensus is the in-band swarm-learning continuation: vote_swarm
		stays resident via `next self` and XOR-accumulates each peer's
		signals (staged in asset[]) into its own context region, so the
		Value builds a co-encounter histogram of structural fingerprints.
		The Go-side label-propagation hook terminates the loop by
		stamping ROLE=Readout once a labelled neighbour lands. Three
		concentric rings read as "many encounters folding into one
		place" — visually distinct from peer_gap's single-pass hourglass
		and from intervene's perturbation asterisk.
		*/
		consensus: {
			category: "consensus",
			label: "consensus",
			color: [200, 120, 255],
			shape: "concentric",
			description: "vote_swarm — XOR-accumulate peer signals",
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
PROGRAM_CATEGORY_BY_NAME is the single hand-maintained table in this
module. New programs in cmd/cfg/config.yml that lack an entry here
classify as "unknown" — the renderer keeps drawing them as plain data
squares while the operator is asked to assign a category. This is a
deliberate failure mode: the substrate runs the program either way; the
visualiser just doesn't paint a special glyph for it.
*/
const PROGRAM_CATEGORY_BY_NAME: Record<string, ProgramCategory> = {
	link: "plumbing",
	affinity: "plumbing",
	structural_component: "structural",
	beam_swarm_step: "beam",
	active_inference: "inference",
	classify_readout: "classify",
	unsupervised_learn: "peer_gap",
	episodic_replay: "peer_gap",
	vote_swarm: "consensus",
	intervene: "intervene",
	surprisal: "gap_probe",
	causal_explore: "gap_probe",
	causal_hub: "gap_probe",
	falsification: "gap_probe",
	hypothesis: "gap_probe",
	measure_field: "resident",
	popcount: "util",
	coupling: "util",
	temperature: "util",
};

export function categoryForProgram(program: string): ProgramCategory {
	if (!program) {
		return "unknown";
	}

	return PROGRAM_CATEGORY_BY_NAME[program] ?? "unknown";
}

export interface ClassifiedProgram {
	program: string;
	category: ProgramCategory;
	style: ProgramCategoryStyle;
}

function unknownProgram(): ClassifiedProgram {
	return {
		program: "",
		category: "unknown",
		style: PROGRAM_CATEGORIES.unknown,
	};
}

function instructionsEqual(
	a: DecodedInstruction,
	b: DecodedInstruction,
): boolean {
	return (
		a.opcode === b.opcode &&
		a.mode === b.mode &&
		a.topology === b.topology &&
		a.predStart === b.predStart &&
		a.predCond === b.predCond &&
		a.aInd === b.aInd &&
		a.bType === b.bType &&
		a.aStart === b.aStart &&
		a.aSpan === b.aSpan &&
		a.bStart === b.bStart &&
		a.bSpan === b.bSpan &&
		a.dstStart === b.dstStart &&
		a.dstSpan === b.dstSpan
	);
}

/*
classifyInstructionStream matches a Value's full decoded program
against PROGRAM_SIGNATURES. A program counts as installed when every
non-zero leading instruction matches the signature in order. Trailing
zero words (the kernel halts on the first zero) are ignored; this lets
the visualiser see a program before the substrate writes the trailing
scheduler word.
*/
export function classifyInstructionStream(
	instructions: ReadonlyArray<DecodedInstruction>,
): ClassifiedProgram {
	if (!instructions.length) {
		return unknownProgram();
	}

	for (const sig of PROGRAM_SIGNATURES) {
		if (sig.instructions.length !== instructions.length) {
			continue;
		}

		let matched = true;
		for (let index = 0; index < sig.instructions.length; index++) {
			if (!instructionsEqual(sig.instructions[index], instructions[index])) {
				matched = false;
				break;
			}
		}

		if (matched) {
			const category = categoryForProgram(sig.name);
			return {
				program: sig.name,
				category,
				style: PROGRAM_CATEGORIES[category],
			};
		}
	}

	return {
		program: `op=0x${instructions[0].opcode.toString(16).padStart(2, "0")}`,
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
	structural: "action",
	beam: "action",
	inference: "action",
	classify: "reaction",
	peer_gap: "reaction",
	consensus: "action",
	intervene: "action",
	gap_probe: "action",
	resident: "action",
	util: "data",
	unknown: "data",
};
