import {
	IconPlayerPause,
	IconPlayerPlay,
	IconSend,
	IconWifi,
	IconWifiOff,
} from "@tabler/icons-react";
import { useMemo, useState } from "react";
import { NodeGraphLegacy } from "@/components/node-graph-legacy";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useField } from "@/context/field-context";
import type { VizGraphSnapshot } from "@/features/telemetry/types";
import { buildValueGraph } from "@/features/values/build-value-graph";
import { cn } from "@/lib/utils";
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

function StatBadge({ label, value }: { label: string; value: number | string }) {
	return (
		<Badge className="border-white/10 bg-white/5 font-mono text-[10px] text-white/65">
			{label}:{" "}
			<span className="text-white/90">{value}</span>
		</Badge>
	);
}

function EmptyInspector() {
	return (
		<Card className="border-white/10 bg-[#090914] text-white/80">
			<CardHeader>
				<CardTitle className="font-mono text-sm">Value Inspector</CardTitle>
			</CardHeader>
			<CardContent className="text-sm text-white/50">
				Click a node in the live graph to inspect its full 1 KB frame below.
			</CardContent>
		</Card>
	);
}

export function FieldViewer({ className }: { className?: string }) {
	const {
		connectionError,
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

	const activeSnapshot = useMemo(() => {
		if (!scrubEnabled || scrubMs === null) {
			return snapshot;
		}

		return nearestSnapshot(snapshotHistory, scrubMs) ?? snapshot;
	}, [scrubEnabled, scrubMs, snapshot, snapshotHistory]);

	const valueGraph = useMemo(() => buildValueGraph(activeSnapshot), [activeSnapshot]);
	const historyMin = snapshotHistory[0]?.timestamp ?? 0;
	const historyMax = snapshotHistory[snapshotHistory.length - 1]?.timestamp ?? 0;

	function submitPrompt() {
		const trimmed = promptText.trim();
		if (!trimmed) {
			return;
		}

		sendPrompt(trimmed);
		setPromptText("");
	}

	return (
		<div className={cn("flex h-screen flex-col bg-[#05050f] text-white", className)}>
			<div className="border-b border-white/10 px-4 py-3">
				<div className="flex flex-wrap items-center gap-3">
					<div className="flex items-center gap-2">
						{connectionError ? (
							<IconWifiOff className="h-4 w-4 text-red-300" />
						) : (
							<IconWifi className="h-4 w-4 text-emerald-300" />
						)}
						<span className="font-mono text-sm text-white/85">Six Visualizer</span>
						<Badge className="border-sky-400/30 bg-sky-400/10 font-mono text-[10px] text-sky-200">
							LIVE
						</Badge>
					</div>
					<div className="ml-auto flex min-w-[320px] flex-1 items-center gap-2 md:max-w-xl">
						<Input
							value={promptText}
							onChange={(event) => setPromptText(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									submitPrompt();
								}
							}}
							placeholder="Inject prompt into bridge..."
							className="border-white/10 bg-white/5 font-mono text-white"
						/>
						<Button onClick={submitPrompt} className="gap-2 font-mono">
							<IconSend className="h-4 w-4" />
							Prompt
						</Button>
					</div>
				</div>
				<div className="mt-3 flex flex-wrap items-center gap-2">
					<StatBadge label="values" value={stats?.values ?? 0} />
					<StatBadge label="communities" value={stats?.communities ?? 0} />
					<StatBadge label="actions" value={stats?.actions ?? 0} />
					<StatBadge label="reactions" value={stats?.reactions ?? 0} />
					<StatBadge label="dropped" value={stats?.dropped ?? 0} />
					{connectionError && (
						<Badge className="border-red-500/30 bg-red-500/10 font-mono text-[10px] text-red-200">
							{connectionError}
						</Badge>
					)}
				</div>
				{snapshotHistory.length > 1 && (
					<div className="mt-3 flex flex-wrap items-center gap-3 rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-xs font-mono text-white/60">
						<button
							type="button"
							onClick={() => setScrubEnabled((value) => !value)}
							className="flex items-center gap-2 text-white/75"
						>
							{scrubEnabled ? (
								<IconPlayerPause className="h-4 w-4" />
							) : (
								<IconPlayerPlay className="h-4 w-4" />
							)}
							{scrubEnabled ? "scrubbing" : "live"}
						</button>
						<input
							type="range"
							min={historyMin}
							max={historyMax}
							value={scrubEnabled ? scrubMs ?? historyMax : historyMax}
							onChange={(event) => {
								setScrubEnabled(true);
								setScrubMs(Number(event.target.value));
							}}
							className="min-w-[240px] flex-1"
						/>
						<span>
							{activeSnapshot ? formatTimestamp(activeSnapshot.timestamp) : "waiting"}
						</span>
					</div>
				)}
			</div>

			<div className="min-h-0 flex-1 overflow-hidden p-4">
				<div className="flex h-full min-h-0 flex-col gap-4">
					<Card className="min-h-0 flex-1 border-white/10 bg-[#090914] p-2">
						<NodeGraphLegacy
							graph={valueGraph}
							showTimeSlider={false}
							onNodeSelect={(_index, name) => {
								selectValueById(name);
							}}
							className="h-full"
						/>
					</Card>

					<div className="max-h-[38vh] min-h-[240px] overflow-auto">
						{selection ? (
							<ValueInspector
								snap={selection}
								onSelectId={(id) => selectValueById(id)}
							/>
						) : (
							<EmptyInspector />
						)}
					</div>
				</div>
			</div>
		</div>
	);
}
