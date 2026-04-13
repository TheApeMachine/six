/*
EventStream renders a live, filterable stream of all raw viz events received
from the backend. Every event kind is shown with its full vals and meta on
expansion. Clicking a row that references a value_id fires onSelectValue so
the caller can jump to the Telemetry view.
*/

import { useCallback, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { VizEvent } from "@/lib/wire";
import { KIND_NAMES } from "@/lib/wire";

interface EventStreamProps {
	events: VizEvent[];
	onSelectValue?: (id: string) => void;
	className?: string;
}

const KIND_COLOR: Record<string, string> = {
	Prompt: "text-amber-300",
	PromptResult: "text-emerald-300",
	TokenizerEmit: "text-sky-300",
	TokenizerChunk: "text-sky-400/70",
	DatasetRead: "text-cyan-400/70",
	QueueSubmit: "text-indigo-300",
	CommunityCreated: "text-purple-300",
	ValueJoinedCommunity: "text-purple-400",
	CommunitySaturated: "text-red-300",
	CommunityAction: "text-emerald-300",
	CommunityReaction: "text-orange-300",
	CommunityEmission: "text-yellow-300",
	CausalHubProbe: "text-amber-400/80",
	BeliefGapEvaluated: "text-violet-300",
	ValueResolved: "text-emerald-400",
	ALUDispatch: "text-orange-400",
	CompilerCompile: "text-fuchsia-300",
	FinalizerRun: "text-fuchsia-400/70",
	BeamCollect: "text-cyan-300",
	BeamCompose: "text-cyan-200",
	BeamBreak: "text-red-400",
	BeamConverge: "text-green-300",
	TrieInsert: "text-blue-300/70",
	TrieDecay: "text-blue-400/60",
	TriePrune: "text-blue-400/50",
	TriePredict: "text-blue-200/70",
	TrieClassify: "text-blue-300",
	TrieSignal: "text-blue-200",
	TrieCoupling: "text-indigo-300/70",
	TrieMode: "text-indigo-200/70",
	TriePressure: "text-indigo-400/70",
	FieldDigest: "text-sky-200",
	FieldPressure: "text-sky-300/70",
	EigenmodeDetected: "text-cyan-200",
	GossipSent: "text-lime-300/70",
	GossipReceived: "text-lime-200/70",
	AdaptiveUpdate: "text-teal-300",
	PoolSchedule: "text-green-300/70",
	PoolComplete: "text-green-200/70",
	TrieGraphSnapshot: "text-amber-200/60",
	NodeCreated: "text-slate-300",
	NodeUpdated: "text-slate-400/70",
	NodeRemoved: "text-red-400/60",
};

function kindColor(kind: number): string {
	const name = KIND_NAMES[kind] || "";
	return KIND_COLOR[name] || "text-white/30";
}

function formatTs(ts: number): string {
	const d = new Date(ts / 1000);
	const hh = d.getHours().toString().padStart(2, "0");
	const mm = d.getMinutes().toString().padStart(2, "0");
	const ss = d.getSeconds().toString().padStart(2, "0");
	const ms = Math.floor((ts / 1000) % 1000)
		.toString()
		.padStart(3, "0");
	return `${hh}:${mm}:${ss}.${ms}`;
}

// All distinct kind names for the filter dropdown.
const ALL_KINDS = Object.values(KIND_NAMES).sort();

interface EventRowProps {
	ev: VizEvent;
	onSelectValue?: (id: string) => void;
}

function EventRow({ ev, onSelectValue }: EventRowProps) {
	const [expanded, setExpanded] = useState(false);
	const kindName = KIND_NAMES[ev.kind] || `kind_${ev.kind}`;
	const valueId =
		ev.meta?.value_id || ev.meta?.action_id || ev.meta?.reaction_id || "";
	const hasPayload =
		Object.keys(ev.vals).length > 0 || Object.keys(ev.meta).length > 0;

	const handleClick = useCallback(() => {
		if (valueId && onSelectValue) {
			onSelectValue(valueId);
			return;
		}
		if (hasPayload) setExpanded((x) => !x);
	}, [valueId, onSelectValue, hasPayload]);

	return (
		<button
			type="button"
			className={cn(
				"w-full text-left border-b border-white/5 px-2 py-0.5 cursor-pointer hover:bg-white/5 transition-colors",
				expanded && "bg-white/5",
			)}
			onClick={handleClick}
		>
			<div className="flex items-baseline gap-2 min-w-0">
				<span className="shrink-0 text-[8px] font-mono text-white/25 tabular-nums">
					{formatTs(ev.ts)}
				</span>
				<span
					className={cn(
						"shrink-0 text-[9px] font-mono font-semibold w-28 truncate",
						kindColor(ev.kind),
					)}
				>
					{kindName}
				</span>
				<span className="text-[9px] font-mono text-white/40 truncate min-w-0">
					{ev.src && (
						<span className="text-white/30 mr-1">
							{ev.src.substring(0, 14)}
						</span>
					)}
					{ev.tgt && (
						<span className="text-white/20 mr-1">
							→{ev.tgt.substring(0, 14)}
						</span>
					)}
					{ev.lbl && (
						<span className="text-white/50">{ev.lbl.substring(0, 40)}</span>
					)}
					{valueId && (
						<span className="ml-1 text-sky-400/70 underline underline-offset-2">
							{valueId.substring(0, 12)}
						</span>
					)}
				</span>
			</div>

			{expanded && hasPayload && (
				<div className="mt-1 ml-32 pb-1 flex flex-wrap gap-x-4 gap-y-0.5">
					{Object.entries(ev.vals).map(([k, v]) => (
						<span key={k} className="text-[8px] font-mono text-white/40">
							<span className="text-white/25">{k}=</span>
							<span className="text-white/60">
								{Number.isInteger(v) ? v : v.toFixed(6).replace(/\.?0+$/, "")}
							</span>
						</span>
					))}
					{Object.entries(ev.meta).map(([k, v]) => (
						<span key={k} className="text-[8px] font-mono text-purple-300/40">
							<span className="text-purple-300/25">{k}=</span>
							<span className="text-purple-200/50">
								{String(v).length > 60
									? String(v).substring(0, 58) + "…"
									: String(v)}
							</span>
						</span>
					))}
				</div>
			)}
		</button>
	);
}

export function EventStream({
	events,
	onSelectValue,
	className,
}: EventStreamProps) {
	const [filterKind, setFilterKind] = useState<string>("");
	const [filterText, setFilterText] = useState<string>("");
	const [paused, setPaused] = useState(false);
	const pausedEventsRef = useRef<VizEvent[]>([]);

	const displayEvents = useMemo(() => {
		const source = paused ? pausedEventsRef.current : events;

		return source
			.filter((ev) => {
				if (filterKind) {
					const name = KIND_NAMES[ev.kind] || "";
					if (name !== filterKind) return false;
				}
				if (filterText) {
					const needle = filterText.toLowerCase();
					const haystack = [
						ev.src,
						ev.tgt,
						ev.lbl,
						...Object.keys(ev.vals),
						...Object.values(ev.meta),
					]
						.join(" ")
						.toLowerCase();
					if (!haystack.includes(needle)) return false;
				}
				return true;
			})
			.slice(0, 300);
	}, [events, filterKind, filterText, paused]);

	const handlePause = useCallback(() => {
		if (!paused) pausedEventsRef.current = events.slice();
		setPaused((x) => !x);
	}, [paused, events]);

	const presentKinds = useMemo(() => {
		const seen = new Set<string>();
		for (const ev of events) {
			const n = KIND_NAMES[ev.kind];
			if (n) seen.add(n);
		}
		return ALL_KINDS.filter((k) => seen.has(k));
	}, [events]);

	return (
		<div className={cn("flex flex-col h-full", className)}>
			{/* Toolbar */}
			<div className="shrink-0 flex items-center gap-2 border-b border-white/10 px-3 py-2">
				<select
					value={filterKind}
					onChange={(e) => setFilterKind(e.target.value)}
					className="h-6 rounded bg-muted/60 px-1.5 text-[10px] font-mono text-muted-foreground border border-white/10 focus:outline-none"
				>
					<option value="">all kinds</option>
					{presentKinds.map((k) => (
						<option key={k} value={k}>
							{k}
						</option>
					))}
				</select>
				<input
					type="text"
					value={filterText}
					onChange={(e) => setFilterText(e.target.value)}
					placeholder="filter src/lbl/meta…"
					className="flex-1 h-6 rounded bg-muted/60 px-2 text-[10px] font-mono text-muted-foreground border border-white/10 focus:outline-none placeholder:text-muted-foreground/40"
				/>
				<button
					type="button"
					onClick={handlePause}
					className={cn(
						"shrink-0 h-6 px-2 rounded text-[10px] font-mono border transition-colors",
						paused
							? "border-amber-500/50 text-amber-400 bg-amber-500/10"
							: "border-white/10 text-muted-foreground hover:text-foreground",
					)}
				>
					{paused ? "resume" : "pause"}
				</button>
				<span className="shrink-0 text-[9px] font-mono text-muted-foreground/40">
					{displayEvents.length}/{events.length}
				</span>
			</div>

			{/* Stream */}
			<div className="flex-1 overflow-y-auto">
				{displayEvents.length === 0 ? (
					<div className="flex h-full items-center justify-center">
						<p className="text-[10px] font-mono text-muted-foreground/40">
							{events.length === 0 ? "waiting for events…" : "no matches"}
						</p>
					</div>
				) : (
					displayEvents.map((ev, i) => (
						<EventRow
							key={`${ev.ts}-${ev.kind}-${i}`}
							ev={ev}
							onSelectValue={onSelectValue}
						/>
					))
				)}
			</div>
		</div>
	);
}
