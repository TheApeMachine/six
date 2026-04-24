import type { DecodedValueRegions } from "@/lib/valueRegions";

export type ValueRole = "data" | "action" | "reaction" | "prompt";

/*
FirmwareStep is one entry in the rule-chain history for a Value. The
visualizer keeps the last N steps so users can watch link → affinity →
resident walk through as the ALU processes each rule.
*/
export interface FirmwareStep {
	/** Canonical firmware name ("link", "affinity", "resident", …). */
	name: string;
	/** Substrate that executed the step ("cpu", "cuda_0", "metal_0", …). */
	substrate: string;
	/** true when the step ran the Value's baked program rather than a named firmware. */
	resident: boolean;
	/** Wall-clock microseconds for the step (event timestamp). */
	ts: number;
}

/** Live counters for the value-only wire path. */
export interface VizRuntimeStats {
	values: number;
}

export interface TelemetryPayloadSnapshot {
	lbl: string;
	src: string;
	tgt: string;
	ts: number;
	vals: Record<string, number>;
	meta: Record<string, string>;
}

/*
CausalState collapses the rule-cascade residues a single Value carries
into three orthogonal booleans the visualiser can highlight
independently. All three are derived directly from the wire frame so
no side channel is needed; see pkg/mesh/value-store.ts:readCausalState
for exact word/bit mapping.

 - hypothesizing: the Value has staged a refutation target in
   properties[1] — i.e. the "what if" question has been asked and the
   ALU is waiting for a signal one-run long enough to refute it.
 - falsified: the kernel's ApplyRefutationProbe has stamped
   FalsifiedBitNoiseWord into properties[4], meaning the hypothesis was
   successfully refuted.
 - intervening: the carrier that arrived at this Value severed causal
   history (no prev) and injected a foreign gradient. The do_intervention
   program then XOR'd that gradient into local context.
*/
export interface CausalState {
	hypothesizing: boolean;
	falsified: boolean;
	intervening: boolean;
	surprisal: number;
	delta_surprisal: number;
	stuck_count: number;
	stuck: boolean;
	ttl: number;
	temperature: number;
}

export interface VizInspectSnapshot {
	id: string;
	role: ValueRole;
	program: string;
	firmwareSteps: FirmwareStep[];
	communityId: number;
	label: string;
	content: string;
	pos: { x: number; y: number };
	resonance: number;
	gap: number;
	resolved: boolean;
	actionResonance: number;
	prevId: string;
	nextId: string;
	communityAffinityHex: string;
	wireFrame: Uint8Array | null;
	/** Ergonomic region slices when a full wire frame is present. */
	wireRegions: DecodedValueRegions | null;
	/*
	Wall-clock (ms since epoch) when the store last applied a wire frame for
	this Value. 0 means "never" — the Value exists in the graph but no frame
	has arrived yet. The inspector uses this to surface staleness so the
	operator can tell "nothing is happening to this Value" apart from "the
	bridge is stalled".
	*/
	frameReceivedAtMs: number;
	telemetry: TelemetryPayloadSnapshot | null;
	/** Causal cascade residues; every Value reports, booleans are independent. */
	causal: CausalState;
}

/** Graph members omit layout `pos` (fixed in FieldMap). */
export type FieldValueSnapshot = Omit<VizInspectSnapshot, "pos">;

export interface FieldSnapshot {
	id: number;
	memberCount: number;
	saturated: boolean;
	saturation: number;
	lastAction: string;
	actionCount: number;
	reactionCount: number;
	affinityHex: string;
	concentration: number;
	members: FieldValueSnapshot[];
	/*
	Crystallisation fingerprint sourced from the FieldMetrics envelope
	mesh.Field.Cycle emits every tick. When no envelope has arrived yet
	every field falls back to 0 — the old hardcoded placeholders — so
	the UI degrades gracefully instead of NaN'ing.
	*/
	coverage: number;
	consensus: number;
	labelDensity: number;
	crystallization: number;
	dominantRatio: number;
	modeCount: number;
	pressureMult: number;
	hypothesizingCount: number;
	falsifiedCount: number;
	interveningCount: number;
}

export interface VizGraphSnapshot {
	timestamp: number;
	/** Increments only when graph membership or member payloads change (not selection). */
	graphSeq: number;
	fields: FieldSnapshot[];
	orphanValues: FieldValueSnapshot[];
	totalValues: number;
	totalCommunities: number;
}
