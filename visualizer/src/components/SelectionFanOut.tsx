import { useMemo } from "react";
import { selectFieldValueById, fieldStore } from "@/lib/field-store";
import { SIGNALS_START_WORD } from "@/lib/layoutGenerated";
import { PROPERTY_WORD } from "@/lib/propertiesGenerated";
import { STATUS_BG_BY_CODE, statusName } from "@/lib/status";
import type { StoredValue } from "@/lib/value-frame";

const REFERENCE_WORD = PROPERTY_WORD("REFERENCE");
const COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const STATUS_WORD = PROPERTY_WORD("STATUS");
const SIGNALS_MATCH_SPAN = 5;

interface SelectionFanOutProps {
	values: ReadonlyMap<string, StoredValue>;
	selected: StoredValue;
}

interface Inbound {
	stored: StoredValue;
	statusCode: number;
	matchPop: number;
	claimedByTarget: boolean;
}

/*
SelectionFanOut surfaces the frame-encoded staging relation. The query
firmware stamps every peer it selects with B.properties.reference =
A.properties.reference (the recruiter's id) and writes a match witness
into B.signals[0,5]. So every Value W whose REFERENCE word equals the
selected Value's id is "staged toward" the selection — that is exactly
the lane the recruiter will sweep over.

The match popcount is the per-peer score the query wrote in phase 1
(xor of the filter pattern against B.affinity, popcount over five
words), so the same row reads as "how good a match was I". When a peer
has been claimed by the recruiter (B.community == selected.id), the
row gets a "claimed" tag — otherwise it is still in the staged-but-
unclaimed pool waiting for the recruiter pass.
*/
export function SelectionFanOut({ values, selected }: SelectionFanOutProps) {
	const targetId = selected.decoded?.words[REFERENCE_WORD] ?? 0n;
	const selfIdWord = selected.decoded?.words[PROPERTY_WORD("CONTINUATION")];

	const inbound = useMemo<Inbound[]>(() => {
		if (!selected.decoded) {
			return [];
		}

		const selectedIdWord = idWordFromHex(selected.id);
		const out: Inbound[] = [];

		for (const candidate of values.values()) {
			const ref = candidate.decoded?.words[REFERENCE_WORD] ?? 0n;

			if (ref === 0n || ref !== selectedIdWord) {
				continue;
			}

			if (candidate.id === selected.id) {
				continue;
			}

			const statusCode = Number(
				candidate.decoded?.words[STATUS_WORD] ?? 0n,
			);
			const community = candidate.decoded?.words[COMMUNITY_WORD] ?? 0n;
			const matchPop = popcountSpan(
				candidate.decoded?.words ?? [],
				SIGNALS_START_WORD,
				SIGNALS_MATCH_SPAN,
			);

			out.push({
				stored: candidate,
				statusCode,
				matchPop,
				claimedByTarget: community === selectedIdWord,
			});
		}

		out.sort((a, b) => a.matchPop - b.matchPop);

		return out;
	}, [values, selected]);

	if (targetId === 0n && inbound.length === 0) {
		return null;
	}

	const claimed = inbound.filter((row) => row.claimedByTarget).length;

	return (
		<div className="space-y-1">
			{targetId !== 0n ? (
				<div className="text-[10px] text-white/60">
					stages for{" "}
					<TargetLink id={targetId.toString(16).padStart(16, "0")} />
					{selfIdWord && selfIdWord === targetId ? (
						<span className="ml-2 text-white/35">(self-loop)</span>
					) : null}
				</div>
			) : null}

			{inbound.length > 0 ? (
				<>
					<div className="text-[10px] text-white/55">
						{inbound.length} value{inbound.length === 1 ? "" : "s"} reference
						this · <span className="text-white/80">{claimed}</span> claimed
					</div>
					<div className="max-h-48 overflow-auto rounded border border-white/10">
						<table className="w-full font-mono text-[10px]">
							<thead className="sticky top-0 bg-[#0a0a14] text-white/45">
								<tr>
									<th className="px-1.5 py-1 text-left">value</th>
									<th className="px-1.5 py-1 text-left">status</th>
									<th className="px-1.5 py-1 text-right">match</th>
									<th className="px-1.5 py-1 text-left">claim</th>
								</tr>
							</thead>
							<tbody>
								{inbound.map((row) => (
									<tr
										key={row.stored.id}
										className="border-t border-white/5 hover:bg-white/5"
									>
										<td className="px-1.5 py-0.5">
											<TargetLink id={row.stored.id} />
										</td>
										<td className="px-1.5 py-0.5">
											<StatusPill code={row.statusCode} />
										</td>
										<td className="px-1.5 py-0.5 text-right text-white/75">
											{row.matchPop}
										</td>
										<td className="px-1.5 py-0.5 text-white/75">
											{row.claimedByTarget ? "✓" : "—"}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</>
			) : null}
		</div>
	);
}

function StatusPill({ code }: { code: number }) {
	const bg = STATUS_BG_BY_CODE[code] ?? "bg-slate-700";

	return (
		<span className="inline-flex items-center gap-1 text-white/70">
			<span className={`inline-block h-1.5 w-1.5 rounded-full ${bg}`} />
			<span>{statusName(code).toLowerCase()}</span>
		</span>
	);
}

function TargetLink({ id }: { id: string }) {
	const known = fieldStore.get().values.has(id);

	if (!known) {
		return <span className="text-white/40">{id.slice(-8)}</span>;
	}

	return (
		<button
			type="button"
			onClick={() => selectFieldValueById(id)}
			className="text-emerald-200 underline-offset-2 hover:underline"
		>
			{id.slice(-8)}
		</button>
	);
}

function idWordFromHex(id: string): bigint {
	if (!id) {
		return 0n;
	}

	try {
		return BigInt(`0x${id}`);
	} catch {
		return 0n;
	}
}

function popcountSpan(
	words: readonly bigint[],
	start: number,
	span: number,
): number {
	let total = 0;

	for (let idx = 0; idx < span; idx++) {
		const word = words[start + idx];
		if (word === undefined) {
			continue;
		}

		let value = word;
		while (value !== 0n) {
			value &= value - 1n;
			total++;
		}
	}

	return total;
}
