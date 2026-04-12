import { useEffect, useRef, useState } from "react";
import {
	initEngine,
	type VizCallbacks,
	type VizInspectSnapshot,
	type VizRuntimeStats,
} from "../lib/engine";

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

			<div className="absolute top-4 right-4 z-20 w-[min(440px,calc(100vw-2rem))] max-h-[min(520px,70vh)] overflow-y-auto rounded border border-white/15 bg-black/75 p-3 text-left shadow-lg backdrop-blur-sm">
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

				<div className="text-[10px] uppercase tracking-wider text-emerald-400/70 mb-1">
					selection (json)
				</div>

				{inspect ? (
					<pre className="whitespace-pre-wrap break-all text-[11px] leading-snug text-emerald-100/90">
						{JSON.stringify(inspect, null, 2)}
					</pre>
				) : (
					<p className="text-[11px] text-white/35">
						Click a particle on the canvas to attach the last wire snapshot for
						that value (vals/meta from the backend).
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
