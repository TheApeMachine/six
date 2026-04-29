import { shallow, useSelector } from "@tanstack/react-store";
import { useMemo, useState } from "react";
import { selectFieldValueById } from "@/lib/field-store";
import { formatInstruction } from "@/lib/instructions";
import { formatLabelsWord } from "@/lib/labels";
import {
	PROPERTY_OFFSET,
	PROPERTY_WORD,
	type PropertyName,
} from "@/lib/propertiesGenerated";
import { statusCellText, statusName } from "@/lib/status";
import { setCursorTick, timelineStore } from "@/lib/timeline-store";
import type { StoredValue } from "@/lib/value-frame";
import { decodeProgramWire } from "@/lib/valueRegions";
import { ChainStrip } from "./ChainStrip";
import { RegionHeatmap } from "./RegionHeatmap";
import { SelectionFanOut } from "./SelectionFanOut";

/** Lane index of the first property word row in the flattened 128-word frame (display label w{base+offset}). */
export const PROPERTY_WORD_BASE_OFFSET = 56;

const STATUS_WORD = PROPERTY_WORD("STATUS");
const PROPERTY_ORDER: PropertyName[] = [
	"STATUS",
	"COMMUNITY",
	"REFERENCE",
	"CONTINUATION",
	"TARGET",
	"PROGRAM_ID",
	"ROLE",
	"LABELS",
	"CONFIDENCE",
	"EPOCH",
	"TTL",
	"TEMPERATURE",
	"NOISE",
	"SURPRISAL",
	"PREV_SURPRISAL",
	"DELTA_SURPRISAL",
];

interface ValueDetailProps {
	stored: StoredValue | null;
	values: ReadonlyMap<string, StoredValue>;
}

export function ValueDetail({ stored, values }: ValueDetailProps) {
	if (!stored || !stored.decoded) {
		return (
			<div className="grid h-full place-items-center font-mono text-[11px] text-white/40">
				select a value
			</div>
		);
	}

	const words = stored.decoded.words;
	const statusCode = Number(words[STATUS_WORD] ?? 0n);
	const firmware =
		stored.classification.program || stored.classification.category;
	const instructions = decodeProgramWire(stored.decoded.regions.program);

	return (
		<div className="space-y-3 font-mono text-[11px]">
			<header className="space-y-1">
				<div className="flex items-baseline justify-between gap-2">
					<span className="truncate text-white">{stored.id}</span>
					<span
						className={`text-[10px] uppercase tracking-widest ${statusCellText(
							statusCode,
						)}`}
					>
						{statusName(statusCode)}
					</span>
				</div>
				<div className="text-white/60">
					firmware <span className="text-white/90">{firmware || "—"}</span>
				</div>
				<div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-white/50">
					<span>
						prev <LinkChip id={stored.decoded.prevId} />
					</span>
					<span>
						next <LinkChip id={stored.decoded.nextId} />
					</span>
					<span>community {stored.communityId}</span>
				</div>
			</header>

			<section>
				<SectionTitle>tick history (this value)</SectionTitle>
				<ValueHistoryStrip valueId={stored.id} />
			</section>

			<section>
				<SectionTitle>chain (sample link)</SectionTitle>
				<ChainStrip values={values} selected={stored} />
				{values.size > 0 && walkLength(values, stored) <= 1 ? (
					<div className="text-[10px] text-white/40">
						no linked siblings yet
					</div>
				) : null}
			</section>

			<section>
				<SectionTitle>selection (frame references)</SectionTitle>
				<SelectionFanOut values={values} selected={stored} />
			</section>

			<section>
				<SectionTitle>tokens (morton-decoded)</SectionTitle>
				<TokenView text={stored.tokenText} />
			</section>

			<section>
				<SectionTitle>properties</SectionTitle>
				<div className="grid grid-cols-2 gap-x-3 gap-y-0.5">
					{PROPERTY_ORDER.map((name) => {
						const word = words[PROPERTY_WORD(name)] ?? 0n;

						return (
							<div
								key={name}
								className="flex justify-between border-b border-white/5 py-0.5 text-[10px]"
							>
								<span className="text-white/50">
									w{PROPERTY_WORD_BASE_OFFSET + PROPERTY_OFFSET[name]}{" "}
									{name.toLowerCase()}
								</span>
								<span className="text-white/85">
									{formatPropertyWord(name, word)}
								</span>
							</div>
						);
					})}
				</div>
			</section>

			<section>
				<SectionTitle>program ({instructions.length} instr)</SectionTitle>
				{instructions.length === 0 ? (
					<div className="text-[10px] text-white/40">empty program region</div>
				) : (
					<ol className="space-y-0.5 text-[10px] text-white/80">
						{instructions.map((instruction, idx) => {
							const rowKey = `${idx}-${instruction.opcode}-${instruction.topology}-${instruction.aStart}-${instruction.bStart}-${instruction.dstStart}`;

							return (
								<li
									key={rowKey}
									className="flex gap-2 border-b border-white/5 py-0.5"
								>
									<span className="w-6 text-right text-white/40">{idx}</span>
									<span className="flex-1 break-all">
										{formatInstruction(instruction)}
									</span>
								</li>
							);
						})}
					</ol>
				)}
			</section>

			<section>
				<SectionTitle>region density (1024 B)</SectionTitle>
				<RegionHeatmap words={words} />
			</section>

			<section>
				<SectionTitle>affinity (257 b)</SectionTitle>
				<div className="break-all rounded border border-white/10 bg-black/30 p-2 text-[10px] text-white/70">
					{stored.affinityHex || "—"}
				</div>
			</section>
		</div>
	);
}

/*
ValueHistoryStrip plots the ticks at which the substrate touched this
specific Value. Status code is encoded as the bar's color so the eye
can spot the SELECTED→READY→BUSY→DONE transitions without expanding the
properties panel; clicking a bar pins the timeline cursor to that tick
so the rest of the dashboard rebuilds the field as it looked then.
*/
function ValueHistoryStrip({ valueId }: { valueId: string }) {
	const { events, tickCount, cursorTick } = useSelector(
		timelineStore,
		(state) => ({
			events: state.events,
			tickCount: state.tickCount,
			cursorTick: state.cursorTick,
		}),
		{ compare: shallow },
	);

	const [openTick, setOpenTick] = useState<number | null>(null);

	const trail = useMemo(() => {
		const out: Array<{
			tick: number;
			tombstone: boolean;
			statusCode: number;
		}> = [];

		for (const event of events) {
			if (event.valueId !== valueId) {
				continue;
			}

			let statusCode = 0;
			if (!event.tombstone && event.frame.byteLength >= (STATUS_WORD + 1) * 8) {
				const dv = new DataView(
					event.frame.buffer,
					event.frame.byteOffset + STATUS_WORD * 8,
					8,
				);
				statusCode = Number(dv.getBigUint64(0, true) & 0xffn);
			}

			out.push({
				tick: event.tick,
				tombstone: event.tombstone,
				statusCode,
			});
		}

		return out;
	}, [events, valueId]);

	const grouped = useMemo(() => {
		const map = new Map<
			number,
			Array<{
				tick: number;
				tombstone: boolean;
				statusCode: number;
			}>
		>();

		for (const entry of trail) {
			const list = map.get(entry.tick) ?? [];
			list.push(entry);
			map.set(entry.tick, list);
		}

		return [...map.entries()].sort((a, b) => a[0] - b[0]);
	}, [trail]);

	if (trail.length === 0 || tickCount === 0) {
		return (
			<div className="rounded border border-white/10 bg-black/30 p-2 text-[10px] text-white/40">
				no recorded changes
			</div>
		);
	}

	const head = tickCount - 1;

	return (
		<div className="rounded border border-white/10 bg-black/30 p-2">
			<div className="relative h-5 w-full">
				{grouped.map(([tick, entries]) => {
					const left = head === 0 ? 0 : (tick / head) * 100;
					const isCursor =
						cursorTick !== null ? tick === cursorTick : tick === head;
					const lastEntry = entries[entries.length - 1];
					const statusBg = lastEntry.tombstone
						? "#ef4444"
						: (STATUS_BAR_COLOR[lastEntry.statusCode] ?? "#64748b");
					const titleParts = entries.map(
						(e) =>
							`tick ${e.tick}${e.tombstone ? " · tombstone" : ` · ${statusName(e.statusCode)}`}`,
					);
					const title = titleParts.join("; ");
					const wide = entries.length > 1;

					return (
						<button
							key={tick}
							type="button"
							onClick={() => {
								setCursorTick(tick);
								setOpenTick(openTick === tick ? null : tick);
							}}
							title={title}
							aria-label={title}
							style={{
								left: `${left}%`,
								backgroundColor: statusBg,
								boxShadow: isCursor
									? "0 0 0 1px #06b6d4, 0 0 6px #06b6d4"
									: "none",
								minWidth: wide ? 10 : 4,
							}}
							className="absolute flex h-5 min-w-1 -translate-x-1/2 cursor-pointer items-center justify-center px-0.5 text-[8px] font-semibold text-white/90"
						>
							{wide ? entries.length : null}
						</button>
					);
				})}
			</div>
			{openTick !== null ? (
				<div className="mt-2 rounded border border-white/10 bg-black/50 p-2 text-[9px] text-white/70">
					<div className="mb-1 text-white/45">tick {openTick}</div>
					<ul className="space-y-0.5">
						{grouped
							.find(([t]) => t === openTick)?.[1]
							?.map((e, idx) => (
								<li key={`${e.tick}-${idx}-${e.tombstone ? "x" : "o"}`}>
									{e.tombstone ? "tombstone" : statusName(e.statusCode)}
								</li>
							))}
					</ul>
				</div>
			) : null}
			<div className="mt-1 flex justify-between text-[9px] text-white/40">
				<span>tick 0</span>
				<span>
					{trail.length} change(s) · {grouped.length} tick(s)
				</span>
				<span>tick {head}</span>
			</div>
		</div>
	);
}

const STATUS_BAR_COLOR: Record<number, string> = {
	0: "#475569", // PENDING
	1: "#10b981", // READY
	2: "#f59e0b", // BUSY
	3: "#0ea5e9", // WAITING
	4: "#06b6d4", // SELECTED
	5: "#64748b", // DONE
	6: "#8b5cf6", // RESOLVED
	7: "#ef4444", // ERROR
};

function SectionTitle({ children }: { children: React.ReactNode }) {
	return (
		<div className="mb-1 text-[9px] uppercase tracking-widest text-white/45">
			{children}
		</div>
	);
}

function LinkChip({ id }: { id: string }) {
	if (!id) {
		return <span className="text-white/30">—</span>;
	}

	return (
		<button
			type="button"
			onClick={() => selectFieldValueById(id)}
			className="text-white/80 underline-offset-2 hover:underline"
		>
			{id.slice(-8)}
		</button>
	);
}

/*
TokenView renders the morton-decoded byte stream as readable text.
Non-printables and replacement chars are surfaced as ·, so the eye can
still see "this region had 12 bytes, two were unprintable" instead of
silently swallowing them. An empty region prints "(empty)" so an
unsettled token region is visually distinct from one that decoded to
empty bytes.
*/
function TokenView({ text }: { text: string }) {
	if (!text) {
		return (
			<div className="rounded border border-white/10 bg-black/30 p-2 text-[10px] text-white/40">
				(empty)
			</div>
		);
	}

	return (
		<pre className="whitespace-pre-wrap break-all rounded border border-white/10 bg-black/30 p-2 text-[11px] text-emerald-100">
			{prettyText(text)}
		</pre>
	);
}

function prettyText(raw: string): string {
	let out = "";

	for (const char of raw) {
		const code = char.codePointAt(0) ?? 0;

		if (code === 0xfffd) {
			out += "·";
			continue;
		}

		if (code < 0x20 && code !== 0x09 && code !== 0x0a) {
			out += "·";
			continue;
		}

		out += char;
	}

	return out;
}

function walkLength(
	values: ReadonlyMap<string, StoredValue>,
	start: StoredValue,
): number {
	let count = 1;
	const seen = new Set<string>([start.id]);

	let cursor: StoredValue | undefined = start;
	while (cursor?.decoded?.prevId) {
		const prev = values.get(cursor.decoded.prevId);

		if (!prev || seen.has(prev.id)) {
			break;
		}

		seen.add(prev.id);
		count++;
		cursor = prev;
	}

	cursor = start;
	while (cursor?.decoded?.nextId) {
		const next = values.get(cursor.decoded.nextId);

		if (!next || seen.has(next.id)) {
			break;
		}

		seen.add(next.id);
		count++;
		cursor = next;
	}

	return count;
}

/*
formatPropertyWord renders a property word as the most useful narrow
text. Status gets its symbolic name; ID-shaped fields (community,
target, reference, continuation) get a short hex tail; numeric scalars
get decimal. Everything else falls back to hex.
*/
function formatPropertyWord(name: PropertyName, word: bigint): string {
	if (name === "LABELS") {
		return formatLabelsWord(word);
	}

	if (name === "STATUS") {
		return statusName(Number(word));
	}

	if (word === 0n) {
		return "0";
	}

	if (
		name === "COMMUNITY" ||
		name === "TARGET" ||
		name === "REFERENCE" ||
		name === "CONTINUATION"
	) {
		const hex = word.toString(16);
		return hex.length > 8 ? `…${hex.slice(-8)}` : hex;
	}

	if (
		name === "TTL" ||
		name === "EPOCH" ||
		name === "CONFIDENCE" ||
		name === "TEMPERATURE" ||
		name === "NOISE" ||
		name === "SURPRISAL" ||
		name === "PREV_SURPRISAL" ||
		name === "DELTA_SURPRISAL" ||
		name === "PROGRAM_ID" ||
		name === "ROLE"
	) {
		return word.toString(10);
	}

	return word.toString(16);
}
