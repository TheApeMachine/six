import { useMemo } from "react";
import type { StoredValue } from "@/lib/value-frame";

interface TopBarProps {
	values: ReadonlyMap<string, StoredValue>;
	connectionError: string | null;
	selectedId: string | null;
}

/*
TopBar is the only persistent chrome in the dashboard. It reports
connection state and three counters (total Values, distinct communities,
orphans), plus the currently selected ID. No tabs, no nav — there is
exactly one screen.
*/
export function TopBar({ values, connectionError, selectedId }: TopBarProps) {
	const { orphanCount, communityCount } = useMemo(() => {
		let orphans = 0;
		const communities = new Set<number>();

		for (const stored of values.values()) {
			if (stored.communityId < 0) {
				orphans++;
				continue;
			}

			communities.add(stored.communityId);
		}

		return { orphanCount: orphans, communityCount: communities.size };
	}, [values]);

	return (
		<div className="flex items-center gap-4 border-b border-white/10 bg-[#0a0a14] px-4 py-2 font-mono text-[11px] text-white/80">
			<span className="flex items-center gap-2">
				<span
					className={
						connectionError
							? "h-2 w-2 rounded-full bg-red-400"
							: "h-2 w-2 rounded-full bg-emerald-400"
					}
				/>
				<span className="font-semibold tracking-widest text-white/90">SIX</span>
				<span className="text-white/50">{connectionError ?? "connected"}</span>
			</span>
			<span className="text-white/40">·</span>
			<span>
				values <span className="text-white">{values.size}</span>
			</span>
			<span>
				communities <span className="text-white">{communityCount}</span>
			</span>
			<span>
				orphans <span className="text-white">{orphanCount}</span>
			</span>
			<span className="ml-auto text-white/40">
				{selectedId ? `selected ${selectedId.slice(-8)}` : "no selection"}
			</span>
		</div>
	);
}
