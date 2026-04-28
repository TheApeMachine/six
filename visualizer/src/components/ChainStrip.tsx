import { selectFieldValueById } from "@/lib/field-store";
import type { StoredValue } from "@/lib/value-frame";

interface ChainStripProps {
	values: ReadonlyMap<string, StoredValue>;
	selected: StoredValue;
}

/*
ChainStrip walks Prev to the chain head and Next to the chain tail
starting from the selected Value. The tokenizer wires every segment of
one sample together via Prev/Next, so this surfaces "everything that
came from the same input" as a clickable row. A visited set guards
against accidental cycles in malformed telemetry.
*/
export function ChainStrip({ values, selected }: ChainStripProps) {
	const chain = walkChain(values, selected);

	if (chain.length <= 1) {
		return null;
	}

	return (
		<div className="flex flex-wrap items-center gap-1">
			{chain.map((stored) => {
				const isSelected = stored.id === selected.id;
				const text = stored.tokenText || stored.id.slice(-8);

				return (
					<button
						key={stored.id}
						type="button"
						onClick={() => selectFieldValueById(stored.id)}
						className={`max-w-[18ch] truncate rounded px-1.5 py-0.5 text-[10px] ${
							isSelected
								? "bg-emerald-500/30 text-emerald-100"
								: "bg-white/5 text-white/70 hover:bg-white/10"
						}`}
						title={`${stored.id}${stored.tokenText ? ` · "${stored.tokenText}"` : ""}`}
					>
						{text}
					</button>
				);
			})}
		</div>
	);
}

function walkChain(
	values: ReadonlyMap<string, StoredValue>,
	start: StoredValue,
): StoredValue[] {
	const seen = new Set<string>([start.id]);
	const before: StoredValue[] = [];
	const after: StoredValue[] = [];

	let cursor: StoredValue | undefined = start;
	while (cursor?.decoded?.prevId) {
		const prev = values.get(cursor.decoded.prevId);

		if (!prev || seen.has(prev.id)) {
			break;
		}

		seen.add(prev.id);
		before.unshift(prev);
		cursor = prev;
	}

	cursor = start;
	while (cursor?.decoded?.nextId) {
		const next = values.get(cursor.decoded.nextId);

		if (!next || seen.has(next.id)) {
			break;
		}

		seen.add(next.id);
		after.push(next);
		cursor = next;
	}

	return [...before, start, ...after];
}
