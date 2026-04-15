import { useEffect, useRef } from "react";
import type {
	FieldSnapshot,
	FieldValueSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";
import { WHEEL_ZOOM_SENSITIVITY, wheelDeltaToPixels } from "@/lib/wheelInput";

const GOLDEN_ANGLE = 2.399963;
const FIELD_SCALE = 260;

function fieldPosition(index: number) {
	const ring = 120 + Math.sqrt(index + 1) * FIELD_SCALE * 0.18;
	const angle = index * GOLDEN_ANGLE;
	return { x: Math.cos(angle) * ring, y: Math.sin(angle) * ring };
}

function communityRadius(memberCount: number) {
	return 28 + Math.sqrt(Math.max(1, memberCount)) * 8;
}

function memberPosition(index: number) {
	const angle = index * GOLDEN_ANGLE;
	const radius = 18 + Math.sqrt(index + 1) * 18;
	return {
		x: Math.cos(angle) * radius,
		y: Math.sin(angle) * radius,
	};
}

function orphanPosition(index: number) {
	const angle = index * GOLDEN_ANGLE;
	const radius = 260 + Math.sqrt(index + 1) * 26;
	return {
		x: Math.cos(angle) * radius,
		y: Math.sin(angle) * radius,
	};
}

function affinityColor(hex: string): string {
	if (!hex || hex.length < 8) {
		return "rgba(186,104,200,0.95)";
	}

	const value = Number.parseInt(hex.slice(0, 8), 16);
	if (Number.isNaN(value)) {
		return "rgba(186,104,200,0.95)";
	}

	const hue = ((value % 360) + 360) % 360;
	return `hsla(${hue}, 80%, 68%, 0.95)`;
}

function valueColor(value: FieldValueSnapshot): string {
	if (value.role === "prompt") return "rgba(245,158,11,0.95)";
	if (value.role === "action") return "rgba(16,185,129,0.95)";
	if (value.role === "reaction") return "rgba(249,115,22,0.95)";
	if (value.resolved) return "rgba(74,222,128,0.95)";
	return "rgba(96,165,250,0.95)";
}

interface FieldLiveCanvasProps {
	className?: string;
	selectedId?: string | null;
	snapshot: VizGraphSnapshot | null;
	onSelectField?: (field: FieldSnapshot | null) => void;
	onSelectValue?: (id: string) => void;
}

export function FieldLiveCanvas({
	className,
	selectedId,
	snapshot,
	onSelectField,
	onSelectValue,
}: FieldLiveCanvasProps) {
	const canvasRef = useRef<HTMLCanvasElement>(null);
	const snapshotRef = useRef(snapshot);
	snapshotRef.current = snapshot;

	const selectedIdRef = useRef(selectedId ?? null);
	selectedIdRef.current = selectedId ?? null;

	const fieldSelectRef = useRef(onSelectField);
	fieldSelectRef.current = onSelectField;

	const valueSelectRef = useRef(onSelectValue);
	valueSelectRef.current = onSelectValue;

	const cameraRef = useRef({ x: 0, y: 0, zoom: 1 });
	const interactRef = useRef({
		didDrag: false,
		mode: null as null | "pan",
		startCameraX: 0,
		startCameraY: 0,
		startMouseX: 0,
		startMouseY: 0,
	});

	useEffect(() => {
		if (!canvasRef.current) {
			return;
		}

		const canvas = canvasRef.current;
		const ctx = canvas.getContext("2d") as CanvasRenderingContext2D;

		let width = 0;
		let height = 0;
		let animation = 0;

		function resize() {
			width = canvas.clientWidth;
			height = canvas.clientHeight;
			canvas.width = width;
			canvas.height = height;
		}

		function worldToScreen(wx: number, wy: number) {
			const camera = cameraRef.current;
			return {
				x: (wx - camera.x) * camera.zoom + width / 2,
				y: (wy - camera.y) * camera.zoom + height / 2,
			};
		}

		function screenToWorld(sx: number, sy: number) {
			const camera = cameraRef.current;
			return {
				x: (sx - width / 2) / camera.zoom + camera.x,
				y: (sy - height / 2) / camera.zoom + camera.y,
			};
		}

		function draw() {
			animation = requestAnimationFrame(draw);
			ctx.clearRect(0, 0, width, height);
			ctx.fillStyle = "#05050f";
			ctx.fillRect(0, 0, width, height);

			const current = snapshotRef.current;
			if (!current) {
				ctx.fillStyle = "rgba(255,255,255,0.18)";
				ctx.font = "13px monospace";
				ctx.textAlign = "center";
				ctx.fillText("waiting for telemetry...", width / 2, height / 2);
				return;
			}

			ctx.textAlign = "center";
			ctx.lineWidth = 1;

			current.fields.forEach((field, fieldIndex) => {
				const fieldWorld = fieldPosition(fieldIndex);
				const fieldScreen = worldToScreen(fieldWorld.x, fieldWorld.y);
				const fieldR =
					communityRadius(field.memberCount) * cameraRef.current.zoom;

				ctx.strokeStyle = affinityColor(field.affinityHex);
				ctx.fillStyle = field.saturated
					? "rgba(127,29,29,0.18)"
					: "rgba(124,58,237,0.12)";
				ctx.beginPath();
				ctx.arc(fieldScreen.x, fieldScreen.y, fieldR, 0, Math.PI * 2);
				ctx.fill();
				ctx.stroke();

				ctx.fillStyle = "rgba(255,255,255,0.72)";
				ctx.font = "11px monospace";
				ctx.fillText(`#${field.id}`, fieldScreen.x, fieldScreen.y - 4);
				ctx.fillStyle = "rgba(255,255,255,0.45)";
				ctx.font = "9px monospace";
				ctx.fillText(
					`${field.memberCount} members`,
					fieldScreen.x,
					fieldScreen.y + 10,
				);

				field.members.forEach((member, memberIndex) => {
					const offset = memberPosition(memberIndex);
					const memberScreen = worldToScreen(
						fieldWorld.x + offset.x,
						fieldWorld.y + offset.y,
					);
					const selected = selectedIdRef.current === member.id;

					ctx.beginPath();
					ctx.fillStyle = valueColor(member);
					ctx.arc(
						memberScreen.x,
						memberScreen.y,
						selected ? 8 : 5,
						0,
						Math.PI * 2,
					);
					ctx.fill();

					if (selected) {
						ctx.strokeStyle = "rgba(255,255,255,0.85)";
						ctx.lineWidth = 2;
						ctx.stroke();
						ctx.lineWidth = 1;
					}
				});
			});

			current.orphanValues.forEach((value, index) => {
				const pos = orphanPosition(index);
				const screen = worldToScreen(pos.x, pos.y);
				const selected = selectedIdRef.current === value.id;

				ctx.beginPath();
				ctx.fillStyle = valueColor(value);
				ctx.arc(screen.x, screen.y, selected ? 10 : 7, 0, Math.PI * 2);
				ctx.fill();

				if (selected) {
					ctx.strokeStyle = "rgba(255,255,255,0.9)";
					ctx.lineWidth = 2;
					ctx.stroke();
					ctx.lineWidth = 1;
				}

				if (value.content) {
					ctx.fillStyle = "rgba(255,255,255,0.55)";
					ctx.font = "9px monospace";
					ctx.fillText(value.content.slice(0, 18), screen.x, screen.y + 18);
				}
			});
		}

		function pickAt(clientX: number, clientY: number) {
			const current = snapshotRef.current;
			if (!current) {
				return;
			}

			const rect = canvas.getBoundingClientRect();
			const world = screenToWorld(clientX - rect.left, clientY - rect.top);

			for (
				let fieldIndex = 0;
				fieldIndex < current.fields.length;
				fieldIndex++
			) {
				const field = current.fields[fieldIndex];
				const fieldWorld = fieldPosition(fieldIndex);

				for (
					let memberIndex = 0;
					memberIndex < field.members.length;
					memberIndex++
				) {
					const member = field.members[memberIndex];
					const offset = memberPosition(memberIndex);
					const dx = fieldWorld.x + offset.x - world.x;
					const dy = fieldWorld.y + offset.y - world.y;
					if (Math.hypot(dx, dy) <= 16 / cameraRef.current.zoom) {
						valueSelectRef.current?.(member.id);
						return;
					}
				}

				if (
					Math.hypot(fieldWorld.x - world.x, fieldWorld.y - world.y) <=
					communityRadius(field.memberCount)
				) {
					fieldSelectRef.current?.(field);
					return;
				}
			}

			for (let index = 0; index < current.orphanValues.length; index++) {
				const value = current.orphanValues[index];
				const pos = orphanPosition(index);
				if (
					Math.hypot(pos.x - world.x, pos.y - world.y) <=
					18 / cameraRef.current.zoom
				) {
					valueSelectRef.current?.(value.id);
					return;
				}
			}

			fieldSelectRef.current?.(null);
		}

		function onMouseDown(event: MouseEvent) {
			interactRef.current.mode = "pan";
			interactRef.current.didDrag = false;
			interactRef.current.startMouseX = event.clientX;
			interactRef.current.startMouseY = event.clientY;
			interactRef.current.startCameraX = cameraRef.current.x;
			interactRef.current.startCameraY = cameraRef.current.y;
		}

		function onMouseMove(event: MouseEvent) {
			if (interactRef.current.mode !== "pan") {
				return;
			}

			const dx = event.clientX - interactRef.current.startMouseX;
			const dy = event.clientY - interactRef.current.startMouseY;

			if (Math.abs(dx) > 2 || Math.abs(dy) > 2) {
				interactRef.current.didDrag = true;
			}

			cameraRef.current.x =
				interactRef.current.startCameraX - dx / cameraRef.current.zoom;
			cameraRef.current.y =
				interactRef.current.startCameraY - dy / cameraRef.current.zoom;
		}

		function onMouseUp(event: MouseEvent) {
			if (!interactRef.current.didDrag) {
				pickAt(event.clientX, event.clientY);
			}

			interactRef.current.mode = null;
		}

		function onWheel(event: WheelEvent) {
			event.preventDefault();
			const rect = canvas.getBoundingClientRect();
			const before = screenToWorld(
				event.clientX - rect.left,
				event.clientY - rect.top,
			);
			const delta = wheelDeltaToPixels(event, width, height);
			const scale = Math.exp(-(delta.deltaY * WHEEL_ZOOM_SENSITIVITY) / 100);
			cameraRef.current.zoom = Math.min(
				6,
				Math.max(0.3, cameraRef.current.zoom * scale),
			);
			const after = screenToWorld(
				event.clientX - rect.left,
				event.clientY - rect.top,
			);
			cameraRef.current.x += before.x - after.x;
			cameraRef.current.y += before.y - after.y;
		}

		resize();
		const observer = new ResizeObserver(resize);
		observer.observe(canvas);
		canvas.addEventListener("mousedown", onMouseDown);
		canvas.addEventListener("mousemove", onMouseMove);
		canvas.addEventListener("mouseup", onMouseUp);
		canvas.addEventListener("mouseleave", onMouseUp);
		canvas.addEventListener("wheel", onWheel, { passive: false });
		draw();

		return () => {
			cancelAnimationFrame(animation);
			observer.disconnect();
			canvas.removeEventListener("mousedown", onMouseDown);
			canvas.removeEventListener("mousemove", onMouseMove);
			canvas.removeEventListener("mouseup", onMouseUp);
			canvas.removeEventListener("mouseleave", onMouseUp);
			canvas.removeEventListener("wheel", onWheel);
		};
	}, []);

	return (
		<canvas
			ref={canvasRef}
			className={
				className ??
				"h-full w-full rounded-xl border border-white/10 bg-[#05050f]"
			}
		/>
	);
}
