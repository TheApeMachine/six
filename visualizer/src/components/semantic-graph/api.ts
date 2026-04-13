/**
 * API client for semantic zoom graph visualization
 *
 * Fetches weight statistics and neuron data for drill-down views.
 * The main graph data is fetched using the existing /api/graph endpoint.
 */

import type { NeuronGraph, WeightStats } from "./types";

const API_BASE = "http://localhost:7543";

/**
 * Fetch weight statistics for a specific parameter
 */
export async function fetchWeightStats(
	modelId: string,
	paramId: string,
	options?: { bins?: number; heatmapSize?: number },
): Promise<WeightStats> {
	const params = new URLSearchParams({
		model: modelId,
		param: paramId,
	});

	if (options?.bins) {
		params.set("bins", options.bins.toString());
	}
	if (options?.heatmapSize) {
		params.set("heatmapSize", options.heatmapSize.toString());
	}

	const response = await fetch(`${API_BASE}/api/graph/weight?${params}`);
	if (!response.ok) {
		const error = await response.json();
		throw new Error(error.error || "Failed to fetch weight stats");
	}

	return response.json();
}

/**
 * Fetch sampled neuron connections for a layer
 */
export async function fetchNeuronGraph(
	modelId: string,
	layerId: string,
	options?: {
		sampleSize?: number;
		method?: "random" | "top_gradient" | "top_activation";
	},
): Promise<NeuronGraph> {
	const params = new URLSearchParams({
		model: modelId,
		layer: layerId,
	});

	if (options?.sampleSize) {
		params.set("sampleSize", options.sampleSize.toString());
	}
	if (options?.method) {
		params.set("method", options.method);
	}

	const response = await fetch(`${API_BASE}/api/graph/neurons?${params}`);
	if (!response.ok) {
		const error = await response.json();
		throw new Error(error.error || "Failed to fetch neuron graph");
	}

	return response.json();
}

/**
 * List available models (checkpoints)
 */
export async function fetchModels(): Promise<
	Array<{
		id: string;
		label: string;
		path: string;
		checkpoint: string | null;
	}>
> {
	const response = await fetch(`${API_BASE}/api/models`);
	if (!response.ok) {
		throw new Error("Failed to fetch models");
	}

	const data = await response.json();
	return data.models || [];
}

/**
 * List research projects
 */
export async function fetchProjects(): Promise<
	Array<{
		id: string;
		name: string;
		path: string;
	}>
> {
	const response = await fetch(`${API_BASE}/api/projects`);
	if (!response.ok) {
		throw new Error("Failed to fetch projects");
	}

	const data = await response.json();
	return data.projects || [];
}

/**
 * List architectures for a project
 */
export async function fetchArchitectures(
	projectId: string,
): Promise<
	Array<{
		id: string;
		name: string;
		path: string;
	}>
> {
	const response = await fetch(
		`${API_BASE}/api/project/architectures?project=${encodeURIComponent(projectId)}`,
	);
	if (!response.ok) {
		throw new Error("Failed to fetch architectures");
	}

	const data = await response.json();
	return data.architectures || [];
}

/**
 * Fetch graph from manifest architecture
 */
export async function fetchArchitectureGraph(manifestPath: string): Promise<{
	nodes: Record<string, unknown>;
	edges: Record<string, unknown>;
	settings: Record<string, unknown>;
}> {
	const response = await fetch(
		`${API_BASE}/api/architecture/graph?path=${encodeURIComponent(manifestPath)}`,
	);
	if (!response.ok) {
		const error = await response.json();
		throw new Error(error.error || "Failed to fetch architecture graph");
	}

	const data = await response.json();
	return data.graph;
}
