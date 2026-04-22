import { useEffect, useRef } from "react";
import type {
	FieldSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";
import { FieldRenderer } from "./fieldRenderer";

/*
FieldLiveCanvas is a thin React shell around FieldRenderer, the WebGL2
instanced renderer that drives the live field view. The shell is
responsible only for mounting two stacked canvases (one for WebGL, one
2D overlay for captions), wiring resize observation, and forwarding
prop updates into the renderer through stable refs — the renderer
itself owns the entire frame loop, particle pool, and input handling
to keep React reconciliation off the hot path.
*/
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
	const containerRef = useRef<HTMLDivElement>(null);
	const glCanvasRef = useRef<HTMLCanvasElement>(null);
	const overlayCanvasRef = useRef<HTMLCanvasElement>(null);
	const rendererRef = useRef<FieldRenderer | null>(null);

	const fieldHandlerRef = useRef(onSelectField);
	fieldHandlerRef.current = onSelectField;
	const valueHandlerRef = useRef(onSelectValue);
	valueHandlerRef.current = onSelectValue;

	// Mount the renderer once. All prop updates funnel through refs/methods.
	useEffect(() => {
		const container = containerRef.current;
		const glCanvas = glCanvasRef.current;
		const overlayCanvas = overlayCanvasRef.current;
		if (!container || !glCanvas) return;

		let renderer: FieldRenderer;
		try {
			renderer = new FieldRenderer(glCanvas, overlayCanvas);
		} catch (err) {
			console.error("FieldRenderer init failed:", err);
			return;
		}
		rendererRef.current = renderer;

		renderer.setHandlers({
			onSelectField: (field) => fieldHandlerRef.current?.(field),
			onSelectValue: (id) => valueHandlerRef.current?.(id),
		});

		const sizeNow = () => {
			renderer.resize(container.clientWidth, container.clientHeight);
		};
		sizeNow();

		const ro = new ResizeObserver(() => sizeNow());
		ro.observe(container);

		renderer.start();

		return () => {
			ro.disconnect();
			renderer.stop();
			renderer.dispose();
			rendererRef.current = null;
		};
	}, []);

	useEffect(() => {
		rendererRef.current?.setSnapshot(snapshot);
	}, [snapshot]);

	useEffect(() => {
		rendererRef.current?.setSelected(selectedId ?? null);
	}, [selectedId]);

	return (
		<div
			ref={containerRef}
			className={
				className ??
				"relative h-full w-full rounded-xl border border-white/10 bg-[#05050f]"
			}
			style={{ position: "relative" }}
		>
			<canvas
				ref={glCanvasRef}
				style={{
					position: "absolute",
					inset: 0,
					width: "100%",
					height: "100%",
					display: "block",
				}}
			/>
			<canvas
				ref={overlayCanvasRef}
				style={{
					position: "absolute",
					inset: 0,
					width: "100%",
					height: "100%",
					display: "block",
					pointerEvents: "none",
				}}
			/>
		</div>
	);
}
