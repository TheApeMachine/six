export interface OperationSpec {
	key: string;
	className: string;
	requiredInputs: string[];
	optionalInputs: string[];
	outputs: string[];
	outputType: string;
}

export type OperationGroups = Record<string, string[]>;

export interface OperationGroupsResponse {
	groups: OperationGroups;
}

export interface OperationSpecResponse {
	operation: OperationSpec;
}

export interface GraphDtoNode {
	id: string;
	op: string;
	name: string;
	kind: "input" | "output" | "op";
	x: number;
	y: number;
	config: Record<string, unknown>;
}

export interface GraphDtoConnection {
	id: string;
	sourceId: string;
	targetId: string;
	sourceOutput: string | null;
	targetInput: string | null;
}

export interface GraphDto {
	nodes: GraphDtoNode[];
	connections: GraphDtoConnection[];
	inputs: string[];
	outputs: string[];
}

export interface ManifestTargetsResponse {
	path: string;
	targets: string[];
}

export interface ManifestGraphResponse {
	path: string;
	target: string;
	graph: GraphDto;
}

export function isOperationGroupsResponse(x: unknown): x is OperationGroupsResponse {
	if (!x || typeof x !== "object") return false;
	const obj = x as Record<string, unknown>;
	if (!obj.groups || typeof obj.groups !== "object") return false;
	return true;
}

export function isOperationSpecResponse(x: unknown): x is OperationSpecResponse {
	if (!x || typeof x !== "object") return false;
	const obj = x as Record<string, unknown>;
	const operation = obj.operation;
	if (!operation || typeof operation !== "object") return false;
	const op = operation as Record<string, unknown>;
	return (
		typeof op.key === "string" &&
		typeof op.className === "string" &&
		Array.isArray(op.requiredInputs) &&
		Array.isArray(op.optionalInputs) &&
		Array.isArray(op.outputs) &&
		typeof op.outputType === "string"
	);
}

export function isManifestTargetsResponse(
	x: unknown,
): x is ManifestTargetsResponse {
	if (!x || typeof x !== "object") return false;
	const obj = x as Record<string, unknown>;
	return (
		typeof obj.path === "string" &&
		Array.isArray(obj.targets) &&
		obj.targets.every((t) => typeof t === "string")
	);
}

export function isManifestGraphResponse(x: unknown): x is ManifestGraphResponse {
	if (!x || typeof x !== "object") return false;
	const obj = x as Record<string, unknown>;
	if (typeof obj.path !== "string" || typeof obj.target !== "string") return false;
	if (!obj.graph || typeof obj.graph !== "object") return false;
	const graph = obj.graph as Record<string, unknown>;
	if (!Array.isArray(graph.nodes) || !Array.isArray(graph.connections)) return false;
	return true;
}
