import {
	IconGraph,
	IconSchema,
	IconSend,
	IconWifi,
	IconWifiOff,
	IconX,
} from "@tabler/icons-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useField } from "@/context/field-context";
import { FieldLiveCanvas } from "@/features/live/FieldLiveCanvas";
import type { FieldSnapshot } from "@/features/telemetry/types";
import { cn } from "@/lib/utils";
import { ProgramLegend } from "./ProgramLegend";
import { ProgramViewer } from "./ProgramViewer";
import { ValueInspector } from "./ValueInspector";

function formatTimestamp(timestamp: number) {
	return new Date(timestamp).toLocaleTimeString([], {
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	});
}

/*
CommunityMetricsStrip renders the per-field crystallisation fingerprint the
orchestrator emits every tick. It is rendered above the ValueInspector when
the selected Value lives inside a community so the operator can correlate a
single Value's causal state with the crystallisation of its home field — the
only level of the hierarchy the mesh currently publishes metrics for. Kept
inline because it is a single-use presentational helper with no reuse story
outside this viewer.
*/
function CommunityMetricsStrip({ field }: { field: FieldSnapshot }) {
	const causalSum =
		field.hypothesizingCount + field.falsifiedCount + field.interveningCount;
	const crystalColor =
		field.crystallization >= 0.35 ? "text-emerald-300" : "text-amber-300";

	return (
		<div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border border-fuchsia-500/20 bg-fuchsia-500/5 px-3 py-2 font-mono text-[10px] text-white/70">
			<span className="text-[9px] uppercase tracking-widest text-fuchsia-200/90">
				field #{field.id}
			</span>
			<span>
				cov <span className="text-white">{field.coverage.toFixed(2)}</span>
				<span className="text-white/30"> · </span>
				cons <span className="text-white">{field.consensus.toFixed(2)}</span>
				<span className="text-white/30"> · </span>
				lbl <span className="text-white">{field.labelDensity.toFixed(2)}</span>
			</span>
			<span>
				⇒ crystal{" "}
				<span className={crystalColor}>{field.crystallization.toFixed(3)}</span>
			</span>
			<span>
				modes <span className="text-white">{field.modeCount}</span>
				<span className="text-white/30"> · </span>
				dom <span className="text-white">{field.dominantRatio.toFixed(2)}</span>
				<span className="text-white/30"> · </span>π{" "}
				<span className="text-white">{field.pressureMult.toFixed(2)}×</span>
			</span>
			{causalSum > 0 && (
				<span className="flex items-center gap-2 text-[9px]">
					{field.hypothesizingCount > 0 && (
						<span className="text-yellow-200/80">
							h:{field.hypothesizingCount}
						</span>
					)}
					{field.falsifiedCount > 0 && (
						<span className="text-red-300/80">f:{field.falsifiedCount}</span>
					)}
					{field.interveningCount > 0 && (
						<span className="text-fuchsia-200/80">
							i:{field.interveningCount}
						</span>
					)}
				</span>
			)}
			<span className="ml-auto text-white/40">
				{field.memberCount} members
				{field.saturated ? " · saturated" : ""}
			</span>
		</div>
	);
}

export function FieldViewer({ className }: { className?: string }) {
	const {
		connectionError,
		selection,
		sendPrompt,
		selectValueById,
		snapshot,
		stats,
	} = useField();
	const [promptText, setPromptText] = useState("");
	const [programDrawerOpen, setProgramDrawerOpen] = useState(false);

	const selectedField = useMemo(() => {
		if (!selection || selection.communityId < 0) {
			return undefined;
		}

		return snapshot.fields.find(
			(candidate) => candidate.id === selection.communityId,
		);
	}, [selection, snapshot.fields]);

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
				snapshot={snapshot}
				selectedId={selection?.id ?? null}
				onSelectField={() => {}}
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
					<div className="text-white/45">raw Value frames (1024 B)</div>
					<div className="mt-2 text-white/45">
						wheel: zoom | drag: pan | click: inspect
					</div>
				</div>

				<div className="pointer-events-auto flex max-w-[340px] flex-col items-end gap-2 font-mono text-[10px] leading-4 text-right text-white/65">
					<ProgramLegend snapshot={snapshot} className="w-[320px] text-left" />
				</div>
			</div>

			{programDrawerOpen && (
				<div className="pointer-events-auto absolute right-0 top-0 bottom-0 z-20 flex w-[min(640px,55vw)] flex-col border-l border-white/10 bg-[#05050f]/95 backdrop-blur">
					<div className="flex items-center justify-between border-b border-white/10 px-3 py-2 font-mono text-[10px] uppercase tracking-widest text-white/60">
						<span>firmware programs · /api/programs</span>
						<button
							type="button"
							onClick={() => setProgramDrawerOpen(false)}
							className="rounded-md border border-white/10 bg-white/5 p-1 text-white/60 hover:bg-white/10 hover:text-white/90"
							title="Close program viewer"
						>
							<IconX className="h-3.5 w-3.5" />
						</button>
					</div>
					<div className="flex-1 overflow-auto p-3">
						<ProgramViewer
							initialProgram={selection?.program || undefined}
							className="min-w-full"
						/>
					</div>
				</div>
			)}

			<div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-4 pb-4">
				{selection && (
					<div className="pointer-events-auto mb-3 max-h-[34vh] space-y-2 overflow-auto rounded-xl border border-white/10 bg-[#0a0a14]/95 p-3">
						{selection.communityId >= 0 && selectedField ? (
							<CommunityMetricsStrip field={selectedField} />
						) : null}
						<ValueInspector
							snap={selection}
							onSelectId={(id) => selectValueById(id)}
						/>
					</div>
				)}
			</div>
		</div>
	);
}
