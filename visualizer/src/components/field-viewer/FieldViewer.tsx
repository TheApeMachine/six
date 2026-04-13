/*
FieldViewer is the primary visualization shell for the inference engine.

Tabs:
  Live      — the engine's 2D canvas drawn in real time (pan/zoom/click)
  Fields    — Wonderlens community spiral with per-field drill-down
  Values    — WebGL causal graph of all live values
  Stream    — raw filterable event stream
  Telemetry — full memory-band inspector for a selected value

Data flow: engine.ts → field-context → FieldViewer → (Live canvas | FieldMap | NodeGraphLegacy | EventStream | ValueInspector)
*/

import {
	IconArrowLeft,
	IconArrowRight,
	IconDevice3dLens,
	IconLoader2,
	IconPlayerPlay,
	IconSend,
	IconTag,
	IconTagOff,
	IconWifi,
	IconWifiOff,
	IconX,
} from "@tabler/icons-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { TimeSlider } from "@/components/node-graph/components/TimeSlider";
import { Graph, NodeGraphLegacy } from "@/components/node-graph-legacy";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useField } from "@/context/field-context";
import type {
	FieldSnapshot,
	FieldValueSnapshot,
	VizGraphSnapshot,
	VizInspectSnapshot,
} from "@/lib/engine";
import { cn } from "@/lib/utils";
import { EventStream } from "./EventStream";
import { FieldMap } from "./FieldMap";
import { ProgramViewer } from "./ProgramViewer";
import { PromptInspector } from "./PromptInspector";
import { ValueInspector } from "./ValueInspector";

type ZoomLevel =
	| "live"
	| "fields"
	| "values"
	| "stream"
	| "telemetry"
	| "programs";

interface BreadcrumbEntry {
	level: ZoomLevel;
	id: string;
	label: string;
}

const ZOOM_LABELS: Record<ZoomLevel, string> = {
	live: "Live",
	fields: "Fields",
	values: "Values",
	stream: "Stream",
	telemetry: "Telemetry",
	programs: "Programs",
};

/*
buildValueGraph constructs a Graph from all live values in the snapshot.
Causal chain edges (prevId → id) link values across any field boundary.
Without causal data, role edges connect data tokens to actions.
*/
function buildValueGraph(
	fields: FieldSnapshot[],
	orphans: FieldValueSnapshot[],
): Graph {
	const all: FieldValueSnapshot[] = [
		...fields.flatMap((f) => f.members),
		...orphans,
	];

	if (all.length === 0) return new Graph();

	const idSet = new Set(all.map((v) => v.id));
	const graph = new Graph();

	for (const value of all) {
		graph.addNode(value.id, {
			kind: "value",
			role: value.role,
			program: value.program,
			community_id: value.communityId,
			short_id: value.id.substring(0, 12),
			label: value.label || value.content.substring(0, 24),
			resolved: value.resolved,
			size_norm: value.resonance,
			brightness_norm: 1 - value.gap,
			weight_mag_norm: value.resolved ? 1 : value.actionResonance,
		});
	}

	for (const value of all) {
		if (value.prevId && idSet.has(value.prevId)) {
			graph.addEdge(value.prevId, value.id, { kind: "causal" });
		}
	}

	if (graph.getEdgeCount() === 0) {
		const dataValues = all.filter((v) => v.role === "data");
		const actionValues = all.filter(
			(v) => v.role === "action" || v.role === "reaction",
		);
		for (const data of dataValues) {
			for (const action of actionValues) {
				graph.addEdge(data.id, action.id, { kind: "role" });
			}
		}
	}

	return graph;
}

interface FieldViewerProps {
	className?: string;
}

function nearestSnapshot(
	history: VizGraphSnapshot[],
	targetMs: number,
): VizGraphSnapshot | null {
	if (history.length === 0) return null;
	let best = history[0];
	let bestDiff = Math.abs(best.timestamp - targetMs);
	for (let i = 1; i < history.length; i++) {
		const diff = Math.abs(history[i].timestamp - targetMs);
		if (diff < bestDiff) {
			bestDiff = diff;
			best = history[i];
		}
	}
	return best;
}

function formatRelative(ts: number, nowTs: number): string {
	const diffSec = Math.round((nowTs - ts) / 1000);
	if (diffSec <= 0) return "live";
	if (diffSec < 60) return `−${diffSec}s`;
	const minutes = Math.floor(diffSec / 60);
	const seconds = diffSec % 60;
	return seconds === 0 ? `−${minutes}m` : `−${minutes}m ${seconds}s`;
}

/*
CommunityInspector shows detailed metrics for a clicked community and its
member values. Each member is clickable to select it and jump to Telemetry.
*/
function CommunityInspector({
	field,
	onClose,
	onSelectMember,
}: {
	field: FieldSnapshot;
	onClose: () => void;
	onSelectMember: (v: FieldValueSnapshot) => void;
}) {
	const [cr, cg, cb] = useMemo(() => {
		if (field.affinityHex) {
			const val = parseInt(field.affinityHex.substring(0, 8), 16);
			const hue = ((val % 360) + 360) % 360;
			const c = 0.75 * (1 - Math.abs(2 * 0.62 - 1));
			const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
			const m = 0.62 - c / 2;
			let r = 0,
				g = 0,
				b = 0;
			if (hue < 60) {
				r = c;
				g = x;
			} else if (hue < 120) {
				r = x;
				g = c;
			} else if (hue < 180) {
				g = c;
				b = x;
			} else if (hue < 240) {
				g = x;
				b = c;
			} else if (hue < 300) {
				r = x;
				b = c;
			} else {
				r = c;
				b = x;
			}
			return [
				Math.round((r + m) * 255),
				Math.round((g + m) * 255),
				Math.round((b + m) * 255),
			];
		}
		return [186, 104, 200];
	}, [field.affinityHex]);

	const accentStyle = { color: `rgb(${cr},${cg},${cb})` };
	const bgStyle = {
		background: `rgba(${cr},${cg},${cb},0.06)`,
		borderColor: `rgba(${cr},${cg},${cb},0.2)`,
	};

	return (
		<div
			className="absolute right-0 top-0 bottom-0 z-20 w-72 border-l bg-card/98 backdrop-blur-sm flex flex-col overflow-hidden"
			style={bgStyle}
		>
			{/* Header */}
			<div className="shrink-0 flex items-center justify-between px-3 py-2 border-b border-white/10">
				<div className="flex items-center gap-2">
					<div
						className="w-3 h-3 rounded-full shrink-0"
						style={{ background: `rgb(${cr},${cg},${cb})` }}
					/>
					<span className="text-xs font-semibold font-mono" style={accentStyle}>
						Field #{field.id}
					</span>
					{field.saturated && (
						<span className="text-[9px] font-mono text-red-400 border border-red-500/30 rounded px-1">
							SATURATED
						</span>
					)}
				</div>
				<button
					type="button"
					onClick={onClose}
					className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
				>
					<IconX className="size-3.5" />
				</button>
			</div>

			{/* Metrics */}
			<div className="shrink-0 grid grid-cols-2 gap-0 border-b border-white/10">
				{[
					["members", field.memberCount],
					["actions", field.actionCount],
					["reactions", field.reactionCount],
					[
						"saturation",
						field.saturated ? `${(field.saturation * 100).toFixed(1)}%` : "—",
					],
					["concentration", field.concentration.toFixed(4)],
					["last action", field.lastAction || "—"],
				].map(([k, v]) => (
					<div
						key={String(k)}
						className="px-3 py-2 border-r border-b border-white/5 last:border-r-0"
					>
						<p className="text-[8px] font-mono text-muted-foreground/50 uppercase tracking-wider">
							{k}
						</p>
						<p
							className="text-[11px] font-mono font-semibold truncate"
							style={v === "—" ? {} : accentStyle}
						>
							{String(v)}
						</p>
					</div>
				))}
			</div>

			{/* Affinity hex */}
			{field.affinityHex && (
				<div className="shrink-0 px-3 py-1.5 border-b border-white/10">
					<p className="text-[8px] font-mono text-muted-foreground/40 uppercase tracking-wider mb-0.5">
						affinity vector
					</p>
					<p className="text-[8px] font-mono text-muted-foreground/60 break-all leading-tight">
						{field.affinityHex}
					</p>
				</div>
			)}

			{/* Saturation bar */}
			{field.saturated && (
				<div className="shrink-0 px-3 py-2 border-b border-white/10">
					<div className="h-1.5 w-full rounded-full bg-secondary overflow-hidden">
						<div
							className="h-full rounded-full bg-red-400 transition-all"
							style={{ width: `${Math.max(2, field.saturation * 100)}%` }}
						/>
					</div>
				</div>
			)}

			{/* Member list */}
			<div className="flex-1 overflow-y-auto">
				<p className="px-3 pt-2 pb-1 text-[9px] font-mono text-muted-foreground/40 uppercase tracking-wider sticky top-0 bg-card/95">
					{field.memberCount} values
				</p>
				{field.members.map((member) => {
					const gapPct = (member.gap * 100).toFixed(0);
					return (
						<button
							key={member.id}
							type="button"
							onClick={() => onSelectMember(member)}
							className="w-full text-left px-3 py-1.5 border-b border-white/5 hover:bg-white/5 transition-colors cursor-pointer"
						>
							<div className="flex items-center gap-2 min-w-0">
								<span
									className={cn(
										"shrink-0 text-[8px] font-mono px-1 py-0.5 rounded",
										member.role === "action"
											? "bg-emerald-500/20 text-emerald-300"
											: member.role === "reaction"
												? "bg-orange-500/20 text-orange-300"
												: "bg-sky-500/20 text-sky-300",
									)}
								>
									{member.role[0].toUpperCase()}
								</span>
								{member.program && (
									<span className="text-[8px] font-mono text-purple-300/70 truncate">
										{member.program}
									</span>
								)}
								<span className="text-[8px] font-mono text-white/30 truncate flex-1">
									{(member.label || member.content || member.id).substring(
										0,
										18,
									)}
								</span>
								{member.resolved ? (
									<span className="shrink-0 text-[8px] font-mono text-emerald-400">
										✓
									</span>
								) : (
									<span
										className={cn(
											"shrink-0 text-[8px] font-mono",
											member.gap > 0.7
												? "text-red-400/70"
												: member.gap > 0.3
													? "text-amber-400/70"
													: "text-emerald-400/70",
										)}
									>
										{gapPct}%
									</span>
								)}
							</div>
							<div className="mt-0.5 h-0.5 w-full rounded-full bg-white/5 overflow-hidden">
								<div
									className={cn(
										"h-full rounded-full",
										member.resolved ? "bg-emerald-400" : "bg-sky-400/50",
									)}
									style={{ width: `${Math.max(2, (1 - member.gap) * 100)}%` }}
								/>
							</div>
						</button>
					);
				})}
			</div>
		</div>
	);
}

export function FieldViewer({ className }: FieldViewerProps) {
	const {
		stats,
		snapshot,
		snapshotHistory,
		selection,
		events,
		sendPrompt,
		setEngineContainer,
	} = useField();

	// The live canvas tab div — always in the DOM, visible only on the "live" tab.
	const liveContainerRef = useRef<HTMLDivElement>(null);

	// Register the live container with field-context so the engine mounts there.
	useEffect(() => {
		if (liveContainerRef.current) {
			setEngineContainer(liveContainerRef.current);
		}
		return () => setEngineContainer(null);
	}, [setEngineContainer]);

	const [zoomLevel, setZoomLevel] = useState<ZoomLevel>("live");
	const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbEntry[]>([]);
	const [showLabels, setShowLabels] = useState(false);
	const [lensEnabled, setLensEnabled] = useState(false);
	const [promptText, setPromptText] = useState("");

	// Selected field (from FieldMap click).
	const [selectedField, setSelectedField] = useState<FieldSnapshot | null>(
		null,
	);

	// Program name to pre-select when jumping to the Programs tab.
	const [jumpToProgram, setJumpToProgram] = useState<string | undefined>(
		undefined,
	);

	// localSelection is set when the user clicks a node in NodeGraphLegacy or
	// a member in the community inspector.
	const [localSelection, setLocalSelection] =
		useState<VizInspectSnapshot | null>(null);

	// Prefer the engine's richer selection (from hidden canvas click handler via
	// field-context), fall back to locally constructed ones.
	const activeSelection = selection ?? localSelection;

	// ── Timeline scrubbing ─────────────────────────────────────────────────────
	const LIVE_THRESHOLD_MS = 3000;
	const [scrubTs, setScrubTs] = useState<number | null>(null);

	const latestTs =
		snapshotHistory.length > 0
			? snapshotHistory[snapshotHistory.length - 1].timestamp
			: null;

	const isLive =
		scrubTs === null ||
		(latestTs !== null && latestTs - scrubTs <= LIVE_THRESHOLD_MS);

	const activeSnapshot: VizGraphSnapshot | null = useMemo(() => {
		if (isLive || scrubTs === null) return snapshot;
		return nearestSnapshot(snapshotHistory, scrubTs);
	}, [isLive, scrubTs, snapshot, snapshotHistory]);

	const handleTimeChange = useCallback(
		(from: number, to: number) => {
			void from;
			const targetMs = to * 1000;
			const latest =
				snapshotHistory.length > 0
					? snapshotHistory[snapshotHistory.length - 1].timestamp
					: null;
			if (latest !== null && latest - targetMs <= LIVE_THRESHOLD_MS) {
				setScrubTs(null);
			} else {
				setScrubTs(targetMs);
			}
		},
		[snapshotHistory],
	);

	const connected = stats !== null;
	const fieldList = useMemo(
		() => activeSnapshot?.fields ?? [],
		[activeSnapshot],
	);
	const orphanList = useMemo(
		() => activeSnapshot?.orphanValues ?? [],
		[activeSnapshot],
	);

	const valueGraph = useMemo(
		() => buildValueGraph(fieldList, orphanList),
		[fieldList, orphanList],
	);

	const valueIntensity = useMemo(() => {
		if (valueGraph.getNodeCount() === 0) return undefined;
		const arr: number[] = new Array(valueGraph.getNodeCount()).fill(0.3);
		for (const field of fieldList) {
			for (const value of field.members) {
				const node = valueGraph.nodes[value.id];
				if (node)
					arr[node.id] = value.resolved ? 1 : Math.max(0.05, 1 - value.gap);
			}
		}
		return arr;
	}, [fieldList, valueGraph]);

	const navigateTo = useCallback(
		(index: number) => {
			const crumb = breadcrumbs[index];
			if (!crumb) return;
			setBreadcrumbs(breadcrumbs.slice(0, index + 1));
			setZoomLevel(crumb.level);
		},
		[breadcrumbs],
	);

	const goUp = useCallback(() => {
		if (breadcrumbs.length > 1) navigateTo(breadcrumbs.length - 2);
	}, [breadcrumbs.length, navigateTo]);

	const handleValueSelect = useCallback(
		(_nodeIndex: number, nodeName: string) => {
			const allValues: FieldValueSnapshot[] = [
				...fieldList.flatMap((f) => f.members),
				...orphanList,
			];
			const found = allValues.find((v) => v.id === nodeName);

			if (found) {
				setLocalSelection({
					id: found.id,
					role: found.role,
					program: found.program,
					communityId: found.communityId,
					label: found.label,
					content: found.content,
					pos: { x: 0, y: 0 },
					resonance: found.resonance,
					gap: found.gap,
					resolved: found.resolved,
					actionResonance: found.actionResonance,
					prevId: found.prevId,
					nextId: found.nextId,
					telemetry: null,
				});
			}

			setZoomLevel("telemetry");
			setBreadcrumbs((prev) => {
				const last = prev[prev.length - 1];
				if (last?.level === "telemetry") return prev;
				return [
					...prev,
					{ level: "telemetry", id: nodeName, label: "Telemetry" },
				];
			});
		},
		[fieldList, orphanList],
	);

	// Selecting a value by ID — used by the event stream and chain navigation.
	const handleSelectById = useCallback(
		(id: string) => {
			const allValues: FieldValueSnapshot[] = [
				...fieldList.flatMap((f) => f.members),
				...orphanList,
			];
			const found = allValues.find((v) => v.id === id);

			if (found) {
				setLocalSelection({
					id: found.id,
					role: found.role,
					program: found.program,
					communityId: found.communityId,
					label: found.label,
					content: found.content,
					pos: { x: 0, y: 0 },
					resonance: found.resonance,
					gap: found.gap,
					resolved: found.resolved,
					actionResonance: found.actionResonance,
					prevId: found.prevId,
					nextId: found.nextId,
					telemetry: null,
				});
				setZoomLevel("telemetry");
			}
		},
		[fieldList, orphanList],
	);

	const handleFieldSelect = useCallback((field: FieldSnapshot | null) => {
		setSelectedField(field);
	}, []);

	const handleMemberSelect = useCallback((member: FieldValueSnapshot) => {
		setLocalSelection({
			id: member.id,
			role: member.role,
			program: member.program,
			communityId: member.communityId,
			label: member.label,
			content: member.content,
			pos: { x: 0, y: 0 },
			resonance: member.resonance,
			gap: member.gap,
			resolved: member.resolved,
			actionResonance: member.actionResonance,
			prevId: member.prevId,
			nextId: member.nextId,
			telemetry: null,
		});
		setZoomLevel("telemetry");
		setSelectedField(null);
	}, []);

	const handlePrompt = useCallback(
		(e: React.FormEvent) => {
			e.preventDefault();
			if (!promptText.trim()) return;
			sendPrompt(promptText.trim());
			setPromptText("");
		},
		[promptText, sendPrompt],
	);

	const TAB_ORDER: ZoomLevel[] = [
		"live",
		"fields",
		"values",
		"stream",
		"telemetry",
		"programs",
	];

	return (
		<div
			className={cn(
				"flex h-screen w-screen flex-col overflow-hidden bg-background",
				className,
			)}
		>
			{/* Header */}
			<header className="z-10 border-b bg-card/90 px-5 py-3 backdrop-blur-sm">
				<div className="flex items-center justify-between gap-4">
					<div className="flex items-center gap-3">
						<div
							className={cn(
								"flex items-center gap-1.5 text-xs",
								connected
									? isLive
										? "text-emerald-400"
										: "text-amber-400"
									: "text-muted-foreground",
							)}
						>
							{connected ? (
								isLive ? (
									<IconWifi className="size-3.5" />
								) : (
									<IconPlayerPlay className="size-3.5" />
								)
							) : (
								<IconWifiOff className="size-3.5" />
							)}
							<span className="font-mono">
								{!connected
									? "waiting"
									: isLive
										? "live"
										: latestTs && scrubTs
											? formatRelative(scrubTs, latestTs)
											: "paused"}
							</span>
						</div>
						<div className="h-3.5 w-px bg-border" />
						<h1 className="text-sm font-semibold">Field Inspector</h1>
						{zoomLevel === "values" && valueGraph.getNodeCount() > 0 && (
							<>
								<div className="h-3.5 w-px bg-border" />
								<span className="text-xs font-mono text-muted-foreground">
									{valueGraph.getNodeCount()} nodes ·{" "}
									{valueGraph.getEdgeCount()} edges
								</span>
							</>
						)}
						{zoomLevel === "stream" && (
							<>
								<div className="h-3.5 w-px bg-border" />
								<span className="text-xs font-mono text-muted-foreground">
									{events.length} events
								</span>
							</>
						)}
					</div>

					<div className="flex items-center gap-4 text-xs font-mono text-muted-foreground">
						<span>{stats?.values ?? 0} values</span>
						<span className="text-purple-400">
							{stats?.communities ?? 0} fields
						</span>
						<span className="text-emerald-400">
							{stats?.actions ?? 0} actions
						</span>
						<span className="text-orange-400">
							{stats?.reactions ?? 0} reactions
						</span>
						{stats && stats.dropped > 0 && (
							<span className="text-red-400">dropped {stats.dropped}</span>
						)}
					</div>

					<div className="flex items-center gap-4">
						{zoomLevel === "fields" && (
							<div className="flex items-center gap-2">
								<IconDevice3dLens
									className={cn(
										"size-3.5",
										lensEnabled ? "text-sky-400" : "text-muted-foreground",
									)}
								/>
								<Label
									htmlFor="lens-switch"
									className="text-xs text-muted-foreground"
								>
									Lens
								</Label>
								<Switch
									id="lens-switch"
									checked={lensEnabled}
									onCheckedChange={setLensEnabled}
									size="sm"
								/>
							</div>
						)}
						<div className="flex items-center gap-2">
							{showLabels ? (
								<IconTag className="size-3.5 text-muted-foreground" />
							) : (
								<IconTagOff className="size-3.5 text-muted-foreground" />
							)}
							<Label
								htmlFor="labels-switch"
								className="text-xs text-muted-foreground"
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
					</div>
				</div>
			</header>

			{/* Breadcrumbs */}
			{breadcrumbs.length > 0 && (
				<nav className="z-10 border-b bg-card/50 px-5 py-2.5 backdrop-blur-sm">
					<div className="flex items-center justify-between">
						<Breadcrumb>
							<BreadcrumbList>
								{breadcrumbs.map((crumb, i) => (
									<BreadcrumbItem key={crumb.id}>
										{i > 0 && <BreadcrumbSeparator />}
										{i === breadcrumbs.length - 1 ? (
											<BreadcrumbPage>
												<span className="text-[10px] uppercase tracking-wide text-muted-foreground">
													{ZOOM_LABELS[crumb.level]}
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
													{ZOOM_LABELS[crumb.level]}
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
								Up
							</Button>
						)}
					</div>
				</nav>
			)}

			{/* Zoom level tabs */}
			<div className="z-10 flex items-center gap-6 border-b bg-card/30 px-5 py-2.5 backdrop-blur-sm">
				{TAB_ORDER.map((level) => (
					<button
						key={level}
						type="button"
						onClick={() => setZoomLevel(level)}
						className={cn(
							"flex items-center gap-2 text-xs transition-all cursor-pointer hover:opacity-100",
							zoomLevel === level ? "opacity-100" : "opacity-40",
						)}
					>
						<span
							className={cn(
								"size-2 rounded-full transition-all",
								zoomLevel === level
									? level === "live"
										? "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.7)] animate-pulse"
										: "bg-sky-400 shadow-[0_0_8px_rgba(56,189,248,0.5)]"
									: "bg-muted-foreground/30",
							)}
						/>
						<span className="font-medium">{ZOOM_LABELS[level]}</span>
					</button>
				))}

				{/* Chain navigation arrows on the Telemetry tab */}
				{zoomLevel === "telemetry" && activeSelection && (
					<div className="ml-auto flex items-center gap-1">
						<button
							type="button"
							disabled={!activeSelection.prevId}
							onClick={() =>
								activeSelection.prevId &&
								handleSelectById(activeSelection.prevId)
							}
							className="flex items-center gap-1 text-[10px] font-mono text-indigo-400 hover:text-indigo-200 disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
						>
							<IconArrowLeft className="size-3" />
							prev
						</button>
						<span className="text-muted-foreground/30 text-xs">|</span>
						<button
							type="button"
							disabled={!activeSelection.nextId}
							onClick={() =>
								activeSelection.nextId &&
								handleSelectById(activeSelection.nextId)
							}
							className="flex items-center gap-1 text-[10px] font-mono text-indigo-400 hover:text-indigo-200 disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
						>
							next
							<IconArrowRight className="size-3" />
						</button>
					</div>
				)}
			</div>

			{/* Main area */}
			<div className="relative flex flex-1 overflow-hidden">
				{/*
				Live canvas — always mounted so the engine has a stable container with
				real dimensions. Visibility toggles via CSS rather than conditional
				rendering to keep ResizeObserver consistent.
				*/}
				<div
					ref={liveContainerRef}
					className="absolute inset-0"
					style={{
						visibility: zoomLevel === "live" ? "visible" : "hidden",
						pointerEvents: zoomLevel === "live" ? "auto" : "none",
					}}
				/>

				{/* Fields — Wonderlens community spiral */}
				{zoomLevel === "fields" && (
					<div
						className="absolute inset-0"
						style={{ right: selectedField ? "18rem" : 0 }}
					>
						{fieldList.length === 0 ? (
							<EmptyState
								connected={connected}
								message="Waiting for field activity…"
								hint="Fields emerge as values cluster by affinity. Hover to inspect a field through the lens."
							/>
						) : (
							<FieldMap
								fields={fieldList}
								lensEnabled={lensEnabled}
								onFieldSelect={handleFieldSelect}
							/>
						)}
					</div>
				)}

				{/* Community inspector panel */}
				{zoomLevel === "fields" && selectedField && (
					<CommunityInspector
						field={selectedField}
						onClose={() => setSelectedField(null)}
						onSelectMember={handleMemberSelect}
					/>
				)}

				{/* Values — causal graph */}
				{zoomLevel === "values" && (
					<div className="absolute inset-0">
						{valueGraph.getNodeCount() === 0 ? (
							<EmptyState
								connected={connected}
								message="No values yet."
								hint="Values appear as the engine processes tokens."
							/>
						) : (
							<NodeGraphLegacy
								graph={valueGraph}
								nodeIntensity={valueIntensity}
								showLabels={showLabels}
								showEdges={true}
								showTimeSlider={false}
								metricsContrast={1.5}
								labelDetailMode="detailed"
								onNodeSelect={handleValueSelect}
							/>
						)}
					</div>
				)}

				{/* Stream — raw event feed */}
				{zoomLevel === "stream" && (
					<div className="absolute inset-0">
						<EventStream
							events={events}
							onSelectValue={handleSelectById}
							className="h-full"
						/>
					</div>
				)}

				{/* Telemetry — full memory-band inspector */}
				{zoomLevel === "telemetry" && (
					<div className="absolute inset-0 p-4 overflow-y-auto">
						{activeSelection ? (
							<ValueInspector
								snap={activeSelection}
								onSelectId={handleSelectById}
							/>
						) : (
							<EmptyState
								connected={connected}
								message="No value selected."
								hint="Click a node in the Values view, an event in the Stream, or a member in the Fields inspector."
							/>
						)}
					</div>
				)}

				{/* Programs — firmware circuit diagram viewer */}
				{zoomLevel === "programs" && (
					<div className="absolute inset-0">
						<ProgramViewer initialProgram={jumpToProgram} />
					</div>
				)}

				{/* Prompt inspector — auto-appears on all non-telemetry tabs */}
				{zoomLevel !== "telemetry" && zoomLevel !== "programs" && (
					<PromptInspector events={events} />
				)}

				{/* Floating inspector panel — shows outside telemetry view, only when
			    no community inspector is open and not on the live/stream/programs tabs. */}
				{activeSelection &&
					zoomLevel !== "telemetry" &&
					zoomLevel !== "stream" &&
					zoomLevel !== "live" &&
					zoomLevel !== "programs" &&
					!(zoomLevel === "fields" && selectedField) && (
						<aside className="absolute right-0 top-0 bottom-0 z-20 w-64 border-l bg-card/95 p-3 backdrop-blur-sm flex flex-col gap-2">
							<div className="flex items-center justify-between shrink-0">
								<p className="text-[10px] uppercase tracking-wider text-muted-foreground">
									Selected
								</p>
								<button
									type="button"
									onClick={() => setZoomLevel("telemetry")}
									className="text-[10px] text-sky-400 hover:text-sky-300 transition-colors cursor-pointer"
								>
									full view →
								</button>
							</div>
							<SidebarPreview
								snap={activeSelection}
								onSelectById={handleSelectById}
								onSelectProgram={(name) => {
									setJumpToProgram(name);
									setZoomLevel("programs");
								}}
							/>
						</aside>
					)}
			</div>

			{/* Timeline scrubber */}
			{snapshotHistory.length > 1 &&
				(() => {
					const histMin = snapshotHistory[0].timestamp / 1000;
					const histMax =
						snapshotHistory[snapshotHistory.length - 1].timestamp / 1000;
					const scrubSec = scrubTs !== null ? scrubTs / 1000 : histMax;
					return (
						<div className="z-10 border-t bg-card/80 px-5 py-2 backdrop-blur-sm">
							<div className="flex items-center gap-3">
								<div className="flex-1">
									<TimeSlider
										min={histMin}
										max={histMax}
										from={histMin}
										to={scrubSec}
										onChange={handleTimeChange}
										showControls={false}
										formatTime={(ts) => {
											if (latestTs === null) return "";
											return formatRelative(ts * 1000, latestTs);
										}}
									/>
								</div>
								<span className="shrink-0 text-[10px] font-mono text-muted-foreground">
									{snapshotHistory.length} frames
								</span>
							</div>
						</div>
					);
				})()}

			{/* Prompt bar */}
			<footer className="z-10 border-t bg-card/90 px-5 py-3 backdrop-blur-sm">
				<form onSubmit={handlePrompt} className="flex gap-2">
					<Input
						value={promptText}
						onChange={(e) => setPromptText(e.target.value)}
						placeholder="Inject prompt into field… (POST /api/prompt)"
						className="font-mono text-sm"
					/>
					<Button
						type="submit"
						variant="outline"
						size="sm"
						disabled={!promptText.trim() || !connected}
					>
						<IconSend className="size-3.5" />
						Inject
					</Button>
				</form>
			</footer>
		</div>
	);
}

function SidebarPreview({
	snap,
	onSelectById,
	onSelectProgram,
}: {
	snap: VizInspectSnapshot;
	onSelectById: (id: string) => void;
	onSelectProgram?: (name: string) => void;
}) {
	const gapPct = (snap.gap * 100).toFixed(1);
	const gapColor = snap.resolved
		? "text-emerald-400"
		: snap.gap < 0.3
			? "text-emerald-300"
			: snap.gap < 0.7
				? "text-amber-300"
				: "text-red-300";

	return (
		<div className="space-y-2 text-[10px] font-mono overflow-hidden">
			<div className="flex flex-wrap gap-1">
				<span className="px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
					{snap.role}
				</span>
				{snap.program &&
					(onSelectProgram ? (
						<button
							type="button"
							onClick={() => onSelectProgram(snap.program)}
							className="px-1.5 py-0.5 rounded bg-purple-500/20 text-purple-300 hover:bg-purple-500/30 hover:text-purple-200 transition-colors cursor-pointer border-0"
							title="View program circuit"
						>
							{snap.program} ↗
						</button>
					) : (
						<span className="px-1.5 py-0.5 rounded bg-purple-500/20 text-purple-300">
							{snap.program}
						</span>
					))}
				{snap.communityId >= 0 && (
					<span className="px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground">
						field #{snap.communityId}
					</span>
				)}
			</div>
			<p className="text-[9px] text-muted-foreground/50 break-all">{snap.id}</p>
			{snap.content && (
				<p className="text-sky-200/70 break-all leading-relaxed">
					&ldquo;{snap.content.substring(0, 80)}
					{snap.content.length > 80 ? "…" : ""}&rdquo;
				</p>
			)}
			<div className="space-y-1">
				<div className="h-1.5 w-full rounded-full bg-secondary overflow-hidden">
					<div
						className={cn(
							"h-full rounded-full transition-all duration-700",
							snap.resolved ? "bg-emerald-400" : "bg-sky-400",
						)}
						style={{ width: `${Math.max(2, (1 - snap.gap) * 100)}%` }}
					/>
				</div>
				<p className={cn("text-[9px]", gapColor)}>
					{snap.resolved ? "resolved" : `${gapPct}% gap`}
				</p>
			</div>
			{/* Chain links */}
			{(snap.prevId || snap.nextId) && (
				<div className="flex items-center gap-2 pt-1">
					{snap.prevId && (
						<button
							type="button"
							onClick={() => onSelectById(snap.prevId)}
							className="text-[9px] text-indigo-400 hover:text-indigo-200 underline underline-offset-2 cursor-pointer"
						>
							← prev
						</button>
					)}
					{snap.nextId && (
						<button
							type="button"
							onClick={() => onSelectById(snap.nextId)}
							className="text-[9px] text-indigo-400 hover:text-indigo-200 underline underline-offset-2 cursor-pointer"
						>
							next →
						</button>
					)}
				</div>
			)}
		</div>
	);
}

function EmptyState({
	connected,
	message,
	hint,
}: {
	connected: boolean;
	message: string;
	hint?: string;
}) {
	return (
		<div className="flex h-full items-center justify-center">
			<Card className="max-w-sm text-center">
				<CardHeader>
					<div className="mx-auto mb-3 flex size-12 items-center justify-center rounded-full bg-muted">
						{connected ? (
							<IconLoader2 className="size-6 text-muted-foreground animate-spin" />
						) : (
							<IconWifiOff className="size-6 text-muted-foreground" />
						)}
					</div>
					<CardTitle className="text-sm">{message}</CardTitle>
				</CardHeader>
				{hint && (
					<CardContent>
						<p className="text-xs text-muted-foreground">{hint}</p>
					</CardContent>
				)}
			</Card>
		</div>
	);
}
