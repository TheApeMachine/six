/*
ValueInspector renders the full [128]uint64 Value layout as one horizontal
memory band where every region segment shows its actual live content rather than
just a label. Each segment is a column with a header (name + word range), a
fill bar, and its data. The identity strip and belief gap live in a thin row
above the band.
*/

import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import type { VizInspectSnapshot } from "@/lib/engine";
import { cn } from "@/lib/utils";

interface ValueInspectorProps {
	snap: VizInspectSnapshot;
	className?: string;
	/*
	onSelectId navigates to a different value in the causal chain. Fired when
	the user clicks the PREV or NEXT region in the memory band.
	*/
	onSelectId?: (id: string) => void;
}

const ROLE_COLORS: Record<string, string> = {
	data: "bg-sky-500/20 text-sky-300 border-sky-500/30",
	action: "bg-emerald-500/20 text-emerald-300 border-emerald-500/30",
	reaction: "bg-orange-500/20 text-orange-300 border-orange-500/30",
	prompt: "bg-amber-500/20 text-amber-300 border-amber-500/30",
};

const STATE_LABELS: Record<number, string> = {
	0: "IDLE",
	1: "READY",
	2: "BUSY",
	3: "WAITING",
	4: "DONE",
};

interface RegionProps {
	label: string;
	words: string;
	fill: number;
	width: string;
	headerClass: string;
	barClass: string;
	textClass: string;
	children: React.ReactNode;
}

function Region({
	label,
	words,
	fill,
	width,
	headerClass,
	barClass,
	textClass,
	children,
}: RegionProps) {
	return (
		<div
			className="flex flex-col shrink-0 border-r border-border/20 last:border-r-0 overflow-hidden"
			style={{ width }}
		>
			<div
				className={cn(
					"px-2 pt-1.5 pb-1 border-b border-border/20",
					headerClass,
				)}
			>
				<p
					className={cn(
						"text-[8px] font-mono font-bold uppercase tracking-widest leading-none",
						textClass,
					)}
				>
					{label}
				</p>
				<p className="text-[7px] font-mono text-white/20 leading-none mt-0.5">
					{words}
				</p>
				<div className="mt-1.5 h-0.5 w-full rounded-full bg-white/5">
					<div
						className={cn("h-full rounded-full", barClass)}
						style={{ width: `${Math.max(4, fill * 100)}%` }}
					/>
				</div>
			</div>
			<div
				className={cn(
					"flex-1 p-2 text-[9px] font-mono space-y-0.5 overflow-hidden",
					textClass,
				)}
			>
				{children}
			</div>
		</div>
	);
}

function Dim({ children }: { children: React.ReactNode }) {
	return <span className="opacity-25">{children}</span>;
}

function KV({
	k,
	v,
	vClass,
}: {
	k: string;
	v: React.ReactNode;
	vClass?: string;
}) {
	return (
		<div className="flex items-baseline gap-1 min-w-0">
			<span className="opacity-40 shrink-0 text-[8px]">{k}</span>
			<span className={cn("break-all leading-tight", vClass)}>{v}</span>
		</div>
	);
}

export function ValueInspector({
	snap,
	className,
	onSelectId,
}: ValueInspectorProps) {
	const gapPct = (snap.gap * 100).toFixed(1);
	const gapColor = snap.resolved
		? "text-emerald-400"
		: snap.gap < 0.3
			? "text-emerald-300"
			: snap.gap < 0.7
				? "text-amber-300"
				: "text-red-300";

	const vals = snap.telemetry?.vals ?? {};
	const meta = snap.telemetry?.meta ?? {};

	const stateWord =
		vals["state"] !== undefined ? Math.round(vals["state"]) : -1;
	const stateLabel = STATE_LABELS[stateWord] ?? null;
	const confidence = vals["confidence"] ?? snap.resonance;
	const epoch = vals["epoch"] !== undefined ? Math.round(vals["epoch"]) : null;
	const temperature = vals["temperature"] ?? vals["temp"] ?? null;
	const prediction = vals["prediction"] ?? vals["pred"] ?? null;
	const predictionError =
		vals["prediction_error"] ?? vals["pred_error"] ?? null;
	const affinityHex = meta["affinity"] ?? meta["initial_affinity"] ?? null;

	return (
		<div className={cn("space-y-2", className)}>
			{/* ── Identity + belief gap ─────────────────────────────────────────── */}
			<Card>
				<CardContent className="px-3 py-2 flex items-center gap-3 flex-wrap">
					<div className="flex flex-wrap gap-1.5">
						<Badge variant="outline" className={ROLE_COLORS[snap.role] ?? ""}>
							{snap.role.toUpperCase()}
						</Badge>
						{snap.program && (
							<Badge
								variant="outline"
								className="border-purple-500/30 bg-purple-500/20 text-purple-300"
							>
								{snap.program}
							</Badge>
						)}
						{snap.communityId >= 0 && (
							<Badge variant="secondary">field #{snap.communityId}</Badge>
						)}
						{snap.resolved && (
							<Badge
								variant="outline"
								className="border-emerald-500/30 bg-emerald-500/20 text-emerald-300"
							>
								RESOLVED
							</Badge>
						)}
						{stateLabel && (
							<Badge
								variant="outline"
								className="border-muted/30 text-muted-foreground"
							>
								{stateLabel}
							</Badge>
						)}
					</div>

					<p className="text-[10px] font-mono text-muted-foreground/50 break-all">
						{snap.id}
					</p>

					<div className="ml-auto flex items-center gap-2 shrink-0">
						<div className="w-28 h-1.5 rounded-full bg-secondary overflow-hidden">
							<div
								className={cn(
									"h-full rounded-full transition-all duration-700",
									snap.resolved ? "bg-emerald-400" : "bg-sky-400",
								)}
								style={{ width: `${Math.max(2, (1 - snap.gap) * 100)}%` }}
							/>
						</div>
						<span className={cn("text-[10px] font-mono", gapColor)}>
							{snap.resolved ? "resolved" : `${gapPct}% gap`}
						</span>
						<span className="text-[10px] font-mono text-muted-foreground/40">
							res {snap.resonance.toFixed(3)}
						</span>
					</div>
				</CardContent>
			</Card>

			{/* ── 1 KB memory band ─────────────────────────────────────────────── */}
			<div className="flex rounded-lg overflow-hidden border border-border/40 bg-card min-h-[180px]">
				{/* TOKEN 0–15 · 1024 bits */}
				<Region
					label="TOKEN"
					words="0–15 · 1024b"
					fill={snap.content ? 1 : 0.05}
					width="16%"
					headerClass="bg-sky-500/10"
					barClass="bg-sky-400"
					textClass="text-sky-200/80"
				>
					{snap.label && (
						<KV k="lbl" v={snap.label} vClass="text-amber-200/80" />
					)}
					{snap.content ? (
						<p className="leading-relaxed opacity-90 break-all">
							&ldquo;{snap.content.substring(0, 160)}
							{snap.content.length > 160 ? "…" : ""}&rdquo;
						</p>
					) : (
						<Dim>no telemetry</Dim>
					)}
				</Region>

				{/* PROGRAM 16–23 · 512 bits */}
				<Region
					label="PROGRAM"
					words="16–23 · 512b"
					fill={snap.program ? 1 : 0.05}
					width="9%"
					headerClass="bg-purple-500/10"
					barClass="bg-purple-400"
					textClass="text-purple-200/80"
				>
					{snap.program ? (
						<p className="font-semibold">{snap.program}</p>
					) : (
						<Dim>no telemetry</Dim>
					)}
				</Region>

				{/* SIGNALS 24–31 · 512 bits */}
				<Region
					label="SIGNALS"
					words="24–31 · 512b"
					fill={snap.actionResonance > 0 ? snap.actionResonance : 0.05}
					width="9%"
					headerClass="bg-emerald-500/10"
					barClass="bg-emerald-400"
					textClass="text-emerald-200/80"
				>
					{snap.actionResonance > 0 ? (
						<KV k="wire" v={snap.actionResonance.toFixed(5)} />
					) : (
						<Dim>no telemetry</Dim>
					)}
				</Region>

				{/* CONTEXT 32–39 · 512 bits */}
				<Region
					label="CONTEXT"
					words="32–39 · 512b"
					fill={snap.telemetry?.src ? 0.6 : 0.05}
					width="9%"
					headerClass="bg-cyan-500/10"
					barClass="bg-cyan-400"
					textClass="text-cyan-200/80"
				>
					{snap.telemetry?.src ? (
						<KV k="src" v={snap.telemetry.src} />
					) : vals["context_dot"] !== undefined ? (
						<KV k="dot" v={(vals["context_dot"] as number).toFixed(5)} />
					) : (
						<Dim>no telemetry</Dim>
					)}
				</Region>

				{/* GRADIENT 40–47 · 512 bits */}
				<Region
					label="GRADIENT"
					words="40–47 · 512b"
					fill={vals["gradient_norm"] !== undefined ? 0.7 : 0.05}
					width="9%"
					headerClass="bg-violet-500/10"
					barClass="bg-violet-400"
					textClass="text-violet-200/80"
				>
					{vals["gradient_norm"] !== undefined ? (
						<KV k="norm" v={(vals["gradient_norm"] as number).toFixed(5)} />
					) : snap.telemetry?.tgt ? (
						<KV k="tgt" v={snap.telemetry.tgt} />
					) : (
						<Dim>no telemetry</Dim>
					)}
				</Region>

				{/* PROPERTIES 48–55 · 512 bits — canonical band */}
				<Region
					label="PROPERTIES"
					words="48–55 · 512b"
					fill={1 - snap.gap}
					width="16%"
					headerClass="bg-amber-500/10"
					barClass="bg-amber-400"
					textClass="text-amber-200/80"
				>
					<KV k="w48 labels" v={snap.label || "—"} />
					<KV
						k="w49 confidence"
						v={(confidence as number).toFixed(4)}
						vClass={
							(confidence as number) > 0.7 ? "text-emerald-300" : undefined
						}
					/>
					<KV k="w50 epoch" v={epoch !== null ? String(epoch) : "—"} />
					<KV
						k="w51 role·prog"
						v={`${snap.role}${snap.program ? "·" + snap.program : ""}`}
						vClass="text-purple-300/70"
					/>
					<KV
						k="w52 state"
						v={stateLabel ?? "—"}
						vClass={stateLabel === "DONE" ? "text-emerald-300" : undefined}
					/>
					<KV
						k="w53 temp"
						v={temperature !== null ? (temperature as number).toFixed(3) : "—"}
					/>
					<KV
						k="w54 pred"
						v={prediction !== null ? (prediction as number).toFixed(3) : "—"}
					/>
					<KV
						k="w55 err"
						v={
							predictionError !== null
								? (predictionError as number).toFixed(5)
								: "—"
						}
						vClass={
							predictionError !== null && (predictionError as number) > 0.5
								? "text-red-300"
								: undefined
						}
					/>
				</Region>

				{/* RESERVED 56–119 · 4096 bits */}
				<Region
					label="RESERVED"
					words="56–119 · 4096b"
					fill={0}
					width="8%"
					headerClass="bg-muted/5"
					barClass="bg-muted"
					textClass="text-muted-foreground/25"
				>
					<p>TTL</p>
					<p>probe ABI</p>
					<p>K-store</p>
				</Region>

				{/* PREV 120 */}
				<Region
					label="PREV"
					words="w120 · 64b"
					fill={snap.prevId ? 1 : 0.05}
					width="6%"
					headerClass="bg-indigo-500/10"
					barClass="bg-indigo-400"
					textClass="text-indigo-200/70"
				>
					{snap.prevId ? (
						<button
							type="button"
							onClick={() => onSelectId?.(snap.prevId)}
							className={cn(
								"break-all text-[8px] text-left leading-tight w-full",
								onSelectId
									? "text-indigo-300 underline underline-offset-2 cursor-pointer hover:text-indigo-100"
									: "text-indigo-200/70",
							)}
						>
							← {snap.prevId.substring(0, 16)}
						</button>
					) : (
						<Dim>—</Dim>
					)}
				</Region>

				{/* NEXT 121 */}
				<Region
					label="NEXT"
					words="w121 · 64b"
					fill={snap.nextId ? 1 : 0.05}
					width="6%"
					headerClass="bg-indigo-500/10"
					barClass="bg-indigo-400"
					textClass="text-indigo-200/70"
				>
					{snap.nextId ? (
						<button
							type="button"
							onClick={() => onSelectId?.(snap.nextId)}
							className={cn(
								"break-all text-[8px] text-left leading-tight w-full",
								onSelectId
									? "text-indigo-300 underline underline-offset-2 cursor-pointer hover:text-indigo-100"
									: "text-indigo-200/70",
							)}
						>
							{snap.nextId.substring(0, 16)} →
						</button>
					) : (
						<Dim>—</Dim>
					)}
				</Region>

				{/* ID 122 */}
				<Region
					label="ID"
					words="w122 · 64b"
					fill={1}
					width="6%"
					headerClass="bg-indigo-600/15"
					barClass="bg-indigo-300"
					textClass="text-indigo-100/80"
				>
					<p className="break-all text-[8px]">{snap.id}</p>
				</Region>

				{/* AFFINITY 123–127 · 257 bits */}
				<Region
					label="AFFINITY"
					words="123–127 · 257b"
					fill={affinityHex ? 0.8 : 0.08}
					width="6%"
					headerClass="bg-pink-500/10"
					barClass="bg-pink-400"
					textClass="text-pink-200/70"
				>
					{affinityHex ? (
						<p className="break-all text-[8px]">{affinityHex}</p>
					) : (
						<Dim>CommunityCreated event</Dim>
					)}
				</Region>
			</div>

			{/* ── Wire event telemetry ─────────────────────────────────────────── */}
			{snap.telemetry &&
				(Object.keys(vals).length > 0 || Object.keys(meta).length > 0) && (
					<Card>
						<CardContent className="px-3 py-2">
							<div className="flex flex-wrap gap-x-6 gap-y-0.5">
								<span className="text-[9px] font-mono text-sky-300/60">
									ts {snap.telemetry.ts} µs
								</span>
								{snap.telemetry.lbl && (
									<span className="text-[9px] font-mono text-amber-200/60">
										{snap.telemetry.lbl}
									</span>
								)}
								{Object.entries(vals)
									.sort(([a], [b]) => a.localeCompare(b))
									.map(([k, v]) => (
										<span
											key={k}
											className="text-[9px] font-mono text-muted-foreground/60"
										>
											{k}={" "}
											<span className="text-foreground/70">
												{(v as number)
													.toFixed(5)
													.replace(/(\.\d*?)0+$/, "$1")
													.replace(/\.$/, "")}
											</span>
										</span>
									))}
								{Object.entries(meta)
									.sort(([a], [b]) => a.localeCompare(b))
									.map(([k, v]) => {
										const str = String(v);
										return (
											<span
												key={k}
												className="text-[9px] font-mono text-purple-200/50"
											>
												{k}={" "}
												<span className="text-purple-100/60">
													{str.length > 40 ? str.substring(0, 38) + "…" : str}
												</span>
											</span>
										);
									})}
							</div>
						</CardContent>
					</Card>
				)}
		</div>
	);
}
