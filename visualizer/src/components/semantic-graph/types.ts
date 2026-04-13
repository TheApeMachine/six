/**
 * Type definitions for semantic zoom graph visualization
 *
 * Extends the existing NodeGraph system with drill-down capabilities.
 */

export type ZoomLevel = "module" | "component" | "weight" | "neuron";

export interface WeightStats {
	paramId: string;
	shape: number[];
	dtype: string;
	paramCount: number;
	mean: number;
	std: number;
	min: number;
	max: number;
	sparsity: number;
	histogramBins: number[];
	histogramCounts: number[];
	heatmapData: number[];
	heatmapWidth: number;
	heatmapHeight: number;
	gradientNorm: number;
	gradientMean: number;
	gradientStd: number;
}

export interface Neuron {
	index: number;
	activation: number;
	gradient: number;
}

export interface NeuronConnection {
	inputIndex: number;
	outputIndex: number;
	weight: number;
}

export interface NeuronGraph {
	layerId: string;
	inputNeurons: Neuron[];
	outputNeurons: Neuron[];
	connections: NeuronConnection[];
	samplingMethod: string;
	totalInputNeurons: number;
	totalOutputNeurons: number;
}

/**
 * Navigation breadcrumb for drill-down
 */
export interface BreadcrumbItem {
	level: ZoomLevel;
	id: string;
	label: string;
}

/**
 * Format parameter count for display
 */
export function formatParamCount(count: number): string {
	if (count >= 1e9) return `${(count / 1e9).toFixed(2)}B`;
	if (count >= 1e6) return `${(count / 1e6).toFixed(2)}M`;
	if (count >= 1e3) return `${(count / 1e3).toFixed(1)}K`;
	return count.toString();
}

/**
 * Format tensor shape for display
 */
export function formatShape(shape: number[]): string {
	if (!shape || shape.length === 0) return "";
	return `[${shape.join(", ")}]`;
}

/**
 * Get zoom level display label
 */
export function getZoomLevelLabel(level: ZoomLevel): string {
	switch (level) {
		case "module":
			return "Modules";
		case "component":
			return "Components";
		case "weight":
			return "Weights";
		case "neuron":
			return "Neurons";
	}
}
