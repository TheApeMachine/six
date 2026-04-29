import type { ColorMode, ScenePreset } from "@/lib/scene-mapping";

interface SceneToolbarProps {
	colorMode: ColorMode;
	preset: ScenePreset;
	onColorModeChange: (mode: ColorMode) => void;
	onPresetChange: (preset: ScenePreset) => void;
}

const COLOR_MODES: { value: ColorMode; label: string; hint: string }[] = [
	{ value: "status", label: "status", hint: "PENDING/READY/BUSY/SELECTED/DONE" },
	{ value: "community", label: "community", hint: "hashed community id" },
	{ value: "role", label: "role", hint: "Programmer/Learner/Readout/Prompt" },
	{ value: "firmware", label: "firmware", hint: "classified program" },
];

const PRESETS: { value: ScenePreset; label: string; hint: string }[] = [
	{ value: "all", label: "all", hint: "everything visible" },
	{ value: "queries", label: "queries", hint: "SELECTED + recruiter spokes" },
	{
		value: "recruitment",
		label: "recruitment",
		hint: "members → recruiter (community)",
	},
	{ value: "prompts", label: "prompts", hint: "prompt routing into communities" },
];

/*
SceneToolbar is the lone control surface for the 3D view. Color modes
re-paint instances without touching positions; presets dim everything
that does not match the chosen story (queries selecting peers,
community recruitment unfolding, prompts routing) and add the
relationship edges those stories need. Both axes are independent so
the user can ask "show me recruitment colored by status" or "show me
prompts colored by community" without rebuilding the layout.
*/
export function SceneToolbar({
	colorMode,
	preset,
	onColorModeChange,
	onPresetChange,
}: SceneToolbarProps) {
	return (
		<div className="flex flex-wrap items-center gap-2 border-b border-white/10 bg-[#0a0a14] px-3 py-2 font-mono text-[11px] text-white/80">
			<span className="text-[10px] uppercase tracking-widest text-white/45">
				color
			</span>
			<div className="flex gap-1">
				{COLOR_MODES.map((mode) => (
					<button
						key={mode.value}
						type="button"
						onClick={() => onColorModeChange(mode.value)}
						title={mode.hint}
						className={`rounded border px-2 py-0.5 text-[10px] ${
							colorMode === mode.value
								? "border-cyan-400 bg-cyan-500/10 text-cyan-100"
								: "border-white/15 text-white/70 hover:bg-white/5"
						}`}
					>
						{mode.label}
					</button>
				))}
			</div>
			<span className="ml-3 text-[10px] uppercase tracking-widest text-white/45">
				view
			</span>
			<div className="flex gap-1">
				{PRESETS.map((entry) => (
					<button
						key={entry.value}
						type="button"
						onClick={() => onPresetChange(entry.value)}
						title={entry.hint}
						className={`rounded border px-2 py-0.5 text-[10px] ${
							preset === entry.value
								? "border-emerald-400 bg-emerald-500/10 text-emerald-100"
								: "border-white/15 text-white/70 hover:bg-white/5"
						}`}
					>
						{entry.label}
					</button>
				))}
			</div>
		</div>
	);
}
