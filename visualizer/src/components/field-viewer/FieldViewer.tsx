import {
	IconGraph,
	IconPlayerPause,
	IconPlayerPlay,
	IconSend,
	IconWifi,
	IconWifiOff,
} from "@tabler/icons-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useField } from "@/context/field-context";
import { FieldLiveCanvas } from "@/features/live/FieldLiveCanvas";
import type {
	FieldSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";
import { cn } from "@/lib/utils";
import { EK } from "@/lib/wire";
import { ValueInspector } from "./ValueInspector";

function nearestSnapshot(history: VizGraphSnapshot[], targetMs: number) {
	if (history.length === 0) {
		return null;
	}

	let best = history[0];
	let bestDiff = Math.abs(best.timestamp - targetMs);

	for (let index = 1; index < history.length; index++) {
		const diff = Math.abs(history[index].timestamp - targetMs);
		if (diff < bestDiff) {
			best = history[index];
			bestDiff = diff;
		}
	}

	return best;
}

function formatTimestamp(timestamp: number) {
	return new Date(timestamp).toLocaleTimeString([], {
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	});
}

function programBreakdown(snapshot: VizGraphSnapshot | null) {
	const counts = new Map<string, number>();

	for (const field of snapshot?.fields ?? []) {
		const key = field.lastAction || "aggregate";
		counts.set(key, (counts.get(key) ?? 0) + 1);
	}

	return Array.from(counts.entries())
		.sort((left, right) => right[1] - left[1])
		.slice(0, 6);
}

export function FieldViewer({ className }: { className?: string }) {
	const {
		connectionError,
		events,
		selection,
		sendPrompt,
		selectValueById,
		snapshot,
		snapshotHistory,
		stats,
	} = useField();
	const [promptText, setPromptText] = useState("");
	const [scrubEnabled, setScrubEnabled] = useState(false);
	const [scrubMs, setScrubMs] = useState<number | null>(null);
	const [selectedField, setSelectedField] = useState<FieldSnapshot | null>(
		null,
	);

	const activeSnapshot = useMemo(() => {
		if (!scrubEnabled || scrubMs === null) {
			return snapshot;
		}

		return nearestSnapshot(snapshotHistory, scrubMs) ?? snapshot;
	}, [scrubEnabled, scrubMs, snapshot, snapshotHistory]);
	const historyMin = snapshotHistory[0]?.timestamp ?? 0;
	const historyMax =
		snapshotHistory[snapshotHistory.length - 1]?.timestamp ?? 0;
	const topPrograms = useMemo(
		() => programBreakdown(activeSnapshot),
		[activeSnapshot],
	);
	const hotFields = useMemo(() => {
		return [...(activeSnapshot?.fields ?? [])]
			.sort((left, right) => {
				return (
					right.memberCount - left.memberCount ||
					right.concentration - left.concentration
				);
			})
			.slice(0, 10);
	}, [activeSnapshot]);
	const selectedEvent = selection?.telemetry;

	function submitPrompt() {
		const trimmed = promptText.trim();
		if (!trimmed) {
			return;
		}

		sendPrompt(trimmed);
		setPromptText("");
	}

	return (
		<div
			className={cn(
				"relative h-screen overflow-hidden bg-[#05050f] text-white",
				className,
			)}
		>
			<FieldLiveCanvas
				snapshot={activeSnapshot}
				selectedId={selection?.id ?? null}
				onSelectField={setSelectedField}
				onSelectValue={(id) => selectValueById(id)}
				className="h-full w-full bg-[#05050f]"
			/>

			<div className="pointer-events-none absolute inset-x-0 top-0 flex justify-between px-4 py-3">
				<div className="pointer-events-auto max-w-[320px] font-mono text-[10px] leading-4 text-white/72">
					<div className="mb-3 flex items-center gap-2 text-white/85">
						{connectionError ? (
							<IconWifiOff className="h-4 w-4 text-red-300" />
						) : (
							<IconWifi className="h-4 w-4 text-emerald-300" />
						)}
						<span>six visualizer</span>
						<Badge className="border-white/10 bg-white/5 px-1.5 py-0 font-mono text-[9px] text-white/75">
							live
						</Badge>
					</div>
					<div>values: {stats?.values ?? 0}</div>
					<div>
						prompt: {events.filter((event) => event.kind === EK.Prompt).length}
					</div>
					<div>
						data:{" "}
						{(stats?.values ?? 0) -
							(stats?.actions ?? 0) -
							(stats?.reactions ?? 0)}
					</div>
					<div>actions: {stats?.actions ?? 0}</div>
					<div>reactions: {stats?.reactions ?? 0}</div>
					<div className="mt-2">communities: {stats?.communities ?? 0}</div>
					<div>bus dropped: {stats?.dropped ?? 0}</div>
					<div>bootstrap: {stats?.bootstrapNodes ?? 0} peers</div>
					<div>json: {stats?.wireJsonBlobs ?? 0}</div>
					<div className="mt-2 text-white/45">
						wheel: zoom | drag: pan | click: inspect
					</div>
					{topPrograms.length > 0 && (
						<div className="mt-4">
							<div className="mb-1 text-white/38">programs</div>
							{topPrograms.map(([program, count]) => (
								<div key={program}>
									{program}: {count}
								</div>
							))}
						</div>
					)}
				</div>

				<div className="pointer-events-auto max-w-[320px] font-mono text-[10px] leading-4 text-right text-white/65">
					<div className="text-white/38">telemetry</div>
					<div>
						values {stats?.values ?? 0} • communities {stats?.communities ?? 0}
					</div>
					<div>
						actions {stats?.actions ?? 0} • reactions {stats?.reactions ?? 0}
					</div>
					<div>frames {activeSnapshot ? snapshotHistory.length : 0}</div>
					<div>
						{activeSnapshot
							? formatTimestamp(activeSnapshot.timestamp)
							: "waiting"}
					</div>
					{connectionError && (
						<div className="mt-2 text-red-300">{connectionError}</div>
					)}
					{selectedField && (
						<div className="mt-4">
							<div className="text-white/38">field #{selectedField.id}</div>
							<div>
								members {selectedField.memberCount} • sat{" "}
								{selectedField.saturated ? 1 : 0}
							</div>
							<div>
								actions {selectedField.actionCount} • reactions{" "}
								{selectedField.reactionCount}
							</div>
							<div>mode {selectedField.lastAction || "aggregate"}</div>
						</div>
					)}
					{!selectedField && hotFields.length > 0 && (
						<div className="mt-4">
							<div className="mb-1 text-white/38">hot fields</div>
							{hotFields.map((field) => (
								<div key={field.id}>
									#{field.id} m{field.memberCount} c
									{field.concentration.toFixed(2)}
								</div>
							))}
						</div>
					)}
					{selectedEvent && (
						<div className="mt-4 text-white/55">
							<div className="text-white/38">inspect</div>
							<div>{selectedEvent.src || "unknown"}</div>
							<div>{selectedEvent.lbl || "event"}</div>
						</div>
					)}
				</div>
			</div>

			{snapshotHistory.length > 1 && (
				<div className="pointer-events-auto absolute left-1/2 top-4 z-10 flex -translate-x-1/2 items-center gap-3 rounded-full border border-white/10 bg-black/45 px-4 py-2 font-mono text-[10px] text-white/70 backdrop-blur-sm">
					<button
						type="button"
						onClick={() => setScrubEnabled((value) => !value)}
						className="flex items-center gap-2"
					>
						{scrubEnabled ? (
							<IconPlayerPause className="h-4 w-4" />
						) : (
							<IconPlayerPlay className="h-4 w-4" />
						)}
						{scrubEnabled ? "scrub" : "live"}
					</button>
					<input
						type="range"
						min={historyMin}
						max={historyMax}
						value={scrubEnabled ? (scrubMs ?? historyMax) : historyMax}
						onChange={(event) => {
							setScrubEnabled(true);
							setScrubMs(Number(event.target.value));
						}}
						className="w-[280px]"
					/>
					<span>
						{activeSnapshot
							? formatTimestamp(activeSnapshot.timestamp)
							: "waiting"}
					</span>
				</div>
			)}

			<div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-4 pb-4">
				{selection && (
					<div className="pointer-events-auto mb-3 max-h-[34vh] overflow-auto rounded-xl border border-white/10 bg-black/65 p-3 backdrop-blur-sm">
						<ValueInspector
							snap={selection}
							onSelectId={(id) => selectValueById(id)}
						/>
					</div>
				)}
				<div className="pointer-events-auto mx-auto flex max-w-2xl items-center gap-2 rounded-full border border-fuchsia-500/20 bg-black/65 px-3 py-2 backdrop-blur-sm">
					<IconGraph className="h-4 w-4 text-fuchsia-300/70" />
					<Input
						value={promptText}
						onChange={(event) => setPromptText(event.target.value)}
						onKeyDown={(event) => {
							if (event.key === "Enter") {
								submitPrompt();
							}
						}}
						placeholder="Inject prompt into field..."
						className="h-8 border-none bg-transparent font-mono text-sm text-white shadow-none focus-visible:ring-0"
					/>
					<Button
						onClick={submitPrompt}
						className="h-8 rounded-md bg-fuchsia-400/20 px-3 font-mono text-[10px] uppercase tracking-wide text-fuchsia-100 hover:bg-fuchsia-400/30"
					>
						<IconSend className="mr-1 h-3.5 w-3.5" />
						Inject
					</Button>
				</div>
			</div>
		</div>
	);
}
