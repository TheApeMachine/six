import { useSelector } from "@tanstack/react-store";
import { useState } from "react";
import { fieldStore } from "@/lib/field-store";
import type { ColorMode, ScenePreset } from "@/lib/scene-mapping";
import { useViewValues } from "@/lib/view-values";
import { Field3DScene } from "./Field3DScene";
import { SceneToolbar } from "./SceneToolbar";
import { Timeline } from "./Timeline";
import { TopBar } from "./TopBar";
import { ValueDetail } from "./ValueDetail";

/*
Dashboard composes the 3D field, the cursor-aware detail panel, and the
timeline. All three panels read from useViewValues so the view stays
internally consistent: when the cursor is pinned to a past tick, the
scene, the chips, and the inspector all reflect the same projected
state.
*/
export function Dashboard() {
	const selectedId = useSelector(fieldStore, (state) => state.selectedId);
	const connectionError = useSelector(
		fieldStore,
		(state) => state.connectionError,
	);
	const { values, ticksSinceTouch, atHead, cursorTick, tickCount } =
		useViewValues();

	const [colorMode, setColorMode] = useState<ColorMode>("community");
	const [preset, setPreset] = useState<ScenePreset>("all");

	const selected = selectedId ? (values.get(selectedId) ?? null) : null;

	return (
		<div className="flex h-screen flex-col bg-[#05050f] text-white">
			<TopBar
				values={values}
				connectionError={connectionError}
				selectedId={selectedId}
			/>
			<SceneToolbar
				colorMode={colorMode}
				preset={preset}
				onColorModeChange={setColorMode}
				onPresetChange={setPreset}
			/>
			<div className="flex min-h-0 flex-1">
				<div className="relative min-h-0 flex-1 overflow-hidden">
					<Field3DScene
						values={values}
						ticksSinceTouch={ticksSinceTouch}
						selectedId={selectedId}
						colorMode={colorMode}
						preset={preset}
					/>
					<div className="pointer-events-none absolute left-3 top-3 rounded border border-white/10 bg-black/40 px-2 py-1 font-mono text-[10px] text-white/70">
						{atHead ? (
							<span>
								<span className="text-emerald-300">live</span> · {values.size}{" "}
								values
							</span>
						) : (
							<span>
								<span className="text-cyan-300">tick {cursorTick}</span> /{" "}
								{tickCount > 0 ? tickCount - 1 : 0}
								{tickCount === 0 ? (
									<span className="text-white/45"> — no ticks</span>
								) : null}{" "}
								· {values.size} values
							</span>
						)}
					</div>
				</div>
				<div className="min-h-0 w-[420px] shrink-0 overflow-auto border-l border-white/10 p-3">
					<ValueDetail stored={selected} values={values} />
				</div>
			</div>
			<Timeline />
		</div>
	);
}
