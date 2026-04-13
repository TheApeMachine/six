import { useEffect, useRef, useState } from "react";
import {
	initEngine,
	type VizCallbacks,
	type VizInspectSnapshot,
	type VizRuntimeStats,
} from "../lib/engine";

function InspectorPanel({ snap }: { snap: VizInspectSnapshot }) {
	const roleColors: Record<string, string> = {
		data: "text-sky-300",
		action: "text-emerald-300",
		reaction: "text-orange-300",
		prompt: "text-amber-300",
	};

	const gapPct = (snap.gap * 100).toFixed(1);
	const gapColor = snap.resolved
		? "text-emerald-400"
		: snap.gap < 0.3
			? "text-emerald-300"
			: snap.gap < 0.7
				? "text-amber-300"
				: "text-red-300";

	return (
		<div className="space-y-3">
			<div className="border-b border-white/10 pb-2">
				<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
					identity
				</div>
				<div className="text-[11px] text-white/90 break-all font-semibold">
					{snap.id}
				</div>
				<div className="flex gap-3 mt-1 text-[11px]">
					<span className={roleColors[snap.role] || "text-white/60"}>
						{snap.role.toUpperCase()}
					</span>
					{snap.program && (
						<span className="text-purple-300">program: {snap.program}</span>
					)}
				</div>
			</div>

			<div className="border-b border-white/10 pb-2">
				<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
					belief gap
				</div>
				<div className="flex items-center gap-3">
					<div className="flex-1 h-2 bg-white/10 rounded-full overflow-hidden">
						<div
							className={`h-full rounded-full transition-all ${snap.resolved ? "bg-emerald-400" : "bg-sky-400"}`}
							style={{ width: `${Math.max(1, (1 - snap.gap) * 100)}%` }}
						/>
					</div>
					<span className={`text-[11px] font-mono ${gapColor}`}>
						{snap.resolved ? "RESOLVED" : `${gapPct}% gap`}
					</span>
				</div>
				<div className="flex gap-4 mt-1 text-[10px] text-white/50">
					<span>resonance: {snap.resonance.toFixed(3)}</span>
					{snap.actionResonance > 0 && (
						<span className="text-emerald-400/70">
							wire resonance: {snap.actionResonance.toFixed(4)}
						</span>
					)}
				</div>
			</div>

			{snap.communityId >= 0 && (
				<div className="border-b border-white/10 pb-2">
					<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
						community
					</div>
					<div className="text-[11px] text-purple-300">#{snap.communityId}</div>
				</div>
			)}

			{(snap.label || snap.content) && (
				<div className="border-b border-white/10 pb-2">
					<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
						content
					</div>
					{snap.label && (
						<div className="text-[11px] text-amber-200/80">
							label: {snap.label}
						</div>
					)}
					{snap.content && (
						<div className="text-[11px] text-emerald-200/80 break-all">
							&ldquo;{snap.content}&rdquo;
						</div>
					)}
				</div>
			)}

			{(snap.prevId || snap.nextId) && (
				<div className="border-b border-white/10 pb-2">
					<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
						causal chain
					</div>
					<div className="text-[10px] text-indigo-300/70 font-mono break-all">
						{snap.prevId && <div>prev: {snap.prevId}</div>}
						{snap.nextId && <div>next: {snap.nextId}</div>}
					</div>
				</div>
			)}

			<div className="border-b border-white/10 pb-2">
				<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
					position
				</div>
				<div className="text-[10px] text-white/50 font-mono">
					({snap.pos.x.toFixed(1)}, {snap.pos.y.toFixed(1)})
				</div>
			</div>

			{snap.telemetry && (
				<div>
					<div className="text-[10px] uppercase tracking-wider text-white/40 mb-1">
						last wire event
					</div>
					<div className="text-[10px] text-white/50 font-mono space-y-0.5">
						<div className="text-sky-300/60">ts: {snap.telemetry.ts} µs</div>
						{snap.telemetry.src && <div>src: {snap.telemetry.src}</div>}
						{snap.telemetry.tgt && <div>tgt: {snap.telemetry.tgt}</div>}
						{snap.telemetry.lbl && (
							<div className="text-amber-200/60">lbl: {snap.telemetry.lbl}</div>
						)}
					</div>

					{Object.keys(snap.telemetry.vals).length > 0 && (
						<div className="mt-1">
							<div className="text-[9px] uppercase tracking-wider text-sky-400/40 mb-0.5">
								vals
							</div>
							<div className="text-[10px] text-sky-200/60 font-mono space-y-0.5">
								{Object.entries(snap.telemetry.vals)
									.sort(([a], [b]) => a.localeCompare(b))
									.map(([k, v]) => (
										<div key={k}>
											{k}:{" "}
											{typeof v === "number"
												? v.toFixed(6).replace(/0+$/, "0")
												: v}
										</div>
									))}
							</div>
						</div>
					)}

					{Object.keys(snap.telemetry.meta).length > 0 && (
						<div className="mt-1">
							<div className="text-[9px] uppercase tracking-wider text-purple-400/40 mb-0.5">
								meta
							</div>
							<div className="text-[10px] text-purple-200/60 font-mono space-y-0.5 break-all">
								{Object.entries(snap.telemetry.meta)
									.sort(([a], [b]) => a.localeCompare(b))
									.map(([k, v]) => (
										<div key={k}>
											{k}: {v.length > 64 ? `${v.substring(0, 62)}..` : v}
										</div>
									))}
							</div>
						</div>
					)}
				</div>
			)}
		</div>
	);
}

export default function Scene() {
	const containerRef = useRef<HTMLDivElement>(null);
	const [promptText, setPromptText] = useState("");
	const [runtime, setRuntime] = useState<VizRuntimeStats | null>(null);
	const [inspect, setInspect] = useState<VizInspectSnapshot | null>(null);
	const engineRef = useRef<ReturnType<typeof initEngine> | null>(null);

	useEffect(() => {
		if (!containerRef.current) return;

		const callbacks: VizCallbacks = {
			onEvent: () => {},
			onStats: (stats) => setRuntime(stats),
			onSelection: (sel) => setInspect(sel),
		};

		engineRef.current = initEngine(containerRef.current, callbacks);

		return () => {
			engineRef.current?.destroy();
		};
	}, []);

	return (
		<div className="w-screen h-screen bg-[#050510] text-white overflow-hidden font-mono text-sm">
			<div ref={containerRef} className="absolute inset-0 z-0" />

			<div className="absolute top-4 right-4 z-20 w-[min(440px,calc(100vw-2rem))] max-h-[min(600px,80vh)] overflow-y-auto rounded border border-white/15 bg-black/80 p-3 text-left shadow-lg backdrop-blur-sm">
				<div className="text-[10px] uppercase tracking-wider text-white/40 mb-2">
					telemetry
				</div>

				{runtime && (
					<div className="mb-3 text-[11px] leading-relaxed text-white/75 border-b border-white/10 pb-2">
						<div>
							values {runtime.values} · communities {runtime.communities} ·
							actions {runtime.actions} · reactions {runtime.reactions}
						</div>
						<div
							className={
								runtime.dropped > 0 ? "text-red-300/95" : "text-white/45"
							}
						>
							bus dropped {runtime.dropped}
						</div>
						<div className="text-white/45">
							bootstrap {runtime.bootstrapNodes} peers · json frames{" "}
							{runtime.wireJsonBlobs}
						</div>
					</div>
				)}

				{inspect ? (
					<InspectorPanel snap={inspect} />
				) : (
					<p className="text-[11px] text-white/35">
						Click a particle on the canvas to inspect it.
					</p>
				)}
			</div>

			<div className="absolute bottom-4 left-1/2 -translate-x-1/2 z-20 w-full max-w-2xl px-4">
				<form
					onSubmit={(e) => {
						e.preventDefault();
						if (promptText.trim() && engineRef.current) {
							engineRef.current.sendPrompt(promptText);
							setPromptText("");
						}
					}}
					className="flex gap-2"
				>
					<input
						type="text"
						value={promptText}
						onChange={(e) => setPromptText(e.target.value)}
						placeholder="Inject prompt into field..."
						className="flex-1 bg-black/80 border border-white/20 rounded px-4 py-3 text-white placeholder-gray-500 focus:outline-none focus:border-[#ba68c8] transition-colors"
					/>
					<button
						type="submit"
						className="bg-[#ba68c8]/20 hover:bg-[#ba68c8]/40 border border-[#ba68c8]/50 text-[#ba68c8] px-6 py-3 rounded font-bold transition-colors"
					>
						INJECT
					</button>
				</form>
			</div>
		</div>
	);
}
