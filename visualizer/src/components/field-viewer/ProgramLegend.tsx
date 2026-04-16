/*
ProgramLegend renders the canvas vocabulary as a side panel: one row per
program category with its glyph, colour, canonical name, one-line
description, and the current live count. Counts are computed from the
live graph snapshot so the operator sees at a glance "4 beams in
flight, 1 classification, 0 causal probes" without having to squint at
the canvas itself.
*/

import { useMemo } from "react";
import type { VizGraphSnapshot } from "@/features/telemetry/types";
import { PROGRAM_CATEGORIES, type ProgramCategory } from "@/lib/programClassifier";
import { cn } from "@/lib/utils";

interface ProgramLegendProps {
	snapshot: VizGraphSnapshot | null;
	className?: string;
}

/*
Category ordering matches the narrative arc of a Value: data → plumbing
→ (beam / classify / peer_gap / gap_probe / intervene / inference /
resident) → util. Putting the active categories first keeps the eye on
the behaviour that actually drives the system.
*/
const CATEGORY_ORDER: ProgramCategory[] = [
	"beam",
	"classify",
	"peer_gap",
	"gap_probe",
	"intervene",
	"inference",
	"resident",
	"plumbing",
	"util",
	"unknown",
];

function categoryFromProgram(program: string): ProgramCategory {
	switch (program) {
		case "link":
		case "affinity":
			return "plumbing";
		case "beam_swarm_step":
			return "beam";
		case "active_inference":
			return "inference";
		case "classify_readout":
			return "classify";
		case "peer_gap":
			return "peer_gap";
		case "intervene":
			return "intervene";
		case "gap_probe":
			return "gap_probe";
		case "measure_field":
			return "resident";
		case "popcount":
		case "coupling":
		case "temperature":
			return "util";
		default:
			return "unknown";
	}
}

/*
Glyph renders the canvas shapes as inline SVG so the legend and the p5
canvas use the same vocabulary. Keeping this in sync by hand is
cheaper than cross-rendering p5 into SVG and the shape count is small.
*/
function Glyph({
	category,
	size = 16,
}: {
	category: ProgramCategory;
	size?: number;
}) {
	const style = PROGRAM_CATEGORIES[category];
	const [r, g, b] = style.color;
	const stroke = `rgb(${r}, ${g}, ${b})`;
	const half = size / 2;
	const s = size;

	switch (style.shape) {
		case "square":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<rect
						x={half - 5}
						y={half - 5}
						width={10}
						height={10}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "triangle_up":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<polygon
						points={`${half},${half - 6} ${half + 6},${half + 5} ${half - 6},${half + 5}`}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "triangle_down":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<polygon
						points={`${half},${half + 6} ${half + 6},${half - 5} ${half - 6},${half - 5}`}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "diamond":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<polygon
						points={`${half},${half - 7} ${half + 7},${half} ${half},${half + 7} ${half - 7},${half}`}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "pentagon": {
			const pts: string[] = [];
			for (let i = 0; i < 5; i++) {
				const a = -Math.PI / 2 + (i * Math.PI * 2) / 5;
				pts.push(
					`${half + Math.cos(a) * 6},${half + Math.sin(a) * 6}`,
				);
			}
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<polygon
						points={pts.join(" ")}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		}
		case "hourglass":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<polygon
						points={`${half - 6},${half - 6} ${half + 6},${half - 6} ${half - 6},${half + 6} ${half + 6},${half + 6}`}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "asterisk":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					{Array.from({ length: 6 }).map((_, index) => {
						const a = (index * Math.PI * 2) / 6;
						return (
							<line
								key={index}
								x1={half}
								y1={half}
								x2={half + Math.cos(a) * 7}
								y2={half + Math.sin(a) * 7}
								stroke={stroke}
								strokeWidth={1.2}
							/>
						);
					})}
				</svg>
			);
		case "ring":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<circle
						cx={half}
						cy={half}
						r={6}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
					<circle
						cx={half}
						cy={half}
						r={3}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "bar":
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<rect
						x={half - 7}
						y={half - 2}
						width={14}
						height={4}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
		case "circle":
		default:
			return (
				<svg width={s} height={s} viewBox={`0 0 ${s} ${s}`}>
					<circle
						cx={half}
						cy={half}
						r={5}
						fill="none"
						stroke={stroke}
						strokeWidth={1.2}
					/>
				</svg>
			);
	}
}

export function ProgramLegend({ snapshot, className }: ProgramLegendProps) {
	const counts = useMemo(() => {
		const map = new Map<ProgramCategory, number>();
		for (const category of CATEGORY_ORDER) {
			map.set(category, 0);
		}

		if (!snapshot) return map;

		const bump = (program: string) => {
			const cat = categoryFromProgram(program);
			map.set(cat, (map.get(cat) ?? 0) + 1);
		};

		for (const field of snapshot.fields) {
			for (const member of field.members) {
				bump(member.program);
			}
		}

		for (const orphan of snapshot.orphanValues) {
			bump(orphan.program);
		}

		return map;
	}, [snapshot]);

	return (
		<div
			className={cn(
				"pointer-events-auto flex flex-col gap-1 rounded-xl border border-white/10 bg-[#0a0a14]/95 px-3 py-2 font-mono text-[10px] text-white/75 backdrop-blur",
				className,
			)}
		>
			<div className="mb-1 text-[9px] uppercase tracking-widest text-white/40">
				program activity
			</div>
			{CATEGORY_ORDER.map((category) => {
				const style = PROGRAM_CATEGORIES[category];
				const count = counts.get(category) ?? 0;
				const dim = count === 0;

				return (
					<div
						key={category}
						className={cn(
							"flex items-center gap-2",
							dim ? "text-white/35" : "text-white/85",
						)}
					>
						<Glyph category={category} />
						<span className="flex-1 truncate">
							<span className="font-semibold">{style.label}</span>
							<span className="ml-1 text-white/40">{style.description}</span>
						</span>
						<span
							className={cn(
								"tabular-nums font-semibold",
								dim ? "text-white/35" : "text-white/90",
							)}
						>
							{count}
						</span>
					</div>
				);
			})}
		</div>
	);
}
