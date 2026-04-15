import { Graph } from "@/components/node-graph-legacy";
import type {
	FieldSnapshot,
	FieldValueSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";

function allValues(snapshot: VizGraphSnapshot | null): FieldValueSnapshot[] {
	if (!snapshot) {
		return [];
	}

	return [
		...snapshot.fields.flatMap((field: FieldSnapshot) => field.members),
		...snapshot.orphanValues,
	];
}

export function buildValueGraph(snapshot: VizGraphSnapshot | null): Graph {
	const values = allValues(snapshot);
	if (values.length === 0) {
		return new Graph();
	}

	const graph = new Graph();
	const ids = new Set(values.map((value) => value.id));

	for (const value of values) {
		graph.addNode(value.id, {
			kind: "value",
			role: value.role,
			program: value.program,
			community_id: value.communityId,
			short_id: value.id.slice(0, 12),
			label: value.label || value.content.slice(0, 24),
			resolved: value.resolved,
			size_norm: value.resonance,
			brightness_norm: 1 - value.gap,
			weight_mag_norm: value.resolved ? 1 : value.actionResonance,
		});
	}

	for (const value of values) {
		if (value.prevId && ids.has(value.prevId)) {
			graph.addEdge(value.prevId, value.id, { kind: "causal" });
		}
	}

	if (graph.getEdgeCount() === 0) {
		const dataValues = values.filter((value) => value.role === "data");
		const actionValues = values.filter(
			(value) => value.role === "action" || value.role === "reaction",
		);

		for (const dataValue of dataValues) {
			for (const actionValue of actionValues) {
				graph.addEdge(dataValue.id, actionValue.id, { kind: "role" });
			}
		}
	}

	return graph;
}
