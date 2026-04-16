/*
DiagnosticsHUD polls the bridge's /api/diagnostics endpoint at a fixed cadence
and renders a compact live health card for the operator. The card surfaces
everything the bridge records about the ingest pipe that the main FieldViewer
cannot derive from decoded Value frames alone:

  - frame rate & byte rate computed as deltas between successive polls
  - total frames / bytes since bridge startup
  - connected WebSocket producers/consumers
  - age of the most recent frame (spikes when the Go runtime has stalled)
  - dropped frames (bridge-side history eviction when VIZ_FRAME_HISTORY>0)
  - prompt-control URL, so operators can tell at a glance whether /api/prompt
    will actually reach a live orchestrator

The HUD is the single most useful debugging surface after the canvas itself:
when the particle cloud falls silent it answers "is the bridge alive, are
frames still arriving, has the producer disconnected" without a terminal.
*/

import { useCallback, useEffect, useRef, useState } from "react";
import { telemetryHttpBase } from "@/features/telemetry/endpoint";
import { cn } from "@/lib/utils";

interface BridgeDiagnostics {
	bytesReceived: number;
	clients: number;
	droppedFrames: number;
	frameHistory: number;
	framesReceived: number;
	lastFrameAt: number | null;
}

interface DiagnosticsPayload {
	controlUrl: string | null;
	diagnostics: BridgeDiagnostics;
	frameHistoryLimit: number;
	ingest: string;
	path: string;
}

interface DiagnosticsHUDProps {
	className?: string;
	/** Poll cadence in milliseconds (default 500ms — tight enough to see spikes). */
	pollIntervalMs?: number;
}

/*
formatRate compresses per-second numbers into a one-line label with k/M suffixes
so the operator never has to count zeros. Zero is still rendered so the HUD
doesn't dim out when the stream is quiet.
*/
function formatRate(value: number, unit: string): string {
	if (!Number.isFinite(value) || value < 0) {
		return `— ${unit}`;
	}

	if (value >= 1_000_000) {
		return `${(value / 1_000_000).toFixed(2)}M ${unit}`;
	}

	if (value >= 1_000) {
		return `${(value / 1_000).toFixed(1)}k ${unit}`;
	}

	if (value >= 10) {
		return `${value.toFixed(0)} ${unit}`;
	}

	return `${value.toFixed(1)} ${unit}`;
}

function formatBytes(n: number): string {
	if (!Number.isFinite(n) || n <= 0) return "0 B";
	const units = ["B", "KB", "MB", "GB"] as const;
	let idx = 0;
	let v = n;

	while (v >= 1024 && idx < units.length - 1) {
		v /= 1024;
		idx++;
	}

	const precision = v >= 100 ? 0 : v >= 10 ? 1 : 2;
	return `${v.toFixed(precision)} ${units[idx]}`;
}

function formatAge(nowMs: number, lastMs: number | null): string {
	if (lastMs === null) return "never";
	const delta = Math.max(0, nowMs - lastMs);

	if (delta < 1_000) return `${delta.toFixed(0)} ms`;
	if (delta < 60_000) return `${(delta / 1_000).toFixed(1)} s`;
	if (delta < 3_600_000) return `${(delta / 60_000).toFixed(1)} min`;
	return `${(delta / 3_600_000).toFixed(1)} h`;
}

/*
ageTone maps the "last frame" age onto a traffic-light palette. The thresholds
are deliberately tight — a healthy live run keeps frames flowing sub-second,
and anything >2s is a stall worth flagging.
*/
function ageTone(nowMs: number, lastMs: number | null): string {
	if (lastMs === null) return "text-white/35";
	const delta = nowMs - lastMs;
	if (delta < 750) return "text-emerald-300";
	if (delta < 2_500) return "text-amber-300";
	return "text-red-300";
}

export function DiagnosticsHUD({
	className,
	pollIntervalMs = 500,
}: DiagnosticsHUDProps) {
	const [payload, setPayload] = useState<DiagnosticsPayload | null>(null);
	const [fetchError, setFetchError] = useState<string | null>(null);
	const [rates, setRates] = useState<{ framesPerSec: number; bytesPerSec: number }>(
		{ framesPerSec: 0, bytesPerSec: 0 },
	);
	const [now, setNow] = useState<number>(() => Date.now());

	/*
	previous holds the most recent successful sample so the next poll can derive
	a rate without server-side cooperation. A ref instead of state so the
	setInterval callback always sees the latest values without re-subscribing.
	*/
	const previous = useRef<{
		framesReceived: number;
		bytesReceived: number;
		sampledAtMs: number;
	} | null>(null);

	const fetchDiagnostics = useCallback(async () => {
		try {
			const response = await fetch(`${telemetryHttpBase()}/api/diagnostics`);
			if (!response.ok) {
				throw new Error(`HTTP ${response.status}`);
			}

			const data = (await response.json()) as DiagnosticsPayload;
			const sampledAtMs = Date.now();

			setPayload(data);
			setFetchError(null);
			setNow(sampledAtMs);

			const prior = previous.current;
			if (prior) {
				const dt = Math.max(1, sampledAtMs - prior.sampledAtMs) / 1_000;
				const dFrames = Math.max(
					0,
					data.diagnostics.framesReceived - prior.framesReceived,
				);
				const dBytes = Math.max(
					0,
					data.diagnostics.bytesReceived - prior.bytesReceived,
				);

				setRates({
					framesPerSec: dFrames / dt,
					bytesPerSec: dBytes / dt,
				});
			}

			previous.current = {
				framesReceived: data.diagnostics.framesReceived,
				bytesReceived: data.diagnostics.bytesReceived,
				sampledAtMs,
			};
		} catch (error) {
			setFetchError(error instanceof Error ? error.message : String(error));
		}
	}, []);

	useEffect(() => {
		let cancelled = false;

		void fetchDiagnostics();

		const interval = window.setInterval(() => {
			if (cancelled) return;
			void fetchDiagnostics();
		}, pollIntervalMs);

		/*
		A second timer drives "last frame age" at 10 Hz between fetches so the
		counter ticks smoothly — the operator perceives a stall immediately
		rather than only at the next fetch boundary.
		*/
		const ticker = window.setInterval(() => {
			if (cancelled) return;
			setNow(Date.now());
		}, 100);

		return () => {
			cancelled = true;
			window.clearInterval(interval);
			window.clearInterval(ticker);
		};
	}, [fetchDiagnostics, pollIntervalMs]);

	const diagnostics = payload?.diagnostics;
	const lastFrameAt = diagnostics?.lastFrameAt ?? null;
	const tone = ageTone(now, lastFrameAt);

	return (
		<div
			className={cn(
				"pointer-events-auto flex flex-col gap-0.5 rounded-xl border border-white/10 bg-[#0a0a14]/95 px-3 py-2 font-mono text-[10px] text-white/75 backdrop-blur",
				className,
			)}
		>
			<div className="mb-1 flex items-center justify-between text-[9px] uppercase tracking-widest text-white/40">
				<span>bridge · /api/diagnostics</span>
				<span
					className={cn(
						"tabular-nums",
						fetchError ? "text-red-300/80" : "text-emerald-300/70",
					)}
				>
					{fetchError ? "err" : "live"}
				</span>
			</div>

			{fetchError ? (
				<div className="text-red-300/80 leading-tight">
					{fetchError}
				</div>
			) : !payload ? (
				<div className="text-white/35">polling…</div>
			) : (
				<>
					<div className="flex items-baseline justify-between gap-3">
						<span className="text-white/40">frame rate</span>
						<span className="text-white/90 tabular-nums">
							{formatRate(rates.framesPerSec, "fps")}
						</span>
					</div>
					<div className="flex items-baseline justify-between gap-3">
						<span className="text-white/40">throughput</span>
						<span className="text-white/90 tabular-nums">
							{formatRate(rates.bytesPerSec, "B/s")}
						</span>
					</div>
					<div className="flex items-baseline justify-between gap-3">
						<span className="text-white/40">last frame</span>
						<span className={cn("tabular-nums", tone)}>
							{formatAge(now, lastFrameAt)} ago
						</span>
					</div>
					<div className="mt-1 border-t border-white/5 pt-1 space-y-0.5 text-[9px] text-white/50">
						<div className="flex items-baseline justify-between gap-3">
							<span>total frames</span>
							<span className="tabular-nums text-white/70">
								{diagnostics?.framesReceived.toLocaleString()}
							</span>
						</div>
						<div className="flex items-baseline justify-between gap-3">
							<span>total bytes</span>
							<span className="tabular-nums text-white/70">
								{formatBytes(diagnostics?.bytesReceived ?? 0)}
							</span>
						</div>
						<div className="flex items-baseline justify-between gap-3">
							<span>clients</span>
							<span className="tabular-nums text-white/70">
								{diagnostics?.clients ?? 0}
							</span>
						</div>
						{payload.frameHistoryLimit > 0 && (
							<div className="flex items-baseline justify-between gap-3">
								<span>buffer</span>
								<span className="tabular-nums text-white/70">
									{diagnostics?.frameHistory ?? 0} / {payload.frameHistoryLimit}
								</span>
							</div>
						)}
						{(diagnostics?.droppedFrames ?? 0) > 0 && (
							<div className="flex items-baseline justify-between gap-3">
								<span className="text-amber-300/70">dropped</span>
								<span className="tabular-nums text-amber-300">
									{diagnostics?.droppedFrames.toLocaleString()}
								</span>
							</div>
						)}
					</div>
					<div className="mt-1 flex items-baseline justify-between gap-3 text-[9px] text-white/35">
						<span>prompt control</span>
						<span
							className={cn(
								"tabular-nums truncate max-w-[180px]",
								payload.controlUrl ? "text-white/70" : "text-white/25",
							)}
							title={payload.controlUrl ?? ""}
						>
							{payload.controlUrl ?? "disabled"}
						</span>
					</div>
				</>
			)}
		</div>
	);
}
