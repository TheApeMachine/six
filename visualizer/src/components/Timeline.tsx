import { useSelector } from "@tanstack/react-store";
import { useEffect, useMemo, useRef } from "react";
import {
	setCursorTick,
	setPlaybackRate,
	setPlaying,
	stepCursor,
	timelineStore,
} from "@/lib/timeline-store";

/*
Timeline is the bottom strip of the dashboard. The user reads tick
density (how many frame events the bridge committed at each tick),
sees where the cursor is, and scrubs by clicking or dragging. Play
plays from cursor toward head at playbackRate ticks per second; once
the cursor catches up to the head, playback stops and the view drops
back to live mode automatically. Step buttons are deliberately atomic
so the user can advance one ALU pass at a time when chasing a single
recruitment or selection event.
*/
export function Timeline() {
	const tickCount = useSelector(timelineStore, (state) => state.tickCount);
	const cursorTick = useSelector(timelineStore, (state) => state.cursorTick);
	const playing = useSelector(timelineStore, (state) => state.playing);
	const playbackRate = useSelector(
		timelineStore,
		(state) => state.playbackRate,
	);
	const events = useSelector(timelineStore, (state) => state.events);

	const canvasRef = useRef<HTMLCanvasElement | null>(null);

	const densities = useMemo(() => {
		if (tickCount === 0) {
			return new Uint32Array(0);
		}

		const arr = new Uint32Array(tickCount);
		for (const event of events) {
			if (event.tick >= 0 && event.tick < tickCount) {
				arr[event.tick]++;
			}
		}

		return arr;
	}, [events, tickCount]);

	useEffect(() => {
		const canvas = canvasRef.current;
		if (!canvas) {
			return;
		}

		const ctx = canvas.getContext("2d");
		if (!ctx) {
			return;
		}

		const dpr = window.devicePixelRatio || 1;
		const cssWidth = canvas.clientWidth;
		const cssHeight = canvas.clientHeight;

		canvas.width = cssWidth * dpr;
		canvas.height = cssHeight * dpr;
		ctx.scale(dpr, dpr);

		ctx.fillStyle = "#0a0a14";
		ctx.fillRect(0, 0, cssWidth, cssHeight);

		if (tickCount === 0 || cssWidth === 0) {
			return;
		}

		const ticksPerPixel = tickCount / cssWidth;
		let maxColumn = 1;
		const columns = new Float32Array(cssWidth);

		for (let column = 0; column < cssWidth; column++) {
			const start = Math.floor(column * ticksPerPixel);
			const end = Math.max(start + 1, Math.floor((column + 1) * ticksPerPixel));
			let total = 0;
			for (let tick = start; tick < end && tick < tickCount; tick++) {
				total += densities[tick];
			}
			columns[column] = total;
			if (total > maxColumn) {
				maxColumn = total;
			}
		}

		ctx.fillStyle = "#1f2937";
		for (let column = 0; column < cssWidth; column++) {
			const height = (columns[column] / maxColumn) * (cssHeight - 12);
			ctx.fillRect(column, cssHeight - 4 - height, 1, height);
		}

		const headColumn = Math.min(
			cssWidth - 1,
			Math.floor((tickCount - 1) / ticksPerPixel),
		);
		ctx.fillStyle = "#10b981";
		ctx.fillRect(headColumn, 0, 2, cssHeight);

		if (cursorTick !== null) {
			const cursorColumn = Math.min(
				cssWidth - 1,
				Math.floor(cursorTick / ticksPerPixel),
			);
			ctx.fillStyle = "#06b6d4";
			ctx.fillRect(cursorColumn - 1, 0, 3, cssHeight);
		}
	}, [densities, tickCount, cursorTick]);

	useEffect(() => {
		if (!playing) {
			return;
		}

		const intervalMs = Math.max(8, Math.floor(1000 / playbackRate));
		const handle = window.setInterval(() => {
			const state = timelineStore.state;
			const head = state.tickCount - 1;
			if (head < 0) {
				setPlaying(false);
				return;
			}

			const current = state.cursorTick ?? head;
			if (current >= head) {
				setCursorTick(null);
				setPlaying(false);
				return;
			}

			setCursorTick(current + 1);
		}, intervalMs);

		return () => {
			window.clearInterval(handle);
		};
	}, [playing, playbackRate]);

	const handleScrub = (event: React.MouseEvent<HTMLCanvasElement>) => {
		const canvas = canvasRef.current;
		if (!canvas || tickCount === 0) {
			return;
		}

		const rect = canvas.getBoundingClientRect();
		const x = event.clientX - rect.left;
		const ratio = Math.max(0, Math.min(1, x / rect.width));
		const tick = Math.round(ratio * (tickCount - 1));

		setCursorTick(tick);
		setPlaying(false);
	};

	const head = tickCount > 0 ? tickCount - 1 : 0;
	const cursor = cursorTick === null ? head : cursorTick;
	const atHead = cursorTick === null;

	return (
		<div className="border-t border-white/10 bg-[#0a0a14] px-3 py-2 font-mono text-[11px] text-white/80">
			<div className="flex items-center gap-2">
				<button
					type="button"
					onClick={() => setCursorTick(0)}
					disabled={tickCount === 0}
					className="rounded border border-white/15 px-2 py-1 text-white/80 hover:bg-white/5 disabled:opacity-40"
				>
					⏮
				</button>
				<button
					type="button"
					onClick={() => stepCursor(-1)}
					disabled={tickCount === 0}
					className="rounded border border-white/15 px-2 py-1 text-white/80 hover:bg-white/5 disabled:opacity-40"
				>
					◀
				</button>
				<button
					type="button"
					onClick={() => setPlaying(!playing)}
					disabled={tickCount === 0}
					className={`rounded border px-2 py-1 ${
						playing
							? "border-cyan-400 text-cyan-200"
							: "border-white/15 text-white/80 hover:bg-white/5"
					} disabled:opacity-40`}
				>
					{playing ? "⏸" : "▶"}
				</button>
				<button
					type="button"
					onClick={() => stepCursor(1)}
					disabled={tickCount === 0}
					className="rounded border border-white/15 px-2 py-1 text-white/80 hover:bg-white/5 disabled:opacity-40"
				>
					▶|
				</button>
				<button
					type="button"
					onClick={() => setCursorTick(null)}
					className={`rounded border px-2 py-1 ${
						atHead
							? "border-emerald-400 text-emerald-200"
							: "border-white/15 text-white/80 hover:bg-white/5"
					}`}
				>
					live
				</button>
				<div className="ml-2 flex items-center gap-1 text-[10px] text-white/50">
					<span>tick</span>
					<span className="text-white/85">{cursor}</span>
					<span>/</span>
					<span className="text-white/85">{head}</span>
				</div>
				<div className="ml-auto flex items-center gap-2 text-[10px] text-white/50">
					<span>rate</span>
					<input
						type="range"
						min={1}
						max={120}
						step={1}
						value={playbackRate}
						onChange={(event) =>
							setPlaybackRate(Number(event.currentTarget.value))
						}
						className="accent-cyan-400"
					/>
					<span className="w-10 text-right text-white/85">{playbackRate}/s</span>
				</div>
			</div>
			<canvas
				ref={canvasRef}
				onClick={handleScrub}
				className="mt-2 h-12 w-full cursor-crosshair rounded border border-white/10"
			/>
		</div>
	);
}
