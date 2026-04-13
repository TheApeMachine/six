/**
 * Semantic Graph Viewer
 *
 * Main component for semantic zoom visualization of neural networks.
 * Uses the existing NodeGraph GPU-accelerated visualization system
 * with added drill-down and weight inspection capabilities.
 */

import {
	IconArrowUp,
	IconLoader2,
	IconTag,
	IconTagOff,
} from "@tabler/icons-react";
import { useCallback, useEffect, useMemo, useState } from "react";
// Use the existing GPU-accelerated graph system
import {
	Graph,
	type GraphData,
	NodeGraphLegacy,
} from "@/components/node-graph-legacy";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";

import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { useProject } from "@/context/project-context";
import { fetchArchitectures, fetchArchitectureGraph } from "./api";
import type {
	BreadcrumbItem as BreadcrumbItemType,
	WeightStats,
	ZoomLevel,
} from "./types";
import { getZoomLevelLabel } from "./types";
import { WeightStatsView } from "./WeightStatsView";

interface SemanticGraphViewerProps {
	className?: string;
}

interface ArchitectureEntry {
	id: string;
	name: string;
	path: string;
}

function isGraphData(x: unknown): x is GraphData {
	if (!x || typeof x !== "object") return false;
	const r = x as Record<string, unknown>;
	return !!r.nodes && !!r.edges && !!r.settings;
}

export function SemanticGraphViewer({ className }: SemanticGraphViewerProps) {
	// Project context
	const { selectedProject } = useProject();

	// Architecture selection
	const [architectures, setArchitectures] = useState<ArchitectureEntry[]>([]);
	const [architecturesLoading, setArchitecturesLoading] = useState(false);
	const [selectedArchitecture, setSelectedArchitecture] = useState<ArchitectureEntry | null>(null);

	// Graph state (uses existing Graph class)
	const [fullGraph, setFullGraph] = useState<Graph | null>(null); // Complete detailed graph
	const [moduleGraph, setModuleGraph] = useState<Graph | null>(null); // Collapsed module view
	const [componentGraph, setComponentGraph] = useState<Graph | null>(null); // Filtered component view
	const [graphLoading, setGraphLoading] = useState(false);
	const [graphError, setGraphError] = useState<string | null>(null);

	// Drill-down state
	const [currentLevel, setCurrentLevel] = useState<ZoomLevel>("module");
	const [_selectedModule, setSelectedModule] = useState<string | null>(null);
	const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbItemType[]>([]);

	// Weight stats (for drill-down view - will be used when viewing trained models)
	const [weightStats, setWeightStats] = useState<WeightStats | null>(null);
	const [_weightLoading, _setWeightLoading] = useState(false);

	// Display settings
	const [showLabels, setShowLabels] = useState(false);
	const [hoveredNode, setHoveredNode] = useState<string | null>(null);

	// Fetch architectures when project changes
	useEffect(() => {
		if (!selectedProject) {
			setArchitectures([]);
			setSelectedArchitecture(null);
			return;
		}

		setArchitecturesLoading(true);
		fetchArchitectures(selectedProject.id)
			.then((archs) => {
				setArchitectures(archs);
				setArchitecturesLoading(false);
				// Auto-select first architecture
				if (archs.length > 0) {
					setSelectedArchitecture(archs[0]);
				} else {
					setSelectedArchitecture(null);
				}
			})
			.catch((err) => {
				console.error("Failed to fetch architectures:", err);
				setArchitecturesLoading(false);
			});
	}, [selectedProject]);

	// Create a collapsed module-level graph from the full graph
	const createModuleGraph = useCallback((fullGraphData: GraphData): Graph => {
		// Group nodes by module (block_0, block_1, etc. + embedding + output)
		const modules: Record<string, { nodes: string[]; edges: Set<string> }> = {
			embedding: { nodes: [], edges: new Set() },
			output: { nodes: [], edges: new Set() },
		};

		// Initialize block modules
		for (let i = 0; i < 6; i++) {
			modules[`block_${i}`] = { nodes: [], edges: new Set() };
		}

		// Classify nodes into modules
		for (const nodeName of Object.keys(fullGraphData.nodes)) {
			if (nodeName === "token_embed") {
				modules.embedding.nodes.push(nodeName);
			} else if (nodeName === "final_norm" || nodeName === "output_proj") {
				modules.output.nodes.push(nodeName);
			} else {
				// Extract block number from name like "block_0_attn_norm"
				const match = nodeName.match(/block_(\d+)_/);
				if (match) {
					const blockNum = match[1];
					modules[`block_${blockNum}`]?.nodes.push(nodeName);
				}
			}
		}

		// Create collapsed graph data
		const collapsedNodes: GraphData["nodes"] = {};
		const collapsedEdges: GraphData["edges"] = {};
		let nodeId = 0;

		// Create module nodes
		const moduleOrder = ["embedding", "block_0", "block_1", "block_2", "block_3", "block_4", "block_5", "output"];
		const moduleIds: Record<string, number> = {};

		for (const moduleName of moduleOrder) {
			const mod = modules[moduleName];
			if (!mod || mod.nodes.length === 0) continue;

			moduleIds[moduleName] = nodeId;
			const displayName = moduleName === "embedding" ? "Embedding" 
				: moduleName === "output" ? "Output"
				: `Layer ${moduleName.replace("block_", "")}`;

			collapsedNodes[moduleName] = {
				id: nodeId++,
				edges: [],
				data: [{
					kind: "module",
					type: moduleName,
					display_name: displayName,
					node_count: mod.nodes.length,
					size_norm: 0.7,
					brightness_norm: 0.6,
					weight_mag_norm: 0.5,
				}],
			};
		}

		// Create edges between sequential modules
		let edgeId = 0;
		for (let i = 0; i < moduleOrder.length - 1; i++) {
			const source = moduleOrder[i];
			const target = moduleOrder[i + 1];
			if (moduleIds[source] !== undefined && moduleIds[target] !== undefined) {
				const edgeKey = `${source}<>${target}`;
				collapsedEdges[edgeKey] = {
					source,
					target,
					id: edgeId++,
					data: [{ kind: "sequential" }],
				};

				// Update node edges
				collapsedNodes[source].edges.push(moduleIds[target]);
				collapsedNodes[target].edges.push(moduleIds[source]);
			}
		}

		const moduleGraphObj = new Graph();
		moduleGraphObj.loadFromData({
			nodes: collapsedNodes,
			edges: collapsedEdges,
			settings: fullGraphData.settings,
		});

		return moduleGraphObj;
	}, []);

	// Load graph when architecture changes
	const loadGraph = useCallback(async (arch: ArchitectureEntry) => {
		setGraphLoading(true);
		setGraphError(null);
		setCurrentLevel("module");
		setSelectedModule(null);
		setBreadcrumbs([{ level: "module", id: arch.id, label: arch.name }]);
		setWeightStats(null);

		try {
			const graphData = await fetchArchitectureGraph(arch.path);

			if (!isGraphData(graphData)) {
				throw new Error("Invalid graph data from backend");
			}

			// Store the full detailed graph
			const fullG = new Graph();
			fullG.loadFromData(graphData);
			setFullGraph(fullG);

			// Create collapsed module-level graph
			const modG = createModuleGraph(graphData);
			setModuleGraph(modG);

			// Clear component graph (will be set on drill-down)
			setComponentGraph(null);
		} catch (e) {
			const msg = e instanceof Error ? e.message : String(e);
			setGraphError(msg);
			setFullGraph(null);
			setModuleGraph(null);
		} finally {
			setGraphLoading(false);
		}
	}, [createModuleGraph]);

	// Load graph when architecture selection changes
	useEffect(() => {
		if (selectedArchitecture) {
			loadGraph(selectedArchitecture);
		} else {
			setFullGraph(null);
			setModuleGraph(null);
			setComponentGraph(null);
		}
	}, [selectedArchitecture, loadGraph]);

	// Create component graph for a specific module (drill-down)
	const createComponentGraph = useCallback((moduleName: string): Graph | null => {
		if (!fullGraph) return null;

		// Filter nodes that belong to this module
		const filteredNodes: GraphData["nodes"] = {};
		const filteredEdges: GraphData["edges"] = {};
		const nodeIdMap: Record<string, number> = {};
		let nodeId = 0;

		// Determine which nodes belong to this module
		const nodeFilter = (name: string): boolean => {
			if (moduleName === "embedding") {
				return name === "token_embed";
			} else if (moduleName === "output") {
				return name === "final_norm" || name === "output_proj";
			} else {
				// block_N pattern
				return name.startsWith(`${moduleName}_`);
			}
		};

		// Collect matching nodes
		for (const [nodeName, node] of Object.entries(fullGraph.nodes)) {
			if (nodeFilter(nodeName)) {
				nodeIdMap[nodeName] = nodeId;
				filteredNodes[nodeName] = {
					...node,
					id: nodeId++,
					edges: [], // Will rebuild
				};
			}
		}

		// Collect edges between filtered nodes
		let edgeId = 0;
		for (const [edgeKey, edge] of Object.entries(fullGraph.edges)) {
			const source = edge.source as string;
			const target = edge.target as string;
			if (nodeIdMap[source] !== undefined && nodeIdMap[target] !== undefined) {
				filteredEdges[edgeKey] = {
					...edge,
					id: edgeId++,
				};

				// Update node edges
				filteredNodes[source].edges.push(nodeIdMap[target]);
				filteredNodes[target].edges.push(nodeIdMap[source]);
			}
		}

		if (Object.keys(filteredNodes).length === 0) {
			return null;
		}

		const compGraph = new Graph();
		compGraph.loadFromData({
			nodes: filteredNodes,
			edges: filteredEdges,
			settings: { epoch: "Event Time", epochFormat: "YYYY-M-D H:m:s", source: "source", target: "target" },
		});

		return compGraph;
	}, [fullGraph]);

	// Handle architecture selection
	const handleArchitectureSelect = useCallback((archId: string | null) => {
		if (archId) {
			const arch = architectures.find((a) => a.id === archId);
			if (arch) {
				setSelectedArchitecture(arch);
			}
		}
	}, [architectures]);

	// Handle node hover
	const handleNodeHover = useCallback(
		(_nodeIndex: number, nodeName: string) => {
			setHoveredNode(nodeName);
		},
		[],
	);

	// Handle module selection (drill down from module to component view)
	const handleModuleSelect = useCallback(
		(_nodeIndex: number, moduleName: string) => {
			const compGraph = createComponentGraph(moduleName);
			if (compGraph) {
				setComponentGraph(compGraph);
				setSelectedModule(moduleName);
				setCurrentLevel("component");

				const displayName = moduleName === "embedding" ? "Embedding"
					: moduleName === "output" ? "Output"
					: `Layer ${moduleName.replace("block_", "")}`;

				setBreadcrumbs((prev) => [
					...prev,
					{ level: "component", id: moduleName, label: displayName },
				]);
			}
		},
		[createComponentGraph],
	);

	// Handle component selection (drill down from component to weight view)
	const handleComponentSelect = useCallback(
		(_nodeIndex: number, nodeName: string) => {
			// For now, just update breadcrumbs and switch to weight view
			// In a real system, this would load weight stats from a checkpoint
			setCurrentLevel("weight");
			setBreadcrumbs((prev) => [
				...prev,
				{ level: "weight", id: nodeName, label: nodeName.split("_").pop() || nodeName },
			]);

			// TODO: If we have a checkpoint, load weight stats here
			// const stats = await fetchWeightStats(checkpointId, nodeName);
			// setWeightStats(stats);
		},
		[],
	);

	// Navigate breadcrumbs
	const navigateTo = useCallback(
		(index: number) => {
			const targetCrumb = breadcrumbs[index];
			if (!targetCrumb) return;

			setBreadcrumbs(breadcrumbs.slice(0, index + 1));
			setCurrentLevel(targetCrumb.level);

			if (targetCrumb.level === "module") {
				setWeightStats(null);
				setSelectedModule(null);
				setComponentGraph(null);
			} else if (targetCrumb.level === "component") {
				setWeightStats(null);
			}
		},
		[breadcrumbs],
	);

	// Go up one level
	const goUp = useCallback(() => {
		if (breadcrumbs.length > 1) {
			navigateTo(breadcrumbs.length - 2);
		}
	}, [breadcrumbs.length, navigateTo]);

	// Get current graph stats based on level
	const currentGraph = currentLevel === "module" ? moduleGraph 
		: currentLevel === "component" ? componentGraph 
		: null;
	const nodeCount = useMemo(() => currentGraph?.getNodeCount() ?? 0, [currentGraph]);
	const edgeCount = useMemo(() => currentGraph?.getEdgeCount() ?? 0, [currentGraph]);

	const zoomLevels: ZoomLevel[] = ["module", "component", "weight", "neuron"];

	return (
		<div
			className={cn(
				"flex flex-col overflow-hidden bg-background",
				className,
			)}
		>
			{/* Header */}
			<header className="border-b bg-card px-6 py-4">
				<div className="flex items-center justify-between">
					<div>
						<h1 className="text-xl font-semibold">Neural Network Inspector</h1>
						<p className="text-sm text-muted-foreground">
							GPU-accelerated semantic zoom visualization
						</p>
					</div>
					<div className="flex items-center gap-4">
						{/* Labels toggle */}
						<div className="flex items-center gap-2">
							{showLabels ? (
								<IconTag className="size-4 text-muted-foreground" />
							) : (
								<IconTagOff className="size-4 text-muted-foreground" />
							)}
							<Label
								htmlFor="labels-switch"
								className="text-sm text-muted-foreground"
							>
								Labels
							</Label>
							<Switch
								id="labels-switch"
								checked={showLabels}
								onCheckedChange={setShowLabels}
								size="sm"
							/>
						</div>

						<div className="h-4 w-px bg-border" />

						<div className="text-sm text-muted-foreground">
							{nodeCount} nodes · {edgeCount} edges
						</div>
						<div className="w-64">
							<Select
								value={selectedArchitecture?.id || undefined}
								onValueChange={handleArchitectureSelect}
								disabled={architecturesLoading || !selectedProject}
							>
								<SelectTrigger>
									<SelectValue
										placeholder={
											!selectedProject
												? "Select a project first"
												: architecturesLoading
													? "Loading..."
													: "Select architecture..."
										}
									/>
								</SelectTrigger>
								<SelectContent>
									{architectures.map((arch) => (
										<SelectItem key={arch.id} value={arch.id}>
											{arch.name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					</div>
				</div>
			</header>

			{/* Breadcrumbs */}
			{breadcrumbs.length > 0 && (
				<nav className="border-b bg-card/50 px-6 py-3">
					<div className="flex items-center justify-between">
						<Breadcrumb>
							<BreadcrumbList>
								{breadcrumbs.map((crumb, i) => (
									<BreadcrumbItem key={crumb.id}>
										{i > 0 && <BreadcrumbSeparator />}
										{i === breadcrumbs.length - 1 ? (
											<BreadcrumbPage>
												<span className="text-[10px] uppercase tracking-wide text-muted-foreground">
													{getZoomLevelLabel(crumb.level)}
												</span>
												<span className="ml-1 font-medium">{crumb.label}</span>
											</BreadcrumbPage>
										) : (
											<BreadcrumbLink
												render={
													<button
														type="button"
														className="cursor-pointer"
														onClick={() => navigateTo(i)}
													/>
												}
											>
												<span className="text-[10px] uppercase tracking-wide text-muted-foreground">
													{getZoomLevelLabel(crumb.level)}
												</span>
												<span className="ml-1">{crumb.label}</span>
											</BreadcrumbLink>
										)}
									</BreadcrumbItem>
								))}
							</BreadcrumbList>
						</Breadcrumb>
						{breadcrumbs.length > 1 && (
							<Button variant="outline" size="sm" onClick={goUp}>
								<IconArrowUp className="mr-1.5 size-4" />
								Go Up
							</Button>
						)}
					</div>
				</nav>
			)}

			{/* Zoom level tabs - clickable */}
			<div className="flex items-center gap-8 border-b px-6 py-3">
				{zoomLevels.map((level) => (
					<button
						key={level}
						type="button"
						onClick={() => setCurrentLevel(level)}
						className={cn(
							"flex items-center gap-2 text-sm transition-all cursor-pointer hover:opacity-100",
							currentLevel === level ? "opacity-100" : "opacity-50",
						)}
					>
						<span
							className={cn(
								"size-2.5 rounded-full transition-all",
								currentLevel === level
									? "bg-emerald-500 shadow-[0_0_8px_rgba(34,197,94,0.5)]"
									: "bg-muted-foreground/30",
							)}
						/>
						<span className="font-medium">{getZoomLevelLabel(level)}</span>
					</button>
				))}
			</div>

			{/* Main content */}
			<main className="flex flex-col">
				{/* Loading state */}
				{graphLoading && (
					<div className="absolute inset-0 z-10 flex items-center justify-center bg-background/80">
						<div className="flex flex-col items-center gap-4">
							<IconLoader2 className="size-8 animate-spin text-muted-foreground" />
							<p className="text-muted-foreground">Loading...</p>
						</div>
					</div>
				)}

				{/* Error state */}
				{graphError && (
					<div className="p-6">
						<Card className="border-destructive/50 bg-destructive/5">
							<CardContent className="py-4">
								<p className="text-destructive">
									<strong>Error:</strong> {graphError}
								</p>
							</CardContent>
						</Card>
					</div>
				)}

				{/* Modules view - high-level collapsed architecture */}
				{currentLevel === "module" && moduleGraph && !graphError && (
					<div className="flex flex-col">
						<NodeGraphLegacy
							graph={moduleGraph}
							metricsContrast={1.5}
							showLabels={showLabels}
							showEdges={true}
							showTimeSlider={false}
							onNodeSelect={handleModuleSelect}
							onNodeHover={handleNodeHover}
							labelDetailMode="detailed"
						/>

						{/* Hover tooltip */}
						{!showLabels && hoveredNode && (
							<div className="pointer-events-none absolute left-4 bottom-4 z-20 max-w-md rounded-lg border bg-popover/95 px-3 py-2 text-sm text-popover-foreground shadow-lg backdrop-blur-sm">
								<div className="font-mono text-xs text-muted-foreground">
									Click to drill down
								</div>
								<div className="font-medium">{hoveredNode}</div>
							</div>
						)}
					</div>
				)}

				{/* Components view - detailed operations for selected module */}
				{currentLevel === "component" && componentGraph && !graphError && (
					<div className="flex flex-col">
						<NodeGraphLegacy
							graph={componentGraph}
							metricsContrast={1.0}
							showLabels={showLabels}
							showEdges={true}
							showTimeSlider={false}
							onNodeSelect={handleComponentSelect}
							onNodeHover={handleNodeHover}
							labelDetailMode="detailed"
						/>

						{/* Hover tooltip */}
						{!showLabels && hoveredNode && (
							<div className="pointer-events-none absolute left-4 bottom-4 z-20 max-w-md rounded-lg border bg-popover/95 px-3 py-2 text-sm text-popover-foreground shadow-lg backdrop-blur-sm">
								<div className="font-mono text-xs text-muted-foreground">
									Click to inspect weights
								</div>
								<div className="font-medium">{hoveredNode}</div>
							</div>
						)}
					</div>
				)}

				{/* Weights view - weight distribution and heatmap */}
				{currentLevel === "weight" && !graphError && (
					<div className="flex flex-col p-6">
						{weightStats ? (
							<WeightStatsView stats={weightStats} />
						) : (
							<div className="flex flex-col items-center justify-center py-12">
								<div className="text-center">
									<h2 className="mb-2 text-lg font-semibold">Weight Inspector</h2>
									<p className="max-w-md text-sm text-muted-foreground">
										Select a weight tensor from the Components view to see its
										distribution, heatmap, and statistics.
									</p>
									<p className="mt-4 text-xs text-muted-foreground">
										This view requires a trained model checkpoint.
									</p>
								</div>
							</div>
						)}
					</div>
				)}

				{/* Neurons view - sampled bipartite neuron graph */}
				{currentLevel === "neuron" && !graphError && (
					<div className="flex flex-col items-center justify-center p-12">
						<div className="text-center">
							<div className="mx-auto mb-4 flex size-16 items-center justify-center rounded-full bg-muted">
								<IconLoader2 className="size-8 text-muted-foreground" />
							</div>
							<h2 className="mb-2 text-lg font-semibold">Sampled Neuron View</h2>
							<p className="max-w-md text-sm text-muted-foreground">
								Shows a sampled subset of neurons (e.g., 64 input → 64 output)
								with edge colors representing weight values. This is an
								illustrative view, not the full layer.
							</p>
							<p className="mt-4 text-xs text-muted-foreground">
								Requires a trained model checkpoint with weight data.
							</p>
						</div>
					</div>
				)}

				{/* Empty state */}
				{!moduleGraph && !graphLoading && !graphError && (
					<div className="flex h-full items-center justify-center">
						<Card>
							<CardHeader>
								<CardTitle>
									{!selectedProject
										? "No Project Selected"
										: "No Architecture Selected"}
								</CardTitle>
							</CardHeader>
							<CardContent>
								<p className="text-muted-foreground">
									{!selectedProject
										? "Select a research project from the sidebar to get started."
										: "Select an architecture from the dropdown to explore its dataflow."}
								</p>
							</CardContent>
						</Card>
					</div>
				)}
			</main>
		</div>
	);
}
