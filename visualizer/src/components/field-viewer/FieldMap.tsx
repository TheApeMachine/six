/*
FieldMap renders live communities as a pannable/zoomable canvas with an optional
Wonderlens overlay.

The lens is a draggable circle — it sits fixed in screen space until you drag it
by clicking inside it and moving. Whatever field is under the lens center is
shown in detail (value member particles with labels) inside the lens. The rest of
the canvas is used for free pan/zoom navigation via drag and scroll outside the
lens.

Single-clicking a field bubble outside the lens fires onFieldSelect so the
caller can show a community inspector panel.
*/

import { useCallback, useEffect, useRef } from "react";
import type { FieldSnapshot, FieldValueSnapshot } from "@/lib/engine";

const GOLDEN_ANGLE = 2.399963;
const LENS_RADIUS = 190;
const WORLD_SCALE = 220;

const PROGRAM_COLORS: Record<string, [number, number, number]> = {
	beam_swarm: [0, 255, 150],
	beam_swarm_step: [0, 255, 150],
	causal_explore: [255, 200, 0],
	active_inference: [255, 150, 50],
	classification: [100, 180, 255],
	surprisal: [0, 200, 255],
	falsification: [255, 80, 80],
	affinity: [186, 104, 200],
	aggregate: [186, 104, 200],
};

/*
hslToRgb mirrors the same function in engine.ts so community colors in
FieldMap are consistent with the live canvas view.
*/
function hslToRgb(h: number, s: number, l: number): [number, number, number] {
	const c = (1 - Math.abs(2 * l - 1)) * s;
	const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
	const m = l - c / 2;
	let r = 0,
		g = 0,
		b = 0;
	if (h < 60) {
		r = c;
		g = x;
	} else if (h < 120) {
		r = x;
		g = c;
	} else if (h < 180) {
		g = c;
		b = x;
	} else if (h < 240) {
		g = x;
		b = c;
	} else if (h < 300) {
		r = x;
		b = c;
	} else {
		r = c;
		b = x;
	}
	return [
		Math.round((r + m) * 255),
		Math.round((g + m) * 255),
		Math.round((b + m) * 255),
	];
}

function affinityColor(hex: string): [number, number, number] {
	if (!hex || hex.length < 4) return [186, 104, 200];
	const val = parseInt(hex.substring(0, 8), 16);
	if (Number.isNaN(val)) return [186, 104, 200];
	const hue = ((val % 360) + 360) % 360;
	return hslToRgb(hue, 0.75, 0.62);
}

function programColor(name: string): [number, number, number] {
	return PROGRAM_COLORS[name] ?? [180, 180, 180];
}

function fieldColor(field: FieldSnapshot): [number, number, number] {
	if (field.affinityHex) return affinityColor(field.affinityHex);
	return field.lastAction ? programColor(field.lastAction) : [186, 104, 200];
}

function memberColor(m: FieldValueSnapshot): [number, number, number] {
	if (m.program) return programColor(m.program);
	if (m.role === "action") return [0, 255, 150];
	if (m.role === "reaction") return [255, 150, 50];
	return [100, 180, 255];
}

function fieldWorldPos(index: number): { x: number; y: number } {
	const ring = WORLD_SCALE * 0.3 + Math.sqrt(index) * WORLD_SCALE * 0.22;
	const angle = index * GOLDEN_ANGLE;
	return { x: Math.cos(angle) * ring, y: Math.sin(angle) * ring };
}

function fieldR(count: number): number {
	return 14 + Math.sqrt(Math.max(1, count)) * 6;
}

function lensParticlePos(
	index: number,
	lx: number,
	ly: number,
): { x: number; y: number } {
	const angle = index * GOLDEN_ANGLE;
	const r = 10 + Math.sqrt(index) * 15;
	return { x: lx + Math.cos(angle) * r, y: ly + Math.sin(angle) * r };
}

interface FieldMapProps {
	fields: FieldSnapshot[];
	lensEnabled: boolean;
	onFieldSelect?: (field: FieldSnapshot | null) => void;
}

export function FieldMap({
	fields,
	lensEnabled,
	onFieldSelect,
}: FieldMapProps) {
	const canvasRef = useRef<HTMLCanvasElement>(null);
	const fieldsRef = useRef(fields);
	fieldsRef.current = fields;

	const lensEnabledRef = useRef(lensEnabled);
	lensEnabledRef.current = lensEnabled;

	const onFieldSelectRef = useRef(onFieldSelect);
	onFieldSelectRef.current = onFieldSelect;

	const camRef = useRef({ x: 0, y: 0, zoom: 1 });
	const lensRef = useRef({ x: 0, y: 0, initialized: false });

	// Track mouse-down position to distinguish click from drag.
	const interactRef = useRef({
		mode: null as null | "drag-lens" | "pan-camera",
		startMouseX: 0,
		startMouseY: 0,
		startValX: 0,
		startValY: 0,
		didDrag: false,
	});

	// World positions for fields — recalculated in the draw loop.
	const worldPositionsRef = useRef<{ x: number; y: number }[]>([]);

	useEffect(() => {
		if (!canvasRef.current) return;

		const canvasEl: HTMLCanvasElement = canvasRef.current;
		const ctx = canvasEl.getContext("2d") as CanvasRenderingContext2D;
		let W = 0;
		let H = 0;
		let frame = 0;
		let animId = 0;

		function worldToScreen(wx: number, wy: number): { x: number; y: number } {
			const { x, y, zoom } = camRef.current;
			return { x: (wx - x) * zoom + W / 2, y: (wy - y) * zoom + H / 2 };
		}

		function screenToWorld(sx: number, sy: number): { x: number; y: number } {
			const { x, y, zoom } = camRef.current;
			return { x: (sx - W / 2) / zoom + x, y: (sy - H / 2) / zoom + y };
		}

		function resize() {
			W = canvasEl.clientWidth;
			H = canvasEl.clientHeight;
			canvasEl.width = W;
			canvasEl.height = H;
			if (!lensRef.current.initialized && W > 0 && H > 0) {
				lensRef.current = { x: W / 2, y: H / 2, initialized: true };
			}
		}

		resize();
		const ro = new ResizeObserver(resize);
		ro.observe(canvasEl);

		function draw() {
			animId = requestAnimationFrame(draw);
			frame++;

			const fs = fieldsRef.current;
			const lens = lensRef.current;
			const lensOn = lensEnabledRef.current;

			ctx.fillStyle = "#05050f";
			ctx.fillRect(0, 0, W, H);

			if (fs.length === 0) {
				ctx.fillStyle = "rgba(255,255,255,0.12)";
				ctx.font = "13px monospace";
				ctx.textAlign = "center";
				ctx.fillText("waiting for fields…", W / 2, H / 2);
				return;
			}

			const worldPositions = fs.map((_, i) => fieldWorldPos(i));
			worldPositionsRef.current = worldPositions;
			const screenPositions = worldPositions.map(({ x, y }) =>
				worldToScreen(x, y),
			);

			let lensNearestIdx = -1;
			if (lensOn && lens.initialized) {
				const lensWorld = screenToWorld(lens.x, lens.y);
				let nearestDist = Infinity;
				for (let i = 0; i < fs.length; i++) {
					const d = Math.hypot(
						worldPositions[i].x - lensWorld.x,
						worldPositions[i].y - lensWorld.y,
					);
					if (d < nearestDist) {
						nearestDist = d;
						lensNearestIdx = i;
					}
				}
			}

			const cam = camRef.current;

			// ── Field bubbles ──────────────────────────────────────────────────────
			for (let i = 0; i < fs.length; i++) {
				const f = fs[i];
				const { x, y } = screenPositions[i];
				const r = fieldR(f.memberCount) * cam.zoom;
				const [cr, cg, cb] = fieldColor(f);
				const isLensTarget = lensOn && i === lensNearestIdx;

				if (f.concentration > 0.15 || f.saturated) {
					ctx.fillStyle = `rgba(${cr},${cg},${cb},${f.concentration * 0.1 + (f.saturated ? 0.05 : 0)})`;
					ctx.beginPath();
					ctx.arc(x, y, r * 1.6, 0, Math.PI * 2);
					ctx.fill();
				}

				ctx.fillStyle = `rgba(${cr},${cg},${cb},${isLensTarget ? 0.12 : 0.05 + f.concentration * 0.1})`;
				ctx.beginPath();
				ctx.arc(x, y, r, 0, Math.PI * 2);
				ctx.fill();

				ctx.strokeStyle = `rgba(${cr},${cg},${cb},${isLensTarget ? 0.7 : f.saturated ? 0.7 : 0.35})`;
				ctx.lineWidth = isLensTarget ? 2 : f.saturated ? 2 : 1;
				ctx.beginPath();
				ctx.arc(x, y, r, 0, Math.PI * 2);
				ctx.stroke();

				// Saturation pulse ring.
				if (f.saturated) {
					const pulse = 0.5 + Math.sin(frame * 0.05 + i * 0.7) * 0.5;
					ctx.strokeStyle = `rgba(255,80,80,${pulse * 0.35})`;
					ctx.lineWidth = 1.5;
					ctx.beginPath();
					ctx.arc(x, y, r + 5 + pulse * 5, 0, Math.PI * 2);
					ctx.stroke();
				}

				// Affinity swatch: a small filled arc segment showing the raw hue.
				if (f.affinityHex && cam.zoom > 0.5) {
					const swatchR = r * 0.18;
					const [ar, ag, ab] = affinityColor(f.affinityHex);
					ctx.fillStyle = `rgba(${ar},${ag},${ab},0.85)`;
					ctx.beginPath();
					ctx.arc(x, y, swatchR, 0, Math.PI * 2);
					ctx.fill();
				}

				if (cam.zoom > 0.4) {
					ctx.fillStyle = `rgba(${cr},${cg},${cb},${isLensTarget ? 0.85 : 0.5})`;
					ctx.font = `${Math.max(7, Math.round(9 * cam.zoom))}px monospace`;
					ctx.textAlign = "center";
					ctx.fillText(`#${f.id} [${f.memberCount}]`, x, y + r + 12 * cam.zoom);

					if (cam.zoom > 0.7 && (f.actionCount > 0 || f.reactionCount > 0)) {
						ctx.fillStyle = `rgba(${cr},${cg},${cb},0.35)`;
						ctx.font = `${Math.max(6, Math.round(7 * cam.zoom))}px monospace`;
						ctx.fillText(
							`a:${f.actionCount} r:${f.reactionCount} c:${f.concentration.toFixed(2)}`,
							x,
							y + r + 22 * cam.zoom,
						);
					}
				}

				// Click hint cursor — small dot at bottom of each bubble when hoverable.
				if (cam.zoom > 0.5 && onFieldSelectRef.current) {
					ctx.fillStyle = `rgba(${cr},${cg},${cb},0.2)`;
					ctx.beginPath();
					ctx.arc(x, y, 4, 0, Math.PI * 2);
					ctx.fill();
				}
			}

			if (!lensOn || !lens.initialized) return;

			// ── Wonderlens ────────────────────────────────────────────────────────
			const lx = lens.x;
			const ly = lens.y;
			const focusField = lensNearestIdx >= 0 ? fs[lensNearestIdx] : null;
			const [cr, cg, cb] = focusField
				? fieldColor(focusField)
				: ([180, 180, 180] as [number, number, number]);

			ctx.save();
			ctx.beginPath();
			ctx.arc(lx, ly, LENS_RADIUS, 0, Math.PI * 2);
			ctx.clip();

			ctx.fillStyle = "rgba(3, 3, 14, 0.87)";
			ctx.fillRect(
				lx - LENS_RADIUS,
				ly - LENS_RADIUS,
				LENS_RADIUS * 2,
				LENS_RADIUS * 2,
			);

			if (focusField) {
				const conc = focusField.concentration;

				ctx.fillStyle = `rgba(${cr},${cg},${cb},${0.07 + conc * 0.08})`;
				ctx.beginPath();
				ctx.arc(lx, ly, 28 + conc * 18, 0, Math.PI * 2);
				ctx.fill();
				ctx.strokeStyle = `rgba(${cr},${cg},${cb},0.28)`;
				ctx.lineWidth = 1;
				ctx.beginPath();
				ctx.arc(lx, ly, 28 + conc * 18, 0, Math.PI * 2);
				ctx.stroke();

				for (let mi = 0; mi < focusField.members.length; mi++) {
					const member = focusField.members[mi];
					const { x: px, y: py } = lensParticlePos(mi, lx, ly);

					if (Math.hypot(px - lx, py - ly) > LENS_RADIUS - 14) continue;

					const [mr, mg, mb] = memberColor(member);
					const alpha = member.resolved
						? 1
						: Math.max(0.2, 1 - member.gap * 0.8);

					if (member.resonance > 0.1) {
						ctx.fillStyle = `rgba(${mr},${mg},${mb},${member.resonance * 0.09})`;
						ctx.beginPath();
						ctx.arc(px, py, 10 + member.resonance * 8, 0, Math.PI * 2);
						ctx.fill();
					}

					ctx.save();
					ctx.translate(px, py);
					ctx.strokeStyle = `rgba(${mr},${mg},${mb},${alpha})`;
					ctx.fillStyle = `rgba(${mr},${mg},${mb},${alpha * 0.2})`;
					ctx.lineWidth = 1;

					if (member.role === "action") {
						ctx.beginPath();
						ctx.moveTo(0, -5);
						ctx.lineTo(5, 4);
						ctx.lineTo(-5, 4);
						ctx.closePath();
						ctx.fill();
						ctx.stroke();
					} else if (member.role === "reaction") {
						ctx.beginPath();
						ctx.moveTo(0, 5);
						ctx.lineTo(5, -4);
						ctx.lineTo(-5, -4);
						ctx.closePath();
						ctx.fill();
						ctx.stroke();
					} else {
						ctx.beginPath();
						ctx.rect(-4, -4, 8, 8);
						ctx.fill();
						ctx.stroke();
					}

					if (member.resolved) {
						ctx.strokeStyle = "rgba(0,255,120,0.7)";
						ctx.beginPath();
						ctx.arc(0, 0, 9, 0, Math.PI * 2);
						ctx.stroke();
					}

					const lbl =
						member.label || member.content.substring(0, 20) || member.program;
					if (lbl) {
						ctx.fillStyle = `rgba(${mr},${mg},${mb},${alpha * 0.85})`;
						ctx.font = "8px monospace";
						ctx.textAlign = "left";
						ctx.fillText(lbl.substring(0, 22), 8, 3);
					}

					// Gap bar to the right of the label.
					if (member.gap < 1 && member.gap >= 0) {
						const barW = 28;
						const fill = Math.max(0, 1 - member.gap) * barW;
						ctx.fillStyle = "rgba(255,255,255,0.07)";
						ctx.fillRect(8, 6, barW, 2);
						ctx.fillStyle = member.resolved
							? "rgba(0,255,120,0.7)"
							: `rgba(${mr},${mg},${mb},0.5)`;
						ctx.fillRect(8, 6, fill, 2);
					}

					ctx.restore();
				}
			} else {
				ctx.fillStyle = "rgba(255,255,255,0.18)";
				ctx.font = "11px monospace";
				ctx.textAlign = "center";
				ctx.fillText("drag over a field", lx, ly);
			}

			ctx.restore();

			// ── Lens ring ─────────────────────────────────────────────────────────
			const pulse = 0.65 + Math.sin(frame * 0.07) * 0.35;

			const grad = ctx.createRadialGradient(
				lx,
				ly,
				LENS_RADIUS - 6,
				lx,
				ly,
				LENS_RADIUS + 18,
			);
			grad.addColorStop(0, `rgba(${cr},${cg},${cb},${0.28 * pulse})`);
			grad.addColorStop(1, "rgba(0,0,0,0)");
			ctx.fillStyle = grad;
			ctx.beginPath();
			ctx.arc(lx, ly, LENS_RADIUS + 18, 0, Math.PI * 2);
			ctx.fill();

			ctx.strokeStyle = `rgba(${cr},${cg},${cb},${0.6 + pulse * 0.2})`;
			ctx.lineWidth = 1.5;
			ctx.beginPath();
			ctx.arc(lx, ly, LENS_RADIUS, 0, Math.PI * 2);
			ctx.stroke();

			// Drag handle.
			ctx.fillStyle = `rgba(${cr},${cg},${cb},0.55)`;
			for (let d = -2; d <= 2; d++) {
				ctx.beginPath();
				ctx.arc(lx + d * 7, ly - LENS_RADIUS + 11, 2, 0, Math.PI * 2);
				ctx.fill();
			}

			if (focusField) {
				ctx.fillStyle = `rgba(${cr},${cg},${cb},0.9)`;
				ctx.font = "bold 11px monospace";
				ctx.textAlign = "center";
				ctx.fillText(
					`Field #${focusField.id}  ·  ${focusField.memberCount} values`,
					lx,
					ly - LENS_RADIUS + 28,
				);

				if (focusField.lastAction) {
					ctx.fillStyle = `rgba(${cr},${cg},${cb},0.55)`;
					ctx.font = "9px monospace";
					ctx.fillText(focusField.lastAction, lx, ly - LENS_RADIUS + 43);
				}

				const statStr = `a:${focusField.actionCount}  r:${focusField.reactionCount}  c:${focusField.concentration.toFixed(3)}${focusField.saturated ? "  SATURATED" : ""}`;
				ctx.fillStyle = focusField.saturated
					? "rgba(255,80,80,0.7)"
					: `rgba(${cr},${cg},${cb},0.4)`;
				ctx.font = "8px monospace";
				ctx.fillText(statStr, lx, ly - LENS_RADIUS + 55);
			}
		}

		animId = requestAnimationFrame(draw);

		return () => {
			cancelAnimationFrame(animId);
			ro.disconnect();
		};
	}, []);

	// ── Mouse handlers ─────────────────────────────────────────────────────────

	const handleMouseDown = useCallback(
		(e: React.MouseEvent<HTMLCanvasElement>) => {
			const rect = e.currentTarget.getBoundingClientRect();
			const mx = e.clientX - rect.left;
			const my = e.clientY - rect.top;
			const lens = lensRef.current;
			const interact = interactRef.current;
			interact.didDrag = false;
			interact.startMouseX = mx;
			interact.startMouseY = my;

			if (
				lensEnabledRef.current &&
				lens.initialized &&
				Math.hypot(mx - lens.x, my - lens.y) < LENS_RADIUS
			) {
				interact.mode = "drag-lens";
				interact.startValX = lens.x;
				interact.startValY = lens.y;
				e.currentTarget.style.cursor = "grabbing";
				return;
			}

			interact.mode = "pan-camera";
			interact.startValX = camRef.current.x;
			interact.startValY = camRef.current.y;
			e.currentTarget.style.cursor = "grabbing";
		},
		[],
	);

	const handleMouseMove = useCallback(
		(e: React.MouseEvent<HTMLCanvasElement>) => {
			const rect = e.currentTarget.getBoundingClientRect();
			const mx = e.clientX - rect.left;
			const my = e.clientY - rect.top;
			const interact = interactRef.current;
			const dx = mx - interact.startMouseX;
			const dy = my - interact.startMouseY;

			if (Math.hypot(dx, dy) > 4) interact.didDrag = true;

			if (interact.mode === "drag-lens") {
				lensRef.current = {
					...lensRef.current,
					x: interact.startValX + dx,
					y: interact.startValY + dy,
				};
				return;
			}

			if (interact.mode === "pan-camera") {
				const zoom = camRef.current.zoom;
				camRef.current = {
					...camRef.current,
					x: interact.startValX - dx / zoom,
					y: interact.startValY - dy / zoom,
				};
				return;
			}

			if (lensEnabledRef.current && lensRef.current.initialized) {
				const inside =
					Math.hypot(mx - lensRef.current.x, my - lensRef.current.y) <
					LENS_RADIUS;
				e.currentTarget.style.cursor = inside ? "grab" : "default";
			} else {
				e.currentTarget.style.cursor = "default";
			}
		},
		[],
	);

	const handleMouseUp = useCallback(
		(e: React.MouseEvent<HTMLCanvasElement>) => {
			const interact = interactRef.current;
			const wasDrag = interact.didDrag;
			interact.mode = null;
			interact.didDrag = false;

			const rect = e.currentTarget.getBoundingClientRect();
			const mx = e.clientX - rect.left;
			const my = e.clientY - rect.top;

			const inside =
				lensEnabledRef.current &&
				lensRef.current.initialized &&
				Math.hypot(mx - lensRef.current.x, my - lensRef.current.y) <
					LENS_RADIUS;
			e.currentTarget.style.cursor = inside ? "grab" : "default";

			// A click outside the lens and without drag selects the nearest field.
			if (!wasDrag && !inside && onFieldSelectRef.current) {
				const cam = camRef.current;
				const W = e.currentTarget.clientWidth;
				const H = e.currentTarget.clientHeight;
				const worldX = (mx - W / 2) / cam.zoom + cam.x;
				const worldY = (my - H / 2) / cam.zoom + cam.y;

				const fs = fieldsRef.current;
				let nearestIdx = -1;
				let nearestDist = Infinity;

				for (let i = 0; i < fs.length; i++) {
					const wp = worldPositionsRef.current[i];
					if (!wp) continue;
					const r = (14 + Math.sqrt(Math.max(1, fs[i].memberCount)) * 6) * 1.5;
					const d = Math.hypot(worldX - wp.x, worldY - wp.y);
					if (d < r && d < nearestDist) {
						nearestDist = d;
						nearestIdx = i;
					}
				}

				onFieldSelectRef.current(nearestIdx >= 0 ? fs[nearestIdx] : null);
			}
		},
		[],
	);

	const handleMouseLeave = useCallback(() => {
		interactRef.current.mode = null;
	}, []);

	const handleWheel = useCallback((e: React.WheelEvent<HTMLCanvasElement>) => {
		e.preventDefault();
		const rect = e.currentTarget.getBoundingClientRect();
		const mx = e.clientX - rect.left;
		const my = e.clientY - rect.top;
		const cam = camRef.current;
		const W = e.currentTarget.clientWidth;
		const H = e.currentTarget.clientHeight;

		const worldX = (mx - W / 2) / cam.zoom + cam.x;
		const worldY = (my - H / 2) / cam.zoom + cam.y;
		const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
		const newZoom = Math.max(0.1, Math.min(6, cam.zoom * factor));

		camRef.current = {
			zoom: newZoom,
			x: worldX - (mx - W / 2) / newZoom,
			y: worldY - (my - H / 2) / newZoom,
		};
	}, []);

	const handleDoubleClick = useCallback(() => {
		camRef.current = { x: 0, y: 0, zoom: 1 };
	}, []);

	return (
		<canvas
			ref={canvasRef}
			className="w-full h-full block"
			onMouseDown={handleMouseDown}
			onMouseMove={handleMouseMove}
			onMouseUp={handleMouseUp}
			onMouseLeave={handleMouseLeave}
			onWheel={handleWheel}
			onDoubleClick={handleDoubleClick}
		/>
	);
}
