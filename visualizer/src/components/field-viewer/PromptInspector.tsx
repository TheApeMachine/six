/*
PromptInspector renders the full lifecycle of the most recent prompt.

It derives every piece of state from the raw event stream rather than storing
its own: prompt text from EventPrompt, token expansion from EventTokenizerEmit,
community routing from EventValueJoinedCommunity, action/reaction counts from
EventCommunityAction/Reaction, beam progression from EventBeamCompose and
EventBeamConverge, and finally the generated text from EventPromptResult.

The panel appears automatically once a prompt event is seen, stays visible
through completion, and can be dismissed. It collapses to a compact badge
when the user wants to reclaim screen space.
*/

import {
	IconChevronDown,
	IconChevronUp,
	IconLoader2,
	IconX,
} from "@tabler/icons-react";
import { useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import type { VizEvent } from "@/lib/wire";
import { EK } from "@/lib/wire";

interface PromptInspectorProps {
	events: VizEvent[];
	className?: string;
}

interface TokenEntry {
	ts: number;
	content: string;
	valueId: string;
}

interface BeamEntry {
	ts: number;
	score: number;
	converged: boolean;
	sequence: string;
}

interface ActionEntry {
	ts: number;
	kind: "action" | "reaction";
	program: string;
	communityId: number;
}

interface JoinEntry {
	ts: number;
	valueId: string;
	communityId: number;
	distance: number;
}

interface PromptLifecycle {
	promptTs: number;
	promptText: string;
	resultTs: number | null;
	resultText: string | null;
	resultScores: Record<string, number>;
	tokens: TokenEntry[];
	joins: JoinEntry[];
	actions: ActionEntry[];
	beamSteps: BeamEntry[];
	elapsedMs: number | null;
	inFlight: boolean;
}

function formatElapsed(ms: number): string {
	if (ms < 1000) return `${ms.toFixed(0)} ms`;
	return `${(ms / 1000).toFixed(2)} s`;
}

function formatTs(tsMicro: number, baseMicro: number): string {
	const delta = (tsMicro - baseMicro) / 1000;
	if (delta < 0) return "0.000";
	if (delta < 1000) return `+${delta.toFixed(0)}ms`;
	return `+${(delta / 1000).toFixed(2)}s`;
}

/*
deriveLifecycle scans backwards through the event stream to find the most
recent prompt and everything that happened after it up to the result (or now).
*/
function deriveLifecycle(events: VizEvent[]): PromptLifecycle | null {
	let promptIdx = -1;
	for (let i = 0; i < events.length; i++) {
		if (events[i].kind === EK.Prompt) {
			promptIdx = i;
			break;
		}
	}
	if (promptIdx < 0) return null;

	const promptEv = events[promptIdx];
	const promptTs = promptEv.ts;
	const promptText = promptEv.meta?.prompt || "";

	let resultTs: number | null = null;
	let resultText: string | null = null;
	let resultScores: Record<string, number> = {};

	const tokens: TokenEntry[] = [];
	const joins: JoinEntry[] = [];
	const actions: ActionEntry[] = [];
	const beamSteps: BeamEntry[] = [];

	// Walk forwards from the prompt (events array is newest-first, so we walk
	// backwards in the array to go forwards in time).
	for (let i = promptIdx; i >= 0; i--) {
		const ev = events[i];
		if (ev.ts < promptTs) continue;

		if (ev.kind === EK.PromptResult && resultTs === null) {
			resultTs = ev.ts;
			resultText = ev.meta?.generation || null;
			resultScores = { ...ev.vals };
		}

		if (ev.kind === EK.TokenizerEmit) {
			tokens.push({
				ts: ev.ts,
				content: ev.meta?.content || ev.lbl || "",
				valueId: ev.meta?.value_id || "",
			});
		}

		if (ev.kind === EK.ValueJoinedCommunity) {
			joins.push({
				ts: ev.ts,
				valueId: ev.meta?.value_id || "",
				communityId: ev.vals?.community_id ?? -1,
				distance: ev.vals?.distance ?? 0,
			});
		}

		if (ev.kind === EK.CommunityAction) {
			actions.push({
				ts: ev.ts,
				kind: "action",
				program: ev.lbl || "unknown",
				communityId: ev.vals?.community_id ?? -1,
			});
		}

		if (ev.kind === EK.CommunityReaction) {
			actions.push({
				ts: ev.ts,
				kind: "reaction",
				program: ev.lbl || "unknown",
				communityId: ev.vals?.community_id ?? -1,
			});
		}

		if (ev.kind === EK.BeamCompose || ev.kind === EK.BeamConverge) {
			beamSteps.push({
				ts: ev.ts,
				score: ev.vals?.best_score ?? ev.vals?.score ?? 0,
				converged: ev.kind === EK.BeamConverge,
				sequence: ev.meta?.sequence || ev.meta?.tokens || "",
			});
		}
	}

	// Sort oldest-first for display.
	tokens.sort((a, b) => a.ts - b.ts);
	joins.sort((a, b) => a.ts - b.ts);
	actions.sort((a, b) => a.ts - b.ts);
	beamSteps.sort((a, b) => a.ts - b.ts);

	const elapsedMs = resultTs !== null ? (resultTs - promptTs) / 1000 : null;
	const inFlight = resultTs === null;

	return {
		promptTs,
		promptText,
		resultTs,
		resultText,
		resultScores,
		tokens,
		joins,
		actions,
		beamSteps,
		elapsedMs,
		inFlight,
	};
}

function ScoreBar({ score, label }: { score: number; label?: string }) {
	const pct = Math.max(0, Math.min(100, score * 100));
	return (
		<div className="flex items-center gap-2 min-w-0">
			{label && (
				<span className="text-[8px] font-mono text-white/30 shrink-0 w-12 text-right">
					{label}
				</span>
			)}
			<div className="flex-1 h-1 rounded-full bg-white/5 overflow-hidden">
				<div
					className="h-full rounded-full bg-amber-400/70 transition-all duration-300"
					style={{ width: `${pct}%` }}
				/>
			</div>
			<span className="text-[9px] font-mono text-amber-300/80 shrink-0 tabular-nums">
				{score.toFixed(4)}
			</span>
		</div>
	);
}

export function PromptInspector({ events, className }: PromptInspectorProps) {
	const [expanded, setExpanded] = useState(true);
	const [dismissed, setDismissed] = useState(false);
	const [prevPromptTs, setPrevPromptTs] = useState<number | null>(null);

	const lifecycle = useMemo(() => deriveLifecycle(events), [events]);

	// Auto-show when a new prompt arrives.
	if (lifecycle && lifecycle.promptTs !== prevPromptTs) {
		setPrevPromptTs(lifecycle.promptTs);
		setDismissed(false);
		setExpanded(true);
	}

	if (!lifecycle || dismissed) return null;

	const {
		promptTs,
		promptText,
		resultText,
		resultScores,
		tokens,
		joins,
		actions,
		beamSteps,
		elapsedMs,
		inFlight,
	} = lifecycle;

	const lastBeam = beamSteps[beamSteps.length - 1] ?? null;
	const resolvedCount = joins.length;

	return (
		<div
			className={cn(
				"absolute bottom-0 right-0 z-30 w-96 border-l border-t bg-card/98 backdrop-blur-sm shadow-xl",
				className,
			)}
		>
			{/* Header bar */}
			<div className="flex items-center gap-2 px-3 py-2 border-b border-white/10">
				<div
					className={cn(
						"size-2 rounded-full shrink-0",
						inFlight
							? "bg-amber-400 animate-pulse shadow-[0_0_6px_rgba(251,191,36,0.8)]"
							: "bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.6)]",
					)}
				/>
				<span className="text-[10px] font-mono font-semibold text-amber-300 uppercase tracking-wider">
					{inFlight ? "Prompt in flight…" : "Prompt complete"}
				</span>
				{inFlight && (
					<IconLoader2 className="size-3 text-amber-400/60 animate-spin ml-0.5" />
				)}
				{elapsedMs !== null && (
					<span className="ml-auto text-[9px] font-mono text-muted-foreground/60 shrink-0">
						{formatElapsed(elapsedMs)}
					</span>
				)}
				{inFlight && (
					<span className="text-[9px] font-mono text-muted-foreground/50 shrink-0 tabular-nums">
						{(Date.now() - promptTs / 1000).toFixed(0)} ms
					</span>
				)}
				<div className="flex items-center gap-1 ml-1">
					<button
						type="button"
						onClick={() => setExpanded((x) => !x)}
						className="text-muted-foreground/50 hover:text-foreground transition-colors"
					>
						{expanded ? (
							<IconChevronDown className="size-3.5" />
						) : (
							<IconChevronUp className="size-3.5" />
						)}
					</button>
					<button
						type="button"
						onClick={() => setDismissed(true)}
						className="text-muted-foreground/50 hover:text-foreground transition-colors"
					>
						<IconX className="size-3.5" />
					</button>
				</div>
			</div>

			{expanded && (
				<div className="max-h-[70vh] overflow-y-auto">
					{/* Prompt text */}
					<div className="px-3 py-2 border-b border-white/10">
						<p className="text-[8px] font-mono text-amber-300/40 uppercase tracking-wider mb-1">
							prompt
						</p>
						<p className="text-[11px] font-mono text-amber-100/90 leading-relaxed wrap-break-word">
							{promptText || (
								<span className="text-white/20 italic">empty</span>
							)}
						</p>
					</div>

					{/* Stats row */}
					<div className="grid grid-cols-4 border-b border-white/10">
						{[
							["tokens", tokens.length],
							["joined", resolvedCount],
							["actions", actions.filter((a) => a.kind === "action").length],
							["beam steps", beamSteps.length],
						].map(([k, v]) => (
							<div
								key={String(k)}
								className="px-2 py-2 border-r border-white/5 last:border-r-0"
							>
								<p className="text-[7px] font-mono text-white/25 uppercase tracking-wider">
									{k}
								</p>
								<p className="text-[13px] font-mono font-semibold text-white/80 tabular-nums">
									{v}
								</p>
							</div>
						))}
					</div>

					{/* Token expansion */}
					{tokens.length > 0 && (
						<div className="px-3 py-2 border-b border-white/10">
							<p className="text-[8px] font-mono text-sky-300/40 uppercase tracking-wider mb-1.5">
								token expansion ({tokens.length})
							</p>
							<div className="flex flex-wrap gap-1">
								{tokens.map((tok, i) => (
									<span
										key={`${tok.ts}-${i}`}
										className="text-[9px] font-mono px-1 py-0.5 rounded bg-sky-500/15 text-sky-200/80 border border-sky-500/20"
										title={tok.valueId}
									>
										{tok.content || `tok_${i}`}
									</span>
								))}
							</div>
						</div>
					)}

					{/* Beam progression */}
					{beamSteps.length > 0 && (
						<div className="px-3 py-2 border-b border-white/10">
							<p className="text-[8px] font-mono text-amber-300/40 uppercase tracking-wider mb-1.5">
								beam progression ({beamSteps.length} steps)
							</p>
							<div className="space-y-1">
								{beamSteps.map((step, i) => (
									<div key={`${step.ts}-${i}`} className="space-y-0.5">
										<div className="flex items-center gap-2">
											<span className="text-[8px] font-mono text-white/20 shrink-0 w-10">
												{formatTs(step.ts, promptTs)}
											</span>
											{step.converged && (
												<span className="text-[8px] font-mono text-emerald-400 shrink-0 font-semibold">
													CONVERGE
												</span>
											)}
											<div className="flex-1">
												<ScoreBar score={step.score} />
											</div>
										</div>
										{step.sequence && (
											<p className="text-[8px] font-mono text-white/35 ml-12 wrap-break-word leading-relaxed">
												"{step.sequence.substring(0, 60)}
												{step.sequence.length > 60 ? "…" : ""}"
											</p>
										)}
									</div>
								))}
							</div>
						</div>
					)}

					{/* Action timeline */}
					{actions.length > 0 && (
						<div className="px-3 py-2 border-b border-white/10">
							<p className="text-[8px] font-mono text-purple-300/40 uppercase tracking-wider mb-1.5">
								actions / reactions
							</p>
							<div className="space-y-0.5">
								{actions.map((act, i) => (
									<div
										key={`${act.ts}-${i}`}
										className="flex items-center gap-2"
									>
										<span className="text-[8px] font-mono text-white/20 shrink-0 w-10">
											{formatTs(act.ts, promptTs)}
										</span>
										<span
											className={cn(
												"text-[8px] font-mono shrink-0",
												act.kind === "action"
													? "text-emerald-400/70"
													: "text-orange-400/70",
											)}
										>
											{act.kind === "action" ? "ACT" : "REA"}
										</span>
										<span className="text-[8px] font-mono text-purple-300/60 shrink-0">
											#{act.communityId}
										</span>
										<span className="text-[8px] font-mono text-white/40 truncate">
											{act.program}
										</span>
									</div>
								))}
							</div>
						</div>
					)}

					{/* Result */}
					{resultText !== null ? (
						<div className="px-3 py-2.5">
							<div className="flex items-center gap-2 mb-1.5">
								<p className="text-[8px] font-mono text-emerald-400/50 uppercase tracking-wider">
									generation
								</p>
								{elapsedMs !== null && (
									<span className="text-[8px] font-mono text-emerald-400/40">
										({formatElapsed(elapsedMs)})
									</span>
								)}
							</div>
							<p className="text-[11px] font-mono text-emerald-100/85 leading-relaxed wrap-break-word whitespace-pre-wrap">
								{resultText}
							</p>
							{Object.keys(resultScores).length > 0 && (
								<div className="mt-2 space-y-1">
									{Object.entries(resultScores)
										.sort(([a], [b]) => a.localeCompare(b))
										.map(([k, v]) => (
											<ScoreBar key={k} score={v} label={k} />
										))}
								</div>
							)}
						</div>
					) : (
						<div className="px-3 py-3 flex items-center gap-2">
							<IconLoader2 className="size-3 text-amber-400/50 animate-spin shrink-0" />
							<span className="text-[9px] font-mono text-muted-foreground/40">
								waiting for generation…
								{lastBeam && (
									<span className="ml-2 text-amber-300/50">
										best score: {lastBeam.score.toFixed(4)}
									</span>
								)}
							</span>
						</div>
					)}
				</div>
			)}
		</div>
	);
}
