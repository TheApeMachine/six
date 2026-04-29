import { memo, useMemo } from "react";
import { selectFieldValueById } from "@/lib/field-store";
import { PROPERTY_WORD } from "@/lib/propertiesGenerated";
import { statusCellBackground, statusName } from "@/lib/status";
import type { StoredValue } from "@/lib/value-frame";

const STATUS_WORD = PROPERTY_WORD("STATUS");

interface ValueGridProps {
	values: ReadonlyMap<string, StoredValue>;
	selectedId: string | null;
}

interface CommunityBucket {
	id: number;
	label: string;
	members: StoredValue[];
}

/*
ValueGrid renders every Value as a small cell, grouped by communityId
(orphans first). Cell color tracks the live status word; the firmware
short-name labels what the Value is doing. Click selects.
*/
export function ValueGrid({ values, selectedId }: ValueGridProps) {
	const buckets = useMemo<CommunityBucket[]>(() => {
		const grouped = new Map<number, StoredValue[]>();

		for (const stored of values.values()) {
			const list = grouped.get(stored.communityId);

			if (list) {
				list.push(stored);
			} else {
				grouped.set(stored.communityId, [stored]);
			}
		}

		const out: CommunityBucket[] = [];
		const orphan = grouped.get(-1);

		if (orphan) {
			out.push({ id: -1, label: "orphan", members: orphan });
			grouped.delete(-1);
		}

		for (const [id, members] of Array.from(grouped.entries()).sort(
			(a, b) => a[0] - b[0],
		)) {
			out.push({ id, label: `c${id}`, members });
		}

		return out;
	}, [values]);

	if (buckets.length === 0) {
		return (
			<div className="grid h-full place-items-center font-mono text-[11px] text-white/40">
				waiting for telemetry…
			</div>
		);
	}

	return (
		<div className="space-y-4">
			{buckets.map((bucket) => (
				<section key={bucket.id}>
					<header className="mb-1 flex items-baseline justify-between font-mono text-[10px] uppercase tracking-widest text-white/50">
						<span>{bucket.label}</span>
						<span>{bucket.members.length}</span>
					</header>
					<div className="grid grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-1.5">
						{bucket.members.map((stored) => (
							<ValueCellMemo
								key={stored.id}
								stored={stored}
								selected={stored.id === selectedId}
							/>
						))}
					</div>
				</section>
			))}
		</div>
	);
}

function ValueCell(props: { stored: StoredValue; selected: boolean }) {
	const { stored, selected } = props;
	const statusCode = Number(stored.decoded?.words[STATUS_WORD] ?? 0n);
	const bg = statusCellBackground(statusCode);
	const firmware =
		stored.classification.program || stored.classification.category;

	return (
		<button
			type="button"
			aria-pressed={selected}
			onClick={() => selectFieldValueById(stored.id)}
			className={`group rounded border px-1.5 py-1 text-left font-mono text-[10px] leading-tight transition-colors ${
				selected
					? "border-white/80 bg-white/10"
					: "border-white/5 hover:border-white/20"
			}`}
			title={`${stored.id} · ${statusName(statusCode)}`}
		>
			<div className="flex items-center gap-1">
				<span className={`inline-block h-1.5 w-1.5 rounded-full ${bg}`} />
				<span className="truncate text-white/85">{firmware || "—"}</span>
			</div>
			<div className="truncate text-[9px] text-white/40">
				{stored.id.slice(-8)}
			</div>
		</button>
	);
}

function valueCellPropsEqual(
	prev: { stored: StoredValue; selected: boolean },
	next: { stored: StoredValue; selected: boolean },
): boolean {
	return prev.selected === next.selected && prev.stored === next.stored;
}

const ValueCellMemo = memo(ValueCell, valueCellPropsEqual);
