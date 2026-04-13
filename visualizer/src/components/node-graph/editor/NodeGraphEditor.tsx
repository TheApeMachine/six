import {
	IconArrowLeft,
	IconArrowRight,
	IconDeviceFloppy,
	IconFolderOpen,
	IconSettings,
} from "@tabler/icons-react";
import { useCallback, useEffect, useRef, useState } from "react";

import {
	ConnectionsLayer,
	ConnectionTargetIndicator,
	MissingInputsPopover,
	type MissingInputsPopoverData,
	NodeReceiverHighlight,
	type OperationMenuData,
	OperationPicker,
} from "./components";
import { NODE_RADIUS } from "./constants";
import {
	getInputPortRotationDegrees,
	getOutputPortRotationDegrees,
	getPortPositionFromRotation,
} from "./geometry";
import { useOperationGroups, useOperationSpecs } from "./hooks";
import { Node } from "./node";
import type { GraphDto, GraphDtoNode } from "./api";
import type {
	ActiveConnection,
	GraphConnection,
	GraphNode,
	NodeId,
} from "./types";
import {
	dataTypeForInputName,
	dataTypeForOutput,
	portColorForDataType,
} from "@/components/operation/types";

const BACKEND_BASE_URL = "http://127.0.0.1:8765";
const DEFAULT_MANIFEST_PATH =
	"/Users/theapemachine/go/src/github.com/theapemachine/caramba2/framework/state/manifest/example/master.yml";

const INPUT_NODE_COLOR = "#4FC3F7";
const OUTPUT_NODE_COLOR = "#FFB74D";
const DEFAULT_OP_COLOR = "#90A4AE";

const GROUP_COLORS: Record<string, string> = {
	attention: "#7E57C2",
	activation: "#26A69A",
	math: "#42A5F5",
	normalization: "#26C6DA",
	projection: "#5C6BC0",
	shape: "#9CCC65",
	positional: "#EC407A",
};

const INITIAL_NODES: GraphNode[] = [];
const INITIAL_CONNECTIONS: GraphConnection[] = [];

function colorForGroup(group: string): string {
	return GROUP_COLORS[group] ?? DEFAULT_OP_COLOR;
}

function colorForNodeType(type: string, kind: GraphDtoNode["kind"]): string {
	if (kind === "input") return INPUT_NODE_COLOR;
	if (kind === "output") return OUTPUT_NODE_COLOR;
	if (type.includes(".")) {
		const [group] = type.split(".", 1);
		return colorForGroup(group);
	}
	return DEFAULT_OP_COLOR;
}

export const NodeGraphEditor = () => {
	const editorRef = useRef<HTMLDivElement | null>(null);
	const autoLoadedRef = useRef(false);

	// Graph state
	const [nodes, setNodes] = useState<GraphNode[]>(INITIAL_NODES);
	const [connections, setConnections] =
		useState<GraphConnection[]>(INITIAL_CONNECTIONS);

	// Manifest state
	const [manifestPath, setManifestPath] = useState(DEFAULT_MANIFEST_PATH);
	const [manifestTargets, setManifestTargets] = useState<string[]>([]);
	const [manifestTarget, setManifestTarget] = useState<string>("");
	const [manifestError, setManifestError] = useState<string | null>(null);
	const [isManifestLoading, setIsManifestLoading] = useState(false);
	const [isManifestSaving, setIsManifestSaving] = useState(false);

	// Interaction state
	const [draggedNode, setDraggedNode] = useState<NodeId | null>(null);
	const [dragOffset, setDragOffset] = useState({ x: 0, y: 0 });
	const [activeConnection, setActiveConnection] =
		useState<ActiveConnection | null>(null);

	// UI state
	const [operationMenu, setOperationMenu] = useState<
		(OperationMenuData & { sourceOutput: string | null }) | null
	>(null);
	const [missingInputsPopover, setMissingInputsPopover] =
		useState<MissingInputsPopoverData | null>(null);

	// Data fetching
	const { groups: operationGroups, error: operationGroupsError } =
		useOperationGroups({ baseUrl: BACKEND_BASE_URL });
	const { specs: operationSpecs } = useOperationSpecs({
		baseUrl: BACKEND_BASE_URL,
		nodes,
	});

	useEffect(() => {
		if (!manifestPath) return;
		autoLoadedRef.current = false;
		let cancelled = false;
		setIsManifestLoading(true);
		setManifestError(null);

		fetch(
			`${BACKEND_BASE_URL}/api/manifest?path=${encodeURIComponent(
				manifestPath,
			)}`,
		)
			.then(async (res) => {
				if (!res.ok) {
					throw new Error(
						`Failed to load manifest: ${res.status} ${res.statusText}`,
					);
				}
				return res.json() as Promise<unknown>;
			})
			.then((x) => {
				if (cancelled) return;
				if (!x || typeof x !== "object") {
					throw new Error("Invalid manifest response");
				}
				const data = x as { targets?: string[] };
				const targets = Array.isArray(data.targets) ? data.targets : null;
				if (!targets || targets.length === 0) {
					throw new Error("Manifest targets missing");
				}
				setManifestTargets(targets);
				setManifestTarget((prev) => {
					if (prev && targets.includes(prev)) return prev;
					return targets[0] ?? "";
				});
			})
			.catch((e: unknown) => {
				if (cancelled) return;
				setManifestError(e instanceof Error ? e.message : String(e));
			})
			.finally(() => {
				if (!cancelled) setIsManifestLoading(false);
			});

		return () => {
			cancelled = true;
		};
	}, [manifestPath]);


	const applyGraphDto = useCallback((graph: GraphDto) => {
		const nextNodes: GraphNode[] = graph.nodes.map((n) => {
			const kind = n.kind ?? "op";
			return {
				id: n.id,
				type: n.op,
				name: n.name,
				x: n.x,
				y: n.y,
				color: colorForNodeType(n.op, kind),
				icon: kind === "input" ? IconArrowRight : kind === "output" ? IconArrowLeft : IconSettings,
				count: 1,
				kind,
				config: n.config ?? {},
			};
		});

		const nextConnections: GraphConnection[] = graph.connections.map((c) => ({
			id: c.id,
			sourceId: c.sourceId,
			targetId: c.targetId,
			sourceOutput: c.sourceOutput ?? null,
			targetInput: c.targetInput ?? null,
		}));

		setNodes(nextNodes);
		setConnections(nextConnections);
	}, []);

	const handleLoadGraph = useCallback(async () => {
		if (!manifestPath) {
			setManifestError("Manifest path is required");
			return;
		}
		if (!manifestTarget) {
			setManifestError("Manifest target is required");
			return;
		}
		setIsManifestLoading(true);
		setManifestError(null);
		try {
			const res = await fetch(
				`${BACKEND_BASE_URL}/api/manifest/graph?path=${encodeURIComponent(
					manifestPath,
				)}&target=${encodeURIComponent(manifestTarget)}`,
			);
			if (!res.ok) {
				throw new Error(
					`Failed to load manifest graph: ${res.status} ${res.statusText}`,
				);
			}
			const payload = (await res.json()) as { graph?: GraphDto };
			if (!payload.graph) {
				throw new Error("Manifest graph missing");
			}
			applyGraphDto(payload.graph);
		} catch (e: unknown) {
			setManifestError(e instanceof Error ? e.message : String(e));
		} finally {
			setIsManifestLoading(false);
		}
	}, [applyGraphDto, manifestPath, manifestTarget]);

	useEffect(() => {
		if (autoLoadedRef.current) return;
		if (!manifestTarget) return;
		autoLoadedRef.current = true;
		handleLoadGraph();
	}, [handleLoadGraph, manifestTarget]);

	const handleSaveGraph = useCallback(async () => {
		if (!manifestPath) {
			setManifestError("Manifest path is required");
			return;
		}
		if (!manifestTarget) {
			setManifestError("Manifest target is required");
			return;
		}
		setIsManifestSaving(true);
		setManifestError(null);
		try {
			const graph: GraphDto = {
				nodes: nodes.map((n) => ({
					id: n.id,
					op: n.type,
					name: n.name,
					kind: n.kind,
					x: n.x,
					y: n.y,
					config: n.config ?? {},
				})),
				connections: connections.map((c) => ({
					id: c.id,
					sourceId: c.sourceId,
					targetId: c.targetId,
					sourceOutput: c.sourceOutput ?? null,
					targetInput: c.targetInput ?? null,
				})),
				inputs: [],
				outputs: [],
			};

			const res = await fetch(`${BACKEND_BASE_URL}/api/manifest/graph`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					path: manifestPath,
					target: manifestTarget,
					graph,
				}),
			});
			if (!res.ok) {
				throw new Error(
					`Failed to save manifest graph: ${res.status} ${res.statusText}`,
				);
			}
		} catch (e: unknown) {
			setManifestError(e instanceof Error ? e.message : String(e));
		} finally {
			setIsManifestSaving(false);
		}
	}, [connections, manifestPath, manifestTarget, nodes]);

	// Helpers for input management
	const inputNamesForNodeId = useCallback(
		(nodeId: NodeId): { required: string[]; optional: string[]; all: string[] } => {
			const node = nodes.find((n) => n.id === nodeId);
			if (!node) return { required: [], optional: [], all: [] };
			if (node.kind === "input") {
				return { required: [], optional: [], all: [] };
			}
			if (node.kind === "output") {
				return { required: ["in"], optional: [], all: ["in"] };
			}
			const spec = operationSpecs[node.type];
			const required = spec?.requiredInputs ?? [];
			const optional = spec?.optionalInputs ?? [];
			return { required, optional, all: [...required, ...optional] };
		},
		[nodes, operationSpecs],
	);

	const firstMissingRequiredInputForTarget = useCallback(
		(targetId: NodeId): string | null => {
			const required = inputNamesForNodeId(targetId).required;
			if (required.length === 0) return null;

			const used = new Set(
				connections
					.filter((c) => c.targetId === targetId)
					.map((c) => c.targetInput)
					.filter((x): x is string => typeof x === "string" && x.length > 0),
			);

			for (const name of required) {
				if (!used.has(name)) return name;
			}
			return null;
		},
		[connections, inputNamesForNodeId],
	);

	const closestAvailableInputForTarget = useCallback(
		(params: {
			targetId: NodeId;
			dropX: number;
			dropY: number;
		}): string | null => {
			const targetNode = nodes.find((n) => n.id === params.targetId);
			if (!targetNode) return null;

			const { required, optional, all } = inputNamesForNodeId(params.targetId);
			if (all.length === 0) return null;

			const used = new Set(
				connections
					.filter((c) => c.targetId === params.targetId)
					.map((c) => c.targetInput)
					.filter((x): x is string => typeof x === "string" && x.length > 0),
			);

			const availableRequired = required.filter((name) => !used.has(name));
			const availableOptional = optional.filter((name) => !used.has(name));
			const available = availableRequired.length > 0 ? availableRequired : availableOptional;
			if (available.length === 0) return null;

			let best: { name: string; d2: number } | null = null;

			for (const name of available) {
				const idx = all.indexOf(name);
				if (idx < 0) continue;
				const rotationDegrees = getInputPortRotationDegrees({
					index: idx,
					total: all.length,
				});
				const p = getPortPositionFromRotation({
					nodeX: targetNode.x,
					nodeY: targetNode.y,
					rotationDegrees,
				});
				const dx = params.dropX - p.x;
				const dy = params.dropY - p.y;
				const d2 = dx * dx + dy * dy;
				if (!best || d2 < best.d2) best = { name, d2 };
			}

			return best?.name ?? null;
		},
		[connections, nodes, inputNamesForNodeId],
	);

	// Backfill missing targetInput on connections when specs are loaded
	useEffect(() => {
		setConnections((prevConnections) => {
			let changed = false;
			const next = prevConnections.map((c) => ({ ...c }));

			for (const node of nodes) {
				const spec = operationSpecs[node.type];
				const inputs = spec
					? [...spec.requiredInputs, ...spec.optionalInputs]
					: [];
				if (inputs.length === 0) continue;

				const incoming = next
					.filter((c) => c.targetId === node.id)
					.sort((a, b) => a.id.localeCompare(b.id));

				const used = new Set(
					incoming
						.map((c) => c.targetInput)
						.filter((x): x is string => typeof x === "string" && x.length > 0),
				);

				let idx = 0;
				for (const c of incoming) {
					if (c.targetInput) continue;
					while (idx < inputs.length && used.has(inputs[idx])) idx++;
					if (idx >= inputs.length) break;
					c.targetInput = inputs[idx];
					used.add(inputs[idx]);
					idx++;
					changed = true;
				}
			}

			return changed ? next : prevConnections;
		});
	}, [nodes, operationSpecs]);

	const findDropTargetNodeId = useCallback(
		(params: {
			sourceId: NodeId;
			tempEndX: number;
			tempEndY: number;
		}): NodeId | null => {
			const threshold = NODE_RADIUS + 20;
			let best: { id: NodeId; distance: number } | null = null;

			for (const node of nodes) {
				if (node.id === params.sourceId) continue;

				const nodeCenterX = node.x + NODE_RADIUS;
				const nodeCenterY = node.y + NODE_RADIUS;
				const dx = params.tempEndX - nodeCenterX;
				const dy = params.tempEndY - nodeCenterY;
				const distance = Math.sqrt(dx * dx + dy * dy);

				if (distance < threshold && (!best || distance < best.distance)) {
					best = { id: node.id, distance };
				}
			}

			return best?.id ?? null;
		},
		[nodes],
	);

	// Event handlers
	const handleNodeDragStart = useCallback(
		(e: React.MouseEvent<HTMLElement>, nodeId: NodeId) => {
			e.preventDefault();
			e.stopPropagation();

			const rect = e.currentTarget.getBoundingClientRect();
			setDragOffset({
				x: e.clientX - rect.left,
				y: e.clientY - rect.top,
			});
			setDraggedNode(nodeId);
		},
		[],
	);

	const handleMouseMove = useCallback(
		(e: React.MouseEvent<HTMLDivElement>) => {
			const editor = editorRef.current;
			if (!editor) return;

			const editorRect = editor.getBoundingClientRect();

			if (draggedNode) {
				setNodes((prevNodes) =>
					prevNodes.map((node) =>
						node.id === draggedNode
							? {
									...node,
									x: e.clientX - editorRect.left - dragOffset.x,
									y: e.clientY - editorRect.top - dragOffset.y,
								}
							: node,
					),
				);
				return;
			}

			setActiveConnection((prev) => {
				if (!prev) return prev;
				return {
					...prev,
					tempEndX: e.clientX - editorRect.left,
					tempEndY: e.clientY - editorRect.top,
				};
			});
		},
		[draggedNode, dragOffset],
	);

	const handleMouseUp = useCallback(() => {
		if (activeConnection) {
			const targetId = findDropTargetNodeId(activeConnection);
			if (targetId) {
				const targetInput =
					closestAvailableInputForTarget({
						targetId,
						dropX: activeConnection.tempEndX,
						dropY: activeConnection.tempEndY,
					}) ?? firstMissingRequiredInputForTarget(targetId);

				const targetNode = nodes.find((n) => n.id === targetId);
				const spec = targetNode ? operationSpecs[targetNode.type] : undefined;
				if (spec) {
					const allInputs = [
						...spec.requiredInputs,
						...spec.optionalInputs,
					];
					const used = new Set(
						connections
							.filter((c) => c.targetId === targetId)
							.map((c) => c.targetInput)
							.filter((x): x is string => typeof x === "string" && x.length > 0),
					);
					const available = allInputs.filter((name) => !used.has(name));
					if (available.length === 0) {
						setDraggedNode(null);
						setActiveConnection(null);
						return;
					}
				}

				if (spec && !targetInput) {
					// All required inputs are connected, don't add ambiguous connection
					setDraggedNode(null);
					setActiveConnection(null);
					return;
				}

				setConnections((prev) => [
					...prev,
					{
						id: `conn-${Date.now()}`,
						sourceId: activeConnection.sourceId,
						targetId,
						sourceOutput: activeConnection.sourceOutput ?? null,
						targetInput,
					},
				]);
			} else {
				setOperationMenu({
					sourceId: activeConnection.sourceId,
					sourceOutput: activeConnection.sourceOutput ?? null,
					x: activeConnection.tempEndX,
					y: activeConnection.tempEndY,
				});
			}
		}
		setDraggedNode(null);
		setActiveConnection(null);
	}, [
		activeConnection,
		closestAvailableInputForTarget,
		findDropTargetNodeId,
		firstMissingRequiredInputForTarget,
		nodes,
		operationSpecs,
		connections,
	]);

	const handleEditorMouseDown = useCallback(
		(e: React.MouseEvent<HTMLDivElement>) => {
			if (e.target === e.currentTarget) {
				setDraggedNode(null);
				setOperationMenu(null);
				setMissingInputsPopover(null);
			}
		},
		[],
	);

	const handleEditorDoubleClick = useCallback(
		(e: React.MouseEvent<HTMLDivElement>) => {
			if (e.target !== e.currentTarget) return;

			const editor = editorRef.current;
			if (!editor) return;

			const editorRect = editor.getBoundingClientRect();
			setOperationMenu({
				sourceId: null,
				sourceOutput: null,
				x: e.clientX - editorRect.left,
				y: e.clientY - editorRect.top,
			});
		},
		[],
	);

	const startConnection = useCallback(
		(e: React.MouseEvent<HTMLElement>, nodeId: NodeId, outputName: string) => {
			e.stopPropagation();

			const editor = editorRef.current;
			if (!editor) return;
			const editorRect = editor.getBoundingClientRect();

			setActiveConnection({
				sourceId: nodeId,
				sourceOutput: outputName,
				tempEndX: e.clientX - editorRect.left,
				tempEndY: e.clientY - editorRect.top,
			});
		},
		[],
	);

	const handleBadgeClick = useCallback(
		(nodeId: NodeId) => {
			const n = nodes.find((x) => x.id === nodeId);
			if (!n) return;
			if (n.kind !== "op") return;
			const s = operationSpecs[n.type];
			if (!s) return;

			const cx = n.x + NODE_RADIUS;
			const cy = n.y;
			const connectedNames = new Set(
				connections
					.filter((c) => c.targetId === nodeId)
					.map((c) => c.targetInput)
					.filter((x): x is string => typeof x === "string" && x.length > 0),
			);
			const missing = s.requiredInputs.filter(
				(name: string) => !connectedNames.has(name),
			);

			setMissingInputsPopover({
				nodeId,
				x: cx,
				y: cy,
				opKey: s.key,
				requiredInputs: s.requiredInputs,
				connectedInputs: connectedNames.size,
				missingInputs: missing,
			});
		},
		[connections, nodes, operationSpecs],
	);

	const handleSelectOperation = useCallback(
		(group: string, op: string) => {
			if (!operationMenu) return;

			const newNodeId = `op-${group}-${op}-${Date.now()}`;
			const color = colorForGroup(group);
			const newNode: GraphNode = {
				id: newNodeId,
				type: `${group}.${op}`,
				name: op,
				x: operationMenu.x - NODE_RADIUS,
				y: operationMenu.y - NODE_RADIUS,
				color,
				icon: IconSettings,
				count: 1,
				kind: "op",
				config: {},
			};

			setNodes((prev) => [...prev, newNode]);

			const sourceId = operationMenu.sourceId;
			if (sourceId) {
				const spec = operationSpecs[newNode.type];
				const required = spec?.requiredInputs ?? [];
				const optional = spec?.optionalInputs ?? [];
				const targetInput = required[0] ?? optional[0] ?? null;

				setConnections((prev) => [
					...prev,
					{
						id: `conn-${Date.now()}`,
						sourceId,
						targetId: newNodeId,
						sourceOutput: operationMenu.sourceOutput ?? null,
						targetInput,
					},
				]);
			}

			setOperationMenu(null);
		},
		[operationMenu, operationSpecs],
	);

	// Render helpers
	const renderNode = (node: GraphNode) => {
		const spec = operationSpecs[node.type];

		const incomingConnections = connections.filter(
			(c) => c.targetId === node.id,
		);
		const connectedInputNames = new Set(
			incomingConnections
				.map((c) => c.targetInput)
				.filter((x): x is string => typeof x === "string" && x.length > 0),
		);

		const requiredInputs = spec?.requiredInputs ?? [];
		const optionalInputs = spec?.optionalInputs ?? [];
		const inputNames =
			node.kind === "output"
				? ["in"]
				: node.kind === "op"
					? [...requiredInputs, ...optionalInputs]
					: [];
		const outputNames =
			node.kind === "input"
				? ["out"]
				: node.kind === "op"
					? spec?.outputs?.length
						? spec.outputs
						: ["out"]
					: [];

		const inputPorts = inputNames.map((name, idx) => ({
			name,
			rotationDegrees: getInputPortRotationDegrees({
				index: idx,
				total: Math.max(1, inputNames.length),
			}),
			isConnected: connectedInputNames.has(name),
			color: portColorForDataType(dataTypeForInputName(name)),
			isRequired: requiredInputs.includes(name) || node.kind === "output",
		}));

		const outputPorts = outputNames.map((name: string, idx: number) => ({
			name,
			rotationDegrees: getOutputPortRotationDegrees({
				index: idx,
				total: Math.max(1, outputNames.length),
			}),
			color: portColorForDataType(
				dataTypeForOutput(name, spec?.outputType ?? "Tensor", idx),
			),
		}));

		const missingCount =
			node.kind === "op"
				? requiredInputs.filter((name: string) => !connectedInputNames.has(name))
						.length
				: 0;

		return (
			<Node
				key={node.id}
				node={node}
				isDragging={draggedNode === node.id}
				portMarkers={[]}
				inputPorts={inputPorts}
				outputPorts={outputPorts}
				badgeCount={missingCount}
				activeConnection={activeConnection}
				onBadgeClick={handleBadgeClick}
				onDragStart={handleNodeDragStart}
				onStartConnection={startConnection}
			/>
		);
	};

	return (
		<div className="bg-canvas flex h-full items-center justify-center">
			<div
				ref={editorRef}
				className="bg-canvas relative h-full w-full overflow-hidden"
				role="application"
				onMouseMove={handleMouseMove}
				onMouseUp={handleMouseUp}
				onMouseDown={handleEditorMouseDown}
				onDoubleClick={handleEditorDoubleClick}
			>
				<div className="bg-background/90 border-border absolute left-4 top-4 z-50 w-[560px] rounded-lg border p-3 shadow-sm backdrop-blur">
					<div className="flex items-center gap-2">
						<input
							className="border-input bg-background text-foreground flex-1 rounded-md border px-2 py-1 text-xs"
							placeholder="Manifest path"
							value={manifestPath}
							onChange={(e) => setManifestPath(e.target.value)}
						/>
						<select
							className="border-input bg-background text-foreground rounded-md border px-2 py-1 text-xs"
							value={manifestTarget}
							onChange={(e) => setManifestTarget(e.target.value)}
						>
							{manifestTargets.map((t) => (
								<option key={t} value={t}>
									{t}
								</option>
							))}
						</select>
						<button
							type="button"
							className="border-input bg-background text-foreground flex items-center gap-1 rounded-md border px-2 py-1 text-xs"
							onClick={handleLoadGraph}
							disabled={isManifestLoading}
						>
							<IconFolderOpen className="size-4" />
							Load
						</button>
						<button
							type="button"
							className="border-input bg-background text-foreground flex items-center gap-1 rounded-md border px-2 py-1 text-xs"
							onClick={handleSaveGraph}
							disabled={isManifestSaving}
						>
							<IconDeviceFloppy className="size-4" />
							Save
						</button>
					</div>
					{manifestError && (
						<div className="text-destructive mt-2 text-xs">
							{manifestError}
						</div>
					)}
				</div>

				<ConnectionsLayer
					nodes={nodes}
					connections={connections}
					activeConnection={activeConnection}
					operationSpecs={operationSpecs}
				/>

				{nodes.map(renderNode)}

				{missingInputsPopover && (
					<MissingInputsPopover
						data={missingInputsPopover}
						onClose={() => setMissingInputsPopover(null)}
					/>
				)}

				{operationMenu && (
					<OperationPicker
						menu={operationMenu}
						groups={operationGroups}
						groupsError={operationGroupsError}
						sourceSpec={
							operationMenu.sourceId
								? operationSpecs[
										nodes.find((n) => n.id === operationMenu.sourceId)?.type ??
											""
									] ?? null
								: null
						}
						operationSpecs={operationSpecs}
						onSelectOperation={handleSelectOperation}
						onClose={() => setOperationMenu(null)}
					/>
				)}

				{activeConnection && (
					<ConnectionTargetIndicator activeConnection={activeConnection} />
				)}

				{activeConnection &&
					nodes.map((node) => (
						<NodeReceiverHighlight
							key={`receiver-${node.id}`}
							node={node}
							activeConnection={activeConnection}
						/>
					))}
			</div>
		</div>
	);
};
