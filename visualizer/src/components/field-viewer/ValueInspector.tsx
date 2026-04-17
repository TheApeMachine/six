/*
ValueInspector renders the full [128]uint64 Value layout as one horizontal
memory band where every region segment shows its actual live content rather than
just a label. Each segment is a column with a header (name + word range), a
fill bar, and its data. The identity strip and belief gap live in a thin row
above the band.
*/

import { type ReactNode, useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import type { VizInspectSnapshot } from "@/features/telemetry/types";
import { cn } from "@/lib/utils";
import { affinityHexWords, VALUE_FRAME_BYTE_LENGTH } from "@/lib/valueLayout";
import {
	affinityHexFromRegions,
	chainPreview,
	decodeProgramWire,
	decodeValueRegionsFromFrame,
	formatWordHexAt,
} from "@/lib/valueRegions";

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

const PROBE_STATUS_LABELS: Record<number, string> = {
	0: "inactive",
	1: "active",
	2: "settled",
	3: "ceiling",
};

/*
FIRMWARE_STATUS_LABELS mirrors pkg/compute/kernel/layout.go:StatusRaw/StatusReady
stored at absolute word 57 (kernel calls it properties[9], config layout puts it
at the start of the Asset region). 0=raw means the firmware chain has not
completed; 1=ready means link → affinity → resident all wrote their words.
*/
const FIRMWARE_STATUS_LABELS: Record<number, string> = {
	0: "raw",
	1: "ready",
};

/*
FALSIFIED_BIT is the high bit (1<<62) the ALU ORs into the noise word when a
Popper-style refutation streak fires. Surfacing it separately from the raw hex
means an operator can see at a glance that the frame has been refuted without
decoding bytes mentally.
*/
const FALSIFIED_BIT = 1n << 62n;

/*
formatValueWordId pads hex value ids to one 64-bit word for display parity
with on-device word layout.
*/
function formatValueWordId(id: string): string {
	if (/^[0-9a-fA-F]+$/.test(id)) {
		return id.length <= 16 ? id.padStart(16, "0") : id;
	}

	return id;
}

/*
effectiveProgram returns the last firmware name the ALU reported for this
Value. When the wire hasn't yet carried an ALUDispatch event for this id
the band shows "unscheduled" instead of lying about a routing default —
with the rule-driven chain live, silence here is meaningful signal.
*/
function effectiveProgram(snap: VizInspectSnapshot): string {
	if (snap.program) return snap.program;

	return "";
}

/*
FIRMWARE_CHAIN_STEPS is the canonical order the rule engine walks for a
freshly minted Value. Rendering every step (lit or dim) makes it obvious
which rules have already fired versus which are still pending.
*/
const FIRMWARE_CHAIN_STEPS = ["link", "affinity", "resident"] as const;

type FirmwareChainStep = (typeof FIRMWARE_CHAIN_STEPS)[number];

function latestFirmwareStepName(snap: VizInspectSnapshot): string {
	if (snap.firmwareSteps.length === 0) return "";

	return snap.firmwareSteps[snap.firmwareSteps.length - 1].name;
}

function stepIsObserved(
	snap: VizInspectSnapshot,
	step: FirmwareChainStep,
): boolean {
	return snap.firmwareSteps.some((entry) => entry.name === step);
}

const STEP_COLORS: Record<FirmwareChainStep, string> = {
	link: "border-indigo-500/40 bg-indigo-500/20 text-indigo-200",
	affinity: "border-pink-500/40 bg-pink-500/20 text-pink-200",
	resident: "border-emerald-500/40 bg-emerald-500/20 text-emerald-200",
};

const STEP_IDLE_CLASS = "border-border/30 bg-muted/10 text-muted-foreground/40";

interface RegionProps {
	label: string;
	words: string;
	fill: number;
	headerClass: string;
	barClass: string;
	textClass: string;
	children: ReactNode;
}

const Region = ({
	label,
	words,
	fill,
	headerClass,
	barClass,
	textClass,
	children,
}: RegionProps) => {
	return (
		<div className="flex flex-col shrink-0 border-r border-border/20 last:border-r-0 overflow-hidden">
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
					"flex-1 min-h-0 p-2 text-[9px] font-mono space-y-0.5 overflow-y-auto overflow-x-hidden",
					textClass,
				)}
			>
				{children}
			</div>
		</div>
	);
};

function Dim({ children }: { children: ReactNode }) {
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

function hexOrDash(value: string | null): string {
	return value ?? "—";
}

/*
WordHexRows prints one row per word index using the same read path as the kernel
(LE uint64 hex). Matches REGION_SPECS in valueRegions.ts — there is no separate
“reserved” band; words 56–119 are the ASSET region.
*/
function WordHexRows({
	from,
	to,
	wordAt,
}: {
	from: number;
	to: number;
	wordAt: (wordIndex: number) => string | null;
}) {
	const rows: ReactNode[] = [];

	for (let wi = from; wi <= to; wi++) {
		const hex = wordAt(wi);

		rows.push(
			<div
				key={wi}
				className="flex gap-1 font-mono text-[7px] leading-snug min-w-0"
			>
				<span className="text-muted-foreground/45 shrink-0 w-7">w{wi}</span>
				<span className="break-all min-w-0">{hex ?? "—"}</span>
			</div>,
		);
	}

	return <div className="space-y-0.5 text-left">{rows}</div>;
}

function decodeProbeStatus(wordHex: string | null): string | null {
	if (!wordHex) return null;

	try {
		const word = BigInt(`0x${wordHex}`);
		const status = Number((word >> 8n) & 0xffn);
		return PROBE_STATUS_LABELS[status] ?? `status ${status}`;
	} catch {
		return null;
	}
}

/*
decodeFirmwareStatus splits the low byte (lifecycle label) from the rest of the
status word. Raw frames read 0, bootstrapped frames read 1; the higher bits are
reserved for future lifecycle flags.
*/
function decodeFirmwareStatus(wordHex: string | null): string | null {
	if (!wordHex) return null;

	try {
		const word = BigInt(`0x${wordHex}`);
		const status = Number(word & 0xffn);

		return FIRMWARE_STATUS_LABELS[status] ?? `status ${status}`;
	} catch {
		return null;
	}
}

/*
decodeNoiseFalsified reports whether the kernel's refutation sentinel bit has
been OR-ed into the noise word. Returns null if the word is unreadable so the
UI can stay silent instead of lying.
*/
function decodeNoiseFalsified(wordHex: string | null): boolean | null {
	if (!wordHex) return null;

	try {
		const word = BigInt(`0x${wordHex}`);

		return (word & FALSIFIED_BIT) !== 0n;
	} catch {
		return null;
	}
}

/*
formatFrameAge compresses "how long since the last wire frame for this Value"
into a compact label. Zero and negative deltas render "just now" so the UI
doesn't flicker between 0ms / 1ms readouts between rerenders.
*/
function formatFrameAge(nowMs: number, receivedAtMs: number): string {
	if (receivedAtMs <= 0) return "never";

	const delta = Math.max(0, nowMs - receivedAtMs);

	if (delta < 50) return "just now";
	if (delta < 1_000) return `${delta.toFixed(0)} ms ago`;
	if (delta < 60_000) return `${(delta / 1_000).toFixed(1)} s ago`;
	if (delta < 3_600_000) return `${(delta / 60_000).toFixed(1)} min ago`;

	return `${(delta / 3_600_000).toFixed(1)} h ago`;
}

/*
frameAgeTone picks a traffic-light class for the age pill. Thresholds match
DiagnosticsHUD: frames <750ms are green, <2.5s amber, older is red — matches
the operator's mental model of "alive / slow / stalled".
*/
function frameAgeTone(nowMs: number, receivedAtMs: number): string {
	if (receivedAtMs <= 0) return "text-white/35";

	const delta = nowMs - receivedAtMs;

	if (delta < 750) return "text-emerald-300";
	if (delta < 2_500) return "text-amber-300";

	return "text-red-300";
}

/*
decodeRegionRef unpacks a packed RegionRef word (low32=start, high32=span) into
a human-readable string. Used for the probe-window lane which stores a
PackRegionRef over token words.
*/
function decodeRegionRef(wordHex: string | null): string | null {
	if (!wordHex) return null;

	try {
		const word = BigInt(`0x${wordHex}`);
		const start = Number(word & 0xffff_ffffn);
		const span = Number((word >> 32n) & 0xffff_ffffn);

		if (start === 0 && span === 0) return null;

		return `w${start}+${span}`;
	} catch {
		return null;
	}
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

	/*
	The selected Value keeps its wireFrame until a new one arrives. An age
	ticker driven by a local 250ms interval lets the operator watch the frame
	go stale in real time — useful for spotting Values that the ALU has
	stopped cycling even though the bridge is still healthy.
	*/
	const [nowMs, setNowMs] = useState<number>(() => Date.now());

	useEffect(() => {
		const interval = window.setInterval(() => setNowMs(Date.now()), 250);

		return () => window.clearInterval(interval);
	}, []);

	const frame = snap.wireFrame;
	const frameOk =
		frame !== null &&
		frame !== undefined &&
		frame.byteLength >= VALUE_FRAME_BYTE_LENGTH;
	/*
	BigInt in region word slices breaks React DevTools (JSON.stringify on props).
	Snapshots only carry wireFrame; decode regions locally from bytes.
	*/
	const regions = useMemo(() => {
		if (!frameOk || !frame) {
			return null;
		}

		return decodeValueRegionsFromFrame(frame);
	}, [frame, frameOk]);

	const wordAt = (idx: number): string | null =>
		formatWordHexAt(regions, frame, idx);

	const propertiesW48 = wordAt(48);
	const propertiesW49 = wordAt(49);
	const propertiesW50 = wordAt(50);
	const propertiesW51 = wordAt(51);
	const propertiesW52 = wordAt(52);
	const propertiesW53 = wordAt(53);
	const propertiesW54 = wordAt(54);
	const propertiesW55 = wordAt(55);
	/*
	Properties is the extended 24-word band per cmd/cfg/config.yml. Community
	id lives at w56 (properties[8], stamped by mesh.Field routing) and firmware
	lifecycle status lives at w57 (properties[9], stamped by the rule engine
	after link → affinity → resident completes). Surfacing both inside the
	PROPERTIES region — where they actually live — matches the wire layout.
	*/
	const propertiesW56Community = wordAt(56);
	const propertiesW57Status = wordAt(57);
	const probeStatus = decodeProbeStatus(propertiesW53);
	const firmwareStatus = decodeFirmwareStatus(propertiesW57Status);
	const noiseFalsified = decodeNoiseFalsified(propertiesW52);
	const probeWindow = decodeRegionRef(propertiesW54);
	const ttlExpired =
		propertiesW51 !== null &&
		((): boolean => {
			try {
				return (BigInt(`0x${propertiesW51}`) & (1n << 63n)) !== 0n;
			} catch {
				return false;
			}
		})();
	const refuteTarget =
		propertiesW49 && propertiesW49 !== "0000000000000000"
			? propertiesW49
			: null;
	const communityIdFromFrame =
		propertiesW56Community && propertiesW56Community !== "0000000000000000"
			? BigInt(`0x${propertiesW56Community}`).toString(10)
			: null;
	const confidence = vals["confidence"] ?? null;
	const epoch = vals["epoch"] !== undefined ? Math.round(vals["epoch"]) : null;

	const affinityFromFrame =
		regions && frameOk
			? affinityHexFromRegions(regions)
			: frameOk && frame
				? affinityHexWords(frame)
				: null;
	const affinityHex = affinityFromFrame || snap.communityAffinityHex || null;

	/*
	Chain: committed ids live at w120/w121 after link; the orchestrator stages
	the same logical ids at w56/w57 until then. Prefer committed words, else
	staging, else last VisValue snapshot.
	*/
	const chain = chainPreview(regions, frame, frameOk);
	const prevWire = chain.prevCommitted || chain.prevStaged;
	const nextWire = chain.nextCommitted || chain.nextStaged;

	const prevId = prevWire || snap.prevId;
	const nextId = nextWire || snap.nextId;

	const idFromFrame = chain.idHex || null;

	const affinityIsAllZero =
		!!affinityHex && /^0+$/.test(affinityHex.replace(/\s+/g, ""));
	const programShown = effectiveProgram(snap);
	const currentFirmware = latestFirmwareStepName(snap);
	const tokenizerLabel =
		snap.label && snap.label !== snap.id && snap.label !== snap.content
			? snap.label
			: "";

	const frameRevisionKey = useMemo(() => {
		if (!frameOk || !frame) {
			return "no-frame";
		}

		let hash = 2166136261;

		for (let i = 0; i < frame.byteLength; i++) {
			hash = Math.imul(hash ^ frame[i]!, 16777619) >>> 0;
		}

		return `${frame.byteLength}:${hash}`;
	}, [frame, frameOk]);

	const tokenBandFilled =
		(frameOk &&
			Array.from({ length: 16 }, (_, wi) => wordAt(wi)).some(
				(hex) => hex && hex !== "0000000000000000",
			)) ||
		!!(snap.content || tokenizerLabel);

	const programWire = regions ? decodeProgramWire(regions.program) : null;

	return (
		<div className={cn("space-y-2", className)}>
			{/* ── Identity + belief gap ─────────────────────────────────────────── */}
			<Card>
				<CardContent className="px-3 py-2 flex items-center gap-3 flex-wrap">
					<div className="flex flex-wrap gap-1.5">
						<Badge variant="outline" className={ROLE_COLORS[snap.role] ?? ""}>
							{snap.role.toUpperCase()}
						</Badge>
						{programShown && (
							<Badge
								variant="outline"
								className="border-purple-500/30 bg-purple-500/20 text-purple-300"
							>
								{programShown}
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
						{probeStatus && (
							<Badge
								variant="outline"
								className="border-muted/30 text-muted-foreground"
							>
								probe {probeStatus}
							</Badge>
						)}
						{/*
						Causal residues are three independent booleans the wire
						carries for every Value: the hypothesis bit means a
						refutation target is staged in properties[1]; falsified
						means the kernel stamped FalsifiedBit into the noise word;
						intervening means the do_intervention rule severed causal
						history and injected a foreign gradient. Exposing all
						three as colour-coded badges lets the operator see the
						causal cascade residue at a glance without decoding hex.
						*/}
						{snap.causal.hypothesizing && (
							<Badge
								variant="outline"
								className="border-yellow-500/40 bg-yellow-500/20 text-yellow-200"
								title="Refutation target staged (properties[1] non-zero)"
							>
								HYPOTHESIS
							</Badge>
						)}
						{snap.causal.falsified && (
							<Badge
								variant="outline"
								className="border-red-500/40 bg-red-500/20 text-red-200"
								title="Falsified bit set in noise word (properties[4])"
							>
								FALSIFIED
							</Badge>
						)}
						{snap.causal.intervening && (
							<Badge
								variant="outline"
								className="border-fuchsia-500/40 bg-fuchsia-500/20 text-fuchsia-200"
								title="do_intervention severed causal history (prev=0, gradient + context non-zero)"
							>
								INTERVENING
							</Badge>
						)}
					</div>

					<div className="flex items-center gap-2 flex-wrap min-w-0">
						<p className="text-[10px] font-mono text-muted-foreground/50 break-all">
							{idFromFrame ?? formatValueWordId(snap.id)}
						</p>
						{frameOk ? (
							<span className="text-[8px] font-mono text-emerald-400/45 shrink-0">
								live frame
							</span>
						) : null}
						<span
							className={cn(
								"text-[8px] font-mono shrink-0 tabular-nums",
								frameAgeTone(nowMs, snap.frameReceivedAtMs),
							)}
							title={
								snap.frameReceivedAtMs > 0
									? new Date(snap.frameReceivedAtMs).toISOString()
									: "no frame received"
							}
						>
							{formatFrameAge(nowMs, snap.frameReceivedAtMs)}
						</span>
					</div>

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

			{/* ── Rule chain ──────────────────────────────────────────────────── */}
			{snap.role === "data" && (
				<Card>
					<CardContent className="px-3 py-2 flex items-center gap-2 flex-wrap">
						<span className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground/50">
							rule chain
						</span>
						{FIRMWARE_CHAIN_STEPS.map((step, index) => {
							const observed = stepIsObserved(snap, step);
							const current = currentFirmware === step;

							return (
								<div key={step} className="flex items-center gap-1.5">
									<span
										className={cn(
											"rounded-md border px-2 py-0.5 text-[9px] font-mono uppercase tracking-wider transition-colors",
											observed ? STEP_COLORS[step] : STEP_IDLE_CLASS,
											current ? "ring-1 ring-offset-0 ring-white/25" : "",
										)}
									>
										{step}
										{current ? (
											<span className="ml-1 text-white/60">●</span>
										) : null}
									</span>
									{index < FIRMWARE_CHAIN_STEPS.length - 1 && (
										<span className="text-muted-foreground/30 text-[9px]">
											→
										</span>
									)}
								</div>
							);
						})}
						{snap.firmwareSteps.length === 0 && (
							<span className="text-[9px] font-mono text-muted-foreground/40 ml-2">
								awaiting first ALU dispatch
							</span>
						)}
						{snap.firmwareSteps.length > 0 && (
							<span className="ml-auto text-[9px] font-mono text-muted-foreground/40">
								last{" "}
								<span className="text-foreground/60">
									{snap.firmwareSteps[snap.firmwareSteps.length - 1].name}
								</span>
								{" · "}
								<span className="text-foreground/40">
									{snap.firmwareSteps[snap.firmwareSteps.length - 1]
										.substrate || "—"}
								</span>
							</span>
						)}
					</CardContent>
				</Card>
			)}

			{/* ── 1 KB memory band (REGION_SPECS / kernel layout) ─────────────── */}
			<div
				key={frameRevisionKey}
				className="flex rounded-lg overflow-hidden border border-border/40 bg-card"
			>
				{/* TOKENS w0–w15 */}
				<Region
					label="TOKENS"
					words="0–15 · 1024b"
					fill={tokenBandFilled ? 1 : 0.05}
					headerClass="bg-sky-500/10"
					barClass="bg-sky-400"
					textClass="text-sky-200/80"
				>
					{frameOk ? (
						<WordHexRows from={0} to={15} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
					{tokenizerLabel ? (
						<KV k="lbl" v={tokenizerLabel} vClass="text-amber-200/80" />
					) : null}
				</Region>

				{/* PROGRAM 16–23 · 512 bits */}
				<Region
					label="PROGRAM"
					words="16–23 · 512b"
					fill={programShown ? 1 : 0.05}
					headerClass="bg-purple-500/10"
					barClass="bg-purple-400"
					textClass="text-purple-200/80"
				>
					{programShown ? (
						<>
							<p className="font-semibold">{programShown}</p>
							{snap.firmwareSteps.length > 1 && (
								<p className="text-[8px] text-purple-200/40 leading-tight">
									{snap.firmwareSteps
										.slice(-3)
										.map((step) => step.name)
										.join(" → ")}
								</p>
							)}
						</>
					) : (
						<Dim>unscheduled</Dim>
					)}
					{programWire && (
						<div className="mt-1.5 space-y-0.5 border-t border-purple-500/25 pt-1.5">
							<p className="text-[7px] font-mono uppercase tracking-wide text-muted-foreground/40">
								alu wire
							</p>
							<div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[8px] font-mono leading-tight">
								<KV
									k="op"
									v={`0x${programWire.opcodeLow.toString(16).padStart(2, "0")}`}
									vClass="text-cyan-300"
								/>
								<KV
									k="srcA"
									v={`${programWire.srcA.start}+${programWire.srcA.span}`}
								/>
								<KV
									k="srcB"
									v={`${programWire.srcB.start}+${programWire.srcB.span}`}
								/>
								<KV
									k="dst"
									v={`${programWire.dst.start}+${programWire.dst.span}`}
								/>
							</div>
						</div>
					)}
				</Region>

				{/* SIGNALS w24–w31 */}
				<Region
					label="SIGNALS"
					words="24–31 · 512b"
					fill={snap.actionResonance > 0 ? snap.actionResonance : 0.05}
					headerClass="bg-emerald-500/10"
					barClass="bg-emerald-400"
					textClass="text-emerald-200/80"
				>
					{frameOk ? (
						<WordHexRows from={24} to={31} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
					{snap.actionResonance > 0 ? (
						<KV k="meta.wire" v={snap.actionResonance.toFixed(5)} />
					) : null}
				</Region>

				{/* CONTEXT w32–w39 */}
				<Region
					label="CONTEXT"
					words="32–39 · 512b"
					fill={snap.telemetry?.src ? 0.6 : 0.05}
					headerClass="bg-cyan-500/10"
					barClass="bg-cyan-400"
					textClass="text-cyan-200/80"
				>
					{frameOk ? (
						<WordHexRows from={32} to={39} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
					{snap.telemetry?.src ? (
						<KV k="evt.src" v={snap.telemetry.src} />
					) : null}
					{vals["context_dot"] !== undefined ? (
						<KV k="evt.dot" v={(vals["context_dot"] as number).toFixed(5)} />
					) : null}
				</Region>

				{/* GRADIENT w40–w47 */}
				<Region
					label="GRADIENT"
					words="40–47 · 512b"
					fill={vals["gradient_norm"] !== undefined ? 0.7 : 0.05}
					headerClass="bg-violet-500/10"
					barClass="bg-violet-400"
					textClass="text-violet-200/80"
				>
					{frameOk ? (
						<WordHexRows from={40} to={47} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
					{vals["gradient_norm"] !== undefined ? (
						<KV k="evt.norm" v={(vals["gradient_norm"] as number).toFixed(5)} />
					) : null}
					{snap.telemetry?.tgt ? (
						<KV k="evt.tgt" v={snap.telemetry.tgt} />
					) : null}
				</Region>

				{/* PROPERTIES 48–63 · 1024 bits — canonical property band */}
				<Region
					label="PROPERTIES"
					words="48–63 · 1024b"
					fill={1 - snap.gap}
					headerClass="bg-amber-500/10"
					barClass="bg-amber-400"
					textClass="text-amber-200/80"
				>
					<KV k="w48 labels" v={hexOrDash(propertiesW48)} />
					<KV
						k="w49 refute"
						v={
							refuteTarget
								? formatValueWordId(refuteTarget)
								: hexOrDash(propertiesW49)
						}
						vClass={refuteTarget ? "text-red-300" : undefined}
					/>
					<KV
						k="w50 rsv"
						v={propertiesW50 ?? (epoch !== null ? String(epoch) : "—")}
					/>
					<KV
						k="w51 ttl"
						v={
							ttlExpired
								? `${hexOrDash(propertiesW51)} · expired`
								: hexOrDash(propertiesW51)
						}
						vClass={ttlExpired ? "text-red-300" : undefined}
					/>
					<KV
						k="w52 noise"
						v={
							noiseFalsified === true
								? `${hexOrDash(propertiesW52)} · falsified`
								: hexOrDash(propertiesW52)
						}
						vClass={noiseFalsified === true ? "text-red-300" : undefined}
					/>
					<KV
						k="w53 probe"
						v={
							probeStatus
								? `${hexOrDash(propertiesW53)} · ${probeStatus}`
								: hexOrDash(propertiesW53)
						}
						vClass={probeStatus === "settled" ? "text-emerald-300" : undefined}
					/>
					<KV
						k="w54 window"
						v={
							probeWindow
								? `${hexOrDash(propertiesW54)} · ${probeWindow}`
								: hexOrDash(propertiesW54)
						}
					/>
					<KV k="w55 depth" v={hexOrDash(propertiesW55)} />
					<KV
						k="w56 community"
						v={
							communityIdFromFrame
								? `${hexOrDash(propertiesW56Community)} · #${communityIdFromFrame}`
								: hexOrDash(propertiesW56Community)
						}
						vClass={communityIdFromFrame ? "text-sky-300" : undefined}
					/>
					<KV
						k="w57 status"
						v={
							firmwareStatus
								? `${hexOrDash(propertiesW57Status)} · ${firmwareStatus}`
								: hexOrDash(propertiesW57Status)
						}
						vClass={
							firmwareStatus === "ready"
								? "text-emerald-300"
								: firmwareStatus === "raw"
									? "text-amber-300"
									: undefined
						}
					/>
					<div className="mt-1 border-t border-amber-500/25 pt-1 text-[7px] uppercase tracking-wide text-amber-200/35">
						extended w58–w63
					</div>
					{frameOk ? (
						<WordHexRows from={58} to={63} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
					{confidence !== null ? (
						<KV
							k="bus.conf"
							v={(confidence as number).toFixed(4)}
							vClass={
								(confidence as number) > 0.7 ? "text-emerald-300" : undefined
							}
						/>
					) : null}
				</Region>

				{/* ASSET w64–w119 (56 words — chain staging + peer S/C/G/P + scratch) */}
				<Region
					label="ASSET"
					words="64–119 · 3584b"
					fill={0.12}
					headerClass="bg-muted/10"
					barClass="bg-muted"
					textClass="text-muted-foreground/85"
				>
					<div className="mb-1 text-[7px] uppercase tracking-wide text-muted-foreground/40">
						peer S+C+G+P staging · scratch · scheduler
					</div>
					{frameOk ? (
						<WordHexRows from={64} to={119} wordAt={wordAt} />
					) : (
						<Dim>no frame</Dim>
					)}
				</Region>

				{/* PREV 120 */}
				<Region
					label="PREV"
					words="w120 · 64b"
					fill={prevId ? 1 : 0.05}
					headerClass="bg-indigo-500/10"
					barClass="bg-indigo-400"
					textClass="text-indigo-200/70"
				>
					{prevId ? (
						<button
							type="button"
							onClick={() => onSelectId?.(prevId)}
							className={cn(
								"break-all text-[8px] text-left leading-tight w-full",
								onSelectId
									? "text-indigo-300 underline underline-offset-2 cursor-pointer hover:text-indigo-100"
									: "text-indigo-200/70",
							)}
						>
							← {formatValueWordId(prevId)}
						</button>
					) : (
						<Dim>—</Dim>
					)}
				</Region>

				{/* NEXT 121 */}
				<Region
					label="NEXT"
					words="w121 · 64b"
					fill={nextId ? 1 : 0.05}
					headerClass="bg-indigo-500/10"
					barClass="bg-indigo-400"
					textClass="text-indigo-200/70"
				>
					{nextId ? (
						<button
							type="button"
							onClick={() => onSelectId?.(nextId)}
							className={cn(
								"break-all text-[8px] text-left leading-tight w-full",
								onSelectId
									? "text-indigo-300 underline underline-offset-2 cursor-pointer hover:text-indigo-100"
									: "text-indigo-200/70",
							)}
						>
							{formatValueWordId(nextId)} →
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
					headerClass="bg-indigo-600/15"
					barClass="bg-indigo-300"
					textClass="text-indigo-100/80"
				>
					<p className="break-all text-[8px]">
						{idFromFrame ?? formatValueWordId(snap.id)}
					</p>
				</Region>

				{/* AFFINITY 123–127 · 257 bits */}
				<Region
					label="AFFINITY"
					words="123–127 · 257b"
					fill={affinityHex ? 0.8 : 0.08}
					headerClass="bg-pink-500/10"
					barClass="bg-pink-400"
					textClass="text-pink-200/70"
				>
					{affinityHex ? (
						<div className="space-y-0.5">
							<p className="break-all text-[8px]">{affinityHex}</p>
							{affinityIsAllZero ? (
								<p className="text-[7px] text-muted-foreground/40 leading-tight">
									w123–w127 are zero
								</p>
							) : null}
						</div>
					) : (
						<Dim>no field affinity</Dim>
					)}
				</Region>
			</div>

			{/* ── Wire event telemetry ─────────────────────────────────────────── */}
			{snap.telemetry &&
				(Object.keys(vals).length > 0 ||
					!!snap.telemetry.lbl ||
					!!snap.telemetry.src ||
					!!snap.telemetry.tgt) && (
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
							</div>
						</CardContent>
					</Card>
				)}
		</div>
	);
}
