export type ValueRole = "data" | "action" | "reaction" | "prompt";

export interface VizRuntimeStats {
	values: number;
	communities: number;
	actions: number;
	reactions: number;
	dropped: number;
	bootstrapNodes: number;
	wireJsonBlobs: number;
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
	telemetry: TelemetryPayloadSnapshot | null;
}

export interface FieldValueSnapshot {
	id: string;
	role: ValueRole;
	program: string;
	communityId: number;
	label: string;
	content: string;
	resonance: number;
	gap: number;
	resolved: boolean;
	actionResonance: number;
	prevId: string;
	nextId: string;
	communityAffinityHex: string;
	wireFrame: Uint8Array | null;
	telemetry: TelemetryPayloadSnapshot | null;
}

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
