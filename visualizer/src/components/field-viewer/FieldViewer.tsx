import {
	IconGraph,
	IconSchema,
	IconSend,
	IconWifi,
	IconWifiOff,
	IconX,
} from "@tabler/icons-react";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useField } from "@/context/field-context";
import { FieldLiveCanvas } from "@/features/live/FieldLiveCanvas";
import { cn } from "@/lib/utils";
import { DiagnosticsHUD } from "./DiagnosticsHUD";
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
					<div className="flex w-full items-start justify-end gap-2">
						<Button
							onClick={() => setProgramDrawerOpen((prev) => !prev)}
							className={cn(
								"h-7 rounded-md border px-2 font-mono text-[9px] uppercase tracking-wider",
								programDrawerOpen
									? "border-fuchsia-400/50 bg-fuchsia-500/20 text-fuchsia-100 hover:bg-fuchsia-500/30"
									: "border-white/10 bg-[#0a0a14]/80 text-white/60 hover:bg-white/10",
							)}
							title="Toggle firmware program viewer"
						>
							<IconSchema className="mr-1 h-3.5 w-3.5" />
							programs
						</Button>
					</div>
					<div>
						<div className="text-white/38">telemetry</div>
						<div>values {stats?.values ?? 0}</div>
						<div>fields {snapshot.fields.length}</div>
						<div>{formatTimestamp(snapshot.timestamp)}</div>
						{connectionError && (
							<div className="mt-2 text-red-300">{connectionError}</div>
						)}
					</div>
					<DiagnosticsHUD className="w-[320px] text-left" />
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
					<div className="pointer-events-auto mb-3 max-h-[34vh] overflow-auto rounded-xl border border-white/10 bg-[#0a0a14]/95 p-3">
						<ValueInspector
							snap={selection}
							onSelectId={(id) => selectValueById(id)}
						/>
					</div>
				)}
				<div className="pointer-events-auto mx-auto flex max-w-2xl items-center gap-2 rounded-full border border-fuchsia-500/20 bg-[#0a0a14]/95 px-3 py-2">
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
