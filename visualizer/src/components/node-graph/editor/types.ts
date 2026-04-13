import type { Icon } from "@tabler/icons-react";

export type NodeId = string;

export interface GraphNode {
	id: NodeId;
	type: string;
	name: string;
	x: number;
	y: number;
	color: string;
	icon: Icon;
	count: number;
	kind: "input" | "output" | "op";
	config: Record<string, unknown>;
}

export interface GraphConnection {
	id: string;
	sourceId: NodeId;
	targetId: NodeId;
	sourceOutput: string | null;
	/**
	 * Name of the target input this connection satisfies (e.g. "x", "a", "b").
	 *
	 * This enables per-input validation (missing inputs) and clearer UX when an
	 * operation requires multiple inputs.
	 */
	targetInput: string | null;
}

export interface ActiveConnection {
	sourceId: NodeId;
	sourceOutput: string | null;
	tempEndX: number;
	tempEndY: number;
}

export interface Dot {
	x: number;
	y: number;
	fade: number;
	/**
	 * Stable position parameter along the connection, in [0, 1].
	 *
	 * We keep this so UI layers can build unique React keys without relying on
	 * rounded coordinates (which can collide) or array indices.
	 */
	t: number;
}

export interface PortPosition {
	x: number;
	y: number;
	angleDegrees: number;
}

