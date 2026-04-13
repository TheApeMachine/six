/*
ProgramViewer renders firmware programs (from config.yml programs:) as
read-only dataflow circuit diagrams.

Each DSL line maps directly to the four-column layout:

  [ srcA region ] ──┬──                           [ dst region ]
                    ├──[ XOR / accumulate ]──►
  [ srcB region ] ──┘

Cross-instruction dependencies (where a dst region feeds a later srcA/srcB)
are drawn as color-coded routing arcs along the right side of the canvas,
one lane per shared region. The color of each arc matches its region type
(tokens=cyan, affinity=violet, signals=amber, etc.) so the data flow through
the pipeline reads at a glance.

Data flows from /api/programs on the viz server; the component is self-contained
and works even when the engine is not processing.
*/

import { Fragment, useEffect, useMemo, useState } from "react";
import { cn } from "@/lib/utils";

// ── Layout constants ──────────────────────────────────────────────────────────

const RW = 128; // region node width
const RH = 34; // region node height
const OW = 80; // op node width
const OH = 50; // op node height
const CG = 32; // column gap
const RS = 76; // row step (vertical distance between instruction baselines)
const PX = 24; // horizontal canvas padding
const PY = 26; // vertical canvas padding (room for column headers)
const ROUTE_BASE_OFFSET = 18; // initial right margin before first routing lane
const LANE_W = 16; // horizontal spacing between routing lanes

// Derived column x-positions
const C0 = PX; // srcA column
const C1 = C0 + RW + CG; // srcB column
const C2 = C1 + RW + CG; // op column
const C3 = C2 + OW + CG; // dst column

// Port y-offsets relative to a row's baseline
const RYO = (OH - RH) / 2; // region top offset — centers RH within OH (= 8)
const RCY = RYO + RH / 2; // region center-y (= 25)
const INA = Math.round(OH * 0.33); // op in-a port y (≈ 16)
const INB = Math.round(OH * 0.67); // op in-b port y (≈ 33)
const OUT = Math.round(OH / 2); // op out port y  (= 25)

function rowY(i: number): number {
	return PY + i * RS;
}

// ── Region color themes ───────────────────────────────────────────────────────

interface RegionTheme {
	bg: string;
	border: string;
	text: string;
	wire: string;
}

const REGION_THEME: Record<string, RegionTheme> = {
	tokens: {
		bg: "rgba(0,212,255,0.09)",
		border: "rgba(0,212,255,0.28)",
		text: "#67e8f9",
		wire: "#00d4ff",
	},
	affinity: {
		bg: "rgba(167,139,250,0.09)",
		border: "rgba(167,139,250,0.28)",
		text: "#c4b5fd",
		wire: "#a78bfa",
	},
	signals: {
		bg: "rgba(251,191,36,0.09)",
		border: "rgba(251,191,36,0.28)",
		text: "#fcd34d",
		wire: "#fbbf24",
	},
	context: {
		bg: "rgba(45,212,191,0.09)",
		border: "rgba(45,212,191,0.28)",
		text: "#5eead4",
		wire: "#2dd4bf",
	},
	gradient: {
		bg: "rgba(251,146,60,0.09)",
		border: "rgba(251,146,60,0.28)",
		text: "#fca5a5",
		wire: "#fb923c",
	},
	properties: {
		bg: "rgba(251,113,133,0.09)",
		border: "rgba(251,113,133,0.28)",
		text: "#fda4af",
		wire: "#fb7185",
	},
	reserved: {
		bg: "rgba(100,116,139,0.07)",
		border: "rgba(100,116,139,0.20)",
		text: "#94a3b8",
		wire: "#64748b",
	},
};

const DEFAULT_THEME: RegionTheme = {
	bg: "rgba(100,116,139,0.07)",
	border: "rgba(100,116,139,0.20)",
	text: "#94a3b8",
	wire: "#64748b",
};

const OP_THEME: Record<string, { bg: string; border: string; text: string }> = {
	xor: {
		bg: "rgba(0,212,255,0.06)",
		border: "rgba(0,212,255,0.22)",
		text: "#67e8f9",
	},
	and: {
		bg: "rgba(167,139,250,0.06)",
		border: "rgba(167,139,250,0.22)",
		text: "#c4b5fd",
	},
	or: {
		bg: "rgba(251,191,36,0.06)",
		border: "rgba(251,191,36,0.22)",
		text: "#fcd34d",
	},
};

const OP_SYMBOL: Record<string, string> = { xor: "⊕", and: "∧", or: "∨" };

// ── DSL parsing ───────────────────────────────────────────────────────────────

interface RegionRef {
	name: string;
	start: number;
	span: number;
	raw: string;
}

interface Instruction {
	index: number;
	srcA: RegionRef;
	srcB: RegionRef;
	dst: RegionRef;
	op: string;
	mode: string;
}

interface ParsedProgram {
	name: string;
	instructions: Instruction[];
	loopsSelf: boolean;
}

function parseRef(raw: string): RegionRef {
	const m = /^(\w+)\[(\d+)(?:,(\d+))?\]$/.exec(raw);
	if (!m) return { name: raw, start: 0, span: 1, raw };
	return {
		name: m[1],
		start: Number(m[2]),
		span: m[3] !== undefined ? Number(m[3]) : 1,
		raw,
	};
}

function parseDSL(name: string, source: string): ParsedProgram {
	const instructions: Instruction[] = [];
	let loopsSelf = false;

	for (const rawLine of source.split("\n")) {
		const line = rawLine.replace(/#.*$/, "").trim();
		if (!line) continue;

		const fields = line.split(/\s+/);

		if (fields[0] === "next") {
			if (fields[1] === "self") loopsSelf = true;
			continue;
		}

		if (fields.length >= 5) {
			instructions.push({
				index: instructions.length,
				srcA: parseRef(fields[0]),
				srcB: parseRef(fields[1]),
				dst: parseRef(fields[2]),
				op: fields[3].toLowerCase(),
				mode: fields[4].toLowerCase(),
			});
		}
	}

	return { name, instructions, loopsSelf };
}

// ── Wire geometry ─────────────────────────────────────────────────────────────

interface WireSpec {
	d: string;
	color: string;
	opacity: number;
	dashed: boolean;
}

/*
bezier returns a cubic bezier SVG path between two points. The control points
are placed along the horizontal axis so that connections always arrive and
leave horizontally, matching the port directions.
*/
function bezier(x1: number, y1: number, x2: number, y2: number): string {
	const cp = Math.max(Math.abs(x2 - x1) * 0.52, 44);
	return `M ${x1} ${y1} C ${x1 + cp} ${y1}, ${x2 - cp} ${y2}, ${x2} ${y2}`;
}

/*
computeWires derives all wire paths for a parsed program.

Intra-row wires (dim): srcA → op.in-a, srcB → op.in-b, op.out → dst.
Cross-row wires (colorful): wherever a dst region of row i feeds srcA/srcB of
a later row j, a bezier arc routes right along the routing channel, then swings
left to reach the source column. Multiple arcs are stacked in parallel lanes.
*/
function computeWires(
	instructions: Instruction[],
	canvasW: number,
): WireSpec[] {
	const wires: WireSpec[] = [];
	const INTRA = "rgba(75,85,110,0.55)";

	for (const { index, srcA: _srcA, srcB: _srcB } of instructions) {
		const base = rowY(index);

		// srcA → op in-a
		wires.push({
			d: bezier(C0 + RW, base + RCY, C2, base + INA),
			color: INTRA,
			opacity: 1,
			dashed: false,
		});
		// srcB → op in-b
		wires.push({
			d: bezier(C1 + RW, base + RCY, C2, base + INB),
			color: INTRA,
			opacity: 1,
			dashed: false,
		});
		// op out → dst
		wires.push({
			d: bezier(C2 + OW, base + OUT, C3, base + RCY),
			color: INTRA,
			opacity: 1,
			dashed: false,
		});
	}

	// Build a map: region raw string → rows that produce/consume it.
	const map = new Map<
		string,
		{ prod: number[]; consA: number[]; consB: number[] }
	>();

	const entry = (key: string) => {
		let val = map.get(key);
		if (!val) {
			val = { prod: [], consA: [], consB: [] };
			map.set(key, val);
		}
		return val;
	};

	for (const { index, srcA, srcB, dst } of instructions) {
		entry(dst.raw).prod.push(index);
		entry(srcA.raw).consA.push(index);
		entry(srcB.raw).consB.push(index);
	}

	let lane = 0;
	const routeBaseX = C3 + RW + ROUTE_BASE_OFFSET;

	for (const [raw, usage] of map) {
		const theme = REGION_THEME[parseRef(raw).name] ?? DEFAULT_THEME;

		for (const prodRow of usage.prod) {
			const consumers = [
				...usage.consA
					.filter((c) => c > prodRow)
					.map((c) => ({ row: c, colX: C0 })),
				...usage.consB
					.filter((c) => c > prodRow)
					.map((c) => ({ row: c, colX: C1 })),
			];

			for (const { row: consRow, colX } of consumers) {
				// Route: exit dst right port → arc through routing channel → enter src left port.
				// The cubic bezier control points at routeX force both the exit (rightward from
				// dst) and the entry (coming from the right toward src) to look natural.
				const routeX = routeBaseX + lane * LANE_W;
				const y1 = rowY(prodRow) + RCY;
				const y2 = rowY(consRow) + RCY;

				wires.push({
					d: `M ${C3 + RW} ${y1} C ${routeX} ${y1}, ${routeX} ${y2}, ${colX} ${y2}`,
					color: theme.wire,
					opacity: 0.6,
					dashed: true,
				});

				lane++;
			}
		}
	}

	void canvasW; // used by the SVG width attribute
	return wires;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function regionNode(
	ref: RegionRef,
	left: number,
	top: number,
): React.ReactNode {
	const t = REGION_THEME[ref.name] ?? DEFAULT_THEME;
	return (
		<div
			style={{
				position: "absolute",
				left,
				top,
				width: RW,
				height: RH,
				background: t.bg,
				border: `1px solid ${t.border}`,
				borderRadius: 8,
				display: "flex",
				flexDirection: "column",
				alignItems: "center",
				justifyContent: "center",
				gap: 1,
			}}
		>
			<span
				style={{
					fontFamily: "monospace",
					fontSize: 10,
					fontWeight: 700,
					color: t.text,
					letterSpacing: "0.04em",
					textTransform: "uppercase",
				}}
			>
				{ref.name}
			</span>
			<span
				style={{
					fontFamily: "monospace",
					fontSize: 8,
					color: t.text,
					opacity: 0.5,
				}}
			>
				[{ref.start},{ref.span}]
			</span>
		</div>
	);
}

// ── Component ─────────────────────────────────────────────────────────────────

interface ProgramViewerProps {
	initialProgram?: string;
	className?: string;
}

export function ProgramViewer({
	initialProgram,
	className,
}: ProgramViewerProps) {
	const [programs, setPrograms] = useState<Record<string, string>>({});
	const [loading, setLoading] = useState(true);
	const [fetchError, setFetchError] = useState<string | null>(null);
	const [selected, setSelected] = useState(initialProgram ?? "");

	// Fetch firmware programs from the viz server once on mount.
	// biome-ignore lint/correctness/useExhaustiveDependencies: intentional one-shot fetch; initialProgram changes are handled by the effect below
	useEffect(() => {
		const meta = import.meta as unknown as {
			env?: Record<string, string>;
		};
		const host = meta.env?.VITE_VIZ_HOST || "localhost";
		const port = meta.env?.VITE_VIZ_PORT || "6600";

		fetch(`http://${host}:${port}/api/programs`)
			.then((r) => {
				if (!r.ok) throw new Error(`HTTP ${r.status}`);
				return r.json() as Promise<Record<string, string>>;
			})
			.then((data) => {
				setPrograms(data);
				const names = Object.keys(data).sort();
				if (names.length > 0) {
					const init =
						initialProgram && data[initialProgram] ? initialProgram : names[0];
					setSelected((prev) => (prev && data[prev] ? prev : init));
				}
				setLoading(false);
			})
			.catch((e: unknown) => {
				setFetchError(String(e));
				setLoading(false);
			});
	}, []);

	// Jump to a specific program when the parent navigation requests it.
	useEffect(() => {
		if (initialProgram && programs[initialProgram]) {
			setSelected(initialProgram);
		}
	}, [initialProgram, programs]);

	const parsed = useMemo(() => {
		if (!selected || !programs[selected]) return null;
		return parseDSL(selected, programs[selected]);
	}, [selected, programs]);

	// Canvas width accounts for routing lane count.
	const maxLanes = useMemo(() => {
		if (!parsed) return 0;
		let lanes = 0;
		const map = new Map<string, { prod: number[]; cons: number[] }>();
		const entry = (k: string) => {
			let val = map.get(k);
			if (!val) {
				val = { prod: [], cons: [] };
				map.set(k, val);
			}
			return val;
		};
		for (const { index, srcA, srcB, dst } of parsed.instructions) {
			entry(dst.raw).prod.push(index);
			entry(srcA.raw).cons.push(index);
			entry(srcB.raw).cons.push(index);
		}
		for (const [, usage] of map) {
			for (const prod of usage.prod) {
				lanes += usage.cons.filter((c) => c > prod).length;
			}
		}
		return lanes;
	}, [parsed]);

	const canvasW =
		C3 + RW + ROUTE_BASE_OFFSET + Math.max(maxLanes, 1) * LANE_W + 12;
	const canvasH = parsed ? PY + parsed.instructions.length * RS + OH + 24 : 200;

	const wires = useMemo(
		() => (parsed ? computeWires(parsed.instructions, canvasW) : []),
		[parsed, canvasW],
	);

	const names = Object.keys(programs).sort();

	if (loading) {
		return (
			<div className={cn("flex items-center justify-center h-full", className)}>
				<span className="text-xs font-mono text-muted-foreground animate-pulse">
					Loading programs…
				</span>
			</div>
		);
	}

	if (fetchError) {
		return (
			<div
				className={cn(
					"flex flex-col items-center justify-center h-full gap-2",
					className,
				)}
			>
				<span className="text-xs font-mono text-destructive">{fetchError}</span>
				<span className="text-[10px] text-muted-foreground">
					Is the viz server running at port 6600?
				</span>
			</div>
		);
	}

	return (
		<div className={cn("flex flex-col h-full overflow-hidden", className)}>
			{/* Program selector */}
			<div className="shrink-0 flex items-center gap-3 px-4 py-2 border-b bg-card/50 backdrop-blur-sm flex-wrap">
				<span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider shrink-0">
					Program
				</span>
				<div className="flex items-center gap-1.5 flex-wrap">
					{names.map((name) => (
						<button
							key={name}
							type="button"
							onClick={() => setSelected(name)}
							className={cn(
								"text-[10px] font-mono px-2 py-0.5 rounded border transition-all cursor-pointer",
								selected === name
									? "bg-accent/20 border-accent/40 text-accent-foreground"
									: "bg-muted/30 border-border text-muted-foreground hover:border-muted-foreground/50 hover:text-foreground",
							)}
						>
							{name}
						</button>
					))}
				</div>
			</div>

			{/* Circuit area */}
			<div className="flex-1 overflow-auto p-5">
				{parsed && (
					<div className="flex flex-col items-start gap-4">
						{/* Header */}
						<div className="flex items-center gap-3 flex-wrap">
							<h2 className="text-sm font-mono font-semibold">{parsed.name}</h2>
							<span className="text-[10px] font-mono text-muted-foreground">
								{parsed.instructions.length} instruction
								{parsed.instructions.length !== 1 ? "s" : ""}
							</span>
							{parsed.loopsSelf && (
								<span className="text-[9px] font-mono px-1.5 py-0.5 rounded border border-violet-500/30 bg-violet-500/10 text-violet-300 tracking-wide">
									↻ next self
								</span>
							)}
						</div>

						{/* Circuit canvas */}
						<div
							style={{
								position: "relative",
								width: canvasW,
								height: canvasH,
								flexShrink: 0,
							}}
						>
							{/* Column header labels */}
							{(
								[
									{
										x: C0,
										w: RW,
										label: "Src A",
									},
									{
										x: C1,
										w: RW,
										label: "Src B",
									},
									{ x: C2, w: OW, label: "Op" },
									{
										x: C3,
										w: RW,
										label: "Dst",
									},
								] as const
							).map(({ x, w, label }) => (
								<div
									key={label}
									style={{
										position: "absolute",
										left: x,
										top: 0,
										width: w,
										height: PY - 6,
										display: "flex",
										alignItems: "center",
										justifyContent: "center",
									}}
								>
									<span
										style={{
											fontFamily: "monospace",
											fontSize: 8,
											fontWeight: 600,
											color: "rgba(148,163,184,0.3)",
											letterSpacing: "0.18em",
											textTransform: "uppercase",
										}}
									>
										{label}
									</span>
								</div>
							))}

							{/* Wire SVG overlay */}
							<svg
								aria-hidden="true"
								style={{
									position: "absolute",
									inset: 0,
									width: canvasW,
									height: canvasH,
									overflow: "visible",
									pointerEvents: "none",
								}}
							>
								{wires.map((wire) => (
									<path
										key={wire.d}
										d={wire.d}
										stroke={wire.color}
										strokeWidth={1.5}
										strokeOpacity={wire.opacity}
										strokeDasharray={wire.dashed ? "5 5" : undefined}
										fill="none"
										strokeLinecap="round"
									/>
								))}
							</svg>

							{/* Instruction rows */}
							{parsed.instructions.map(
								({ index, srcA, srcB, dst, op, mode }) => {
									const base = rowY(index);
									const opT = OP_THEME[op] ?? OP_THEME.xor;

									return (
										<Fragment key={index}>
											{regionNode(srcA, C0, base + RYO)}
											{regionNode(srcB, C1, base + RYO)}

											{/* Op node */}
											<div
												style={{
													position: "absolute",
													left: C2,
													top: base,
													width: OW,
													height: OH,
													background: opT.bg,
													border: `1px solid ${opT.border}`,
													borderRadius: 10,
													display: "flex",
													flexDirection: "column",
													alignItems: "center",
													justifyContent: "center",
													gap: 2,
												}}
											>
												<span
													style={{
														fontFamily: "monospace",
														fontSize: 22,
														color: opT.text,
														lineHeight: 1,
													}}
												>
													{OP_SYMBOL[op] ?? op.toUpperCase()}
												</span>
												<span
													style={{
														fontFamily: "monospace",
														fontSize: 7,
														color: opT.text,
														opacity: 0.5,
														letterSpacing: "0.09em",
														textTransform: "uppercase",
													}}
												>
													{mode}
												</span>
											</div>

											{regionNode(dst, C3, base + RYO)}
										</Fragment>
									);
								},
							)}
						</div>

						{/* Legend: region color key */}
						<div className="flex items-center gap-2 flex-wrap">
							<span className="text-[9px] font-mono text-muted-foreground/40 uppercase tracking-wider mr-1">
								Regions
							</span>
							{Object.entries(REGION_THEME).map(([name, theme]) => (
								<span
									key={name}
									className="text-[9px] font-mono px-1.5 py-0.5 rounded"
									style={{
										background: theme.bg,
										border: `1px solid ${theme.border}`,
										color: theme.text,
									}}
								>
									{name}
								</span>
							))}
						</div>

						{/* DSL source as collapsible reference */}
						<details className="w-full max-w-2xl">
							<summary className="text-[10px] font-mono text-muted-foreground/35 cursor-pointer select-none hover:text-muted-foreground/60 transition-colors">
								source DSL
							</summary>
							<pre className="mt-2 text-[10px] font-mono text-muted-foreground/60 bg-muted/15 rounded-lg p-3 leading-relaxed overflow-x-auto whitespace-pre">
								{programs[selected]}
							</pre>
						</details>
					</div>
				)}
			</div>
		</div>
	);
}
