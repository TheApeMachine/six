import { useMemo, useState } from "react";
import {
	type ProgramCategory,
	PROGRAM_CATEGORIES,
} from "@/lib/programClassifier";
import { PROPERTY_WORD, VALUE_ROLE } from "@/lib/propertiesGenerated";
import { type ColorMode, colorForCommunity } from "@/lib/scene-mapping";
import { statusName } from "@/lib/status";
import type { StoredValue } from "@/lib/value-frame";

interface LegendEntry {
	key: string;
	swatch: string;
	label: string;
	count: number;
	muted?: boolean;
}

interface LegendProps {
	values: ReadonlyMap<string, StoredValue>;
	colorMode: ColorMode;
}

/*
Legend is the in-scene cheat sheet. It mirrors the active color mode
so the user does not have to memorise palettes: each row is a swatch,
a label, and the live count of Values currently in that bucket. The
firmware mode brings back the operator-facing program-activity panel
that used to live in the previous UI; status, role, and community
modes use the same row format with mode-appropriate buckets.

Positioned in the top-left of the scene, collapsible to a single
header row so it doesn't fight the live counter or steal screen
space. Counts sit on the right so the eye scans label → count
without zigzagging.
*/
export function Legend({ values, colorMode }: LegendProps) {
	const [collapsed, setCollapsed] = useState(false);

	const entries = useMemo(() => {
		switch (colorMode) {
			case "status":
				return statusEntries(values);
			case "role":
				return roleEntries(values);
			case "community":
				return communityEntries(values);
			case "firmware":
				return firmwareEntries(values);
			default:
				return [];
		}
	}, [values, colorMode]);

	const headerLabel = useMemo(() => HEADER_LABEL[colorMode], [colorMode]);

	return (
		<div className="pointer-events-auto w-[260px] rounded border border-white/10 bg-black/55 font-mono text-[11px] text-white/80 backdrop-blur-sm">
			<button
				type="button"
				onClick={() => setCollapsed((value) => !value)}
				className="flex w-full items-center justify-between px-2 py-1.5 text-[10px] uppercase tracking-widest text-white/60 hover:text-white/90"
			>
				<span>{headerLabel}</span>
				<span className="text-white/40">{collapsed ? "+" : "−"}</span>
			</button>
			{collapsed ? null : (
				<ul className="max-h-[55vh] divide-y divide-white/5 overflow-auto px-2 pb-2">
					{entries.length === 0 ? (
						<li className="py-1.5 text-[10px] text-white/40">no buckets</li>
					) : (
						entries.map((entry) => (
							<li
								key={entry.key}
								className={`flex items-center gap-2 py-1 ${
									entry.muted ? "text-white/40" : ""
								}`}
							>
								<span
									className="h-3 w-3 shrink-0 rounded-sm"
									style={{ backgroundColor: entry.swatch }}
								/>
								<span className="truncate">{entry.label}</span>
								<span className="ml-auto tabular-nums text-white/70">
									{entry.count}
								</span>
							</li>
						))
					)}
				</ul>
			)}
		</div>
	);
}

const HEADER_LABEL: Record<ColorMode, string> = {
	status: "status",
	role: "role",
	community: "communities",
	firmware: "program activity",
};

const STATUS_SWATCH: Record<number, string> = {
	0: "#94a3b8",
	1: "#10b981",
	2: "#f59e0b",
	3: "#38bdf8",
	4: "#22d3ee",
	5: "#a3b3c4",
	6: "#a78bfa",
	7: "#f87171",
};

const ROLE_SWATCH: Record<number, string> = {
	0: "#9ca3af",
	1: "#fb923c",
	2: "#2dd4bf",
	3: "#fde047",
	4: "#c084fc",
	[VALUE_ROLE.Prompt]: "#fff03a",
};

const ROLE_LABEL: Record<number, string> = {
	0: "none",
	1: "programmer",
	2: "learner",
	3: "readout",
	4: "association",
	[VALUE_ROLE.Prompt]: "prompt",
};

function statusEntries(values: ReadonlyMap<string, StoredValue>): LegendEntry[] {
	const counts = new Map<number, number>();
	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}
		const code = Number(stored.decoded.words[STATUS_WORD_INDEX] ?? 0n);
		counts.set(code, (counts.get(code) ?? 0) + 1);
	}

	const entries: LegendEntry[] = [];
	for (let code = 0; code <= 7; code++) {
		const count = counts.get(code) ?? 0;
		entries.push({
			key: `status-${code}`,
			swatch: STATUS_SWATCH[code] ?? "#94a3b8",
			label: statusName(code).toLowerCase(),
			count,
			muted: count === 0,
		});
	}
	return entries;
}

function roleEntries(values: ReadonlyMap<string, StoredValue>): LegendEntry[] {
	const counts = new Map<number, number>();
	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}
		const role = Number(stored.decoded.words[ROLE_WORD_INDEX] ?? 0n);
		counts.set(role, (counts.get(role) ?? 0) + 1);
	}

	const order = [0, 1, 2, 3, 4, VALUE_ROLE.Prompt];
	return order.map((role) => {
		const count = counts.get(role) ?? 0;
		return {
			key: `role-${role}`,
			swatch: ROLE_SWATCH[role] ?? "#9ca3af",
			label: ROLE_LABEL[role] ?? `role ${role}`,
			count,
			muted: count === 0,
		};
	});
}

function communityEntries(
	values: ReadonlyMap<string, StoredValue>,
): LegendEntry[] {
	const counts = new Map<number, number>();
	let orphanCount = 0;
	for (const stored of values.values()) {
		if (stored.communityId < 0) {
			orphanCount++;
			continue;
		}
		counts.set(
			stored.communityId,
			(counts.get(stored.communityId) ?? 0) + 1,
		);
	}

	const sorted = [...counts.entries()].sort((a, b) => b[1] - a[1]);
	const top = sorted.slice(0, 12);

	const entries: LegendEntry[] = top.map(([id, count]) => {
		const color = colorForCommunity(id);
		return {
			key: `community-${id}`,
			swatch: `#${color.getHexString()}`,
			label: `c${id.toString(16).slice(-6)}`,
			count,
		};
	});

	if (sorted.length > top.length) {
		const rest = sorted.slice(top.length).reduce((sum, [, c]) => sum + c, 0);
		entries.push({
			key: "community-rest",
			swatch: "#374151",
			label: `+${sorted.length - top.length} more`,
			count: rest,
			muted: true,
		});
	}

	if (orphanCount > 0) {
		entries.push({
			key: "community-orphans",
			swatch: "#6b7280",
			label: "orphans",
			count: orphanCount,
			muted: true,
		});
	}

	return entries;
}

function firmwareEntries(
	values: ReadonlyMap<string, StoredValue>,
): LegendEntry[] {
	const counts = new Map<ProgramCategory, number>();
	for (const stored of values.values()) {
		const cat = stored.classification.category;
		counts.set(cat, (counts.get(cat) ?? 0) + 1);
	}

	const order: ProgramCategory[] = [
		"query",
		"structural",
		"beam",
		"classify",
		"peer_gap",
		"consensus",
		"gap_probe",
		"intervene",
		"inference",
		"resident",
		"recruiter",
		"plumbing",
		"util",
		"unknown",
	];

	return order.map((cat) => {
		const style = PROGRAM_CATEGORIES[cat];
		const count = counts.get(cat) ?? 0;
		const [r, g, b] = style.color;
		return {
			key: `firmware-${cat}`,
			swatch: `rgb(${r}, ${g}, ${b})`,
			label: style.label,
			count,
			muted: count === 0,
		};
	});
}

const STATUS_WORD_INDEX = PROPERTY_WORD("STATUS");
const ROLE_WORD_INDEX = PROPERTY_WORD("ROLE");
