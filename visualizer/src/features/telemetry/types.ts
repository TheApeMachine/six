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
	telemetry: TelemetryPayloadSnapshot | null;
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
}

export interface VizGraphSnapshot {
	timestamp: number;
	fields: FieldSnapshot[];
	orphanValues: FieldValueSnapshot[];
	totalValues: number;
	totalCommunities: number;
}
