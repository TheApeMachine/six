/*
Status enum mirror — pkg/primitive/properties.go:StatusType. Word 61
(properties[5]) carries the live status. Tailwind class strings are
co-located so the dashboard does not have to rebuild the color map at
every render. STATUSES is the single editable list — maps below derive
from it so keys cannot drift apart.
*/

const STATUSES = [
	{ code: 0, name: "PENDING" as const, bg: "bg-slate-700", text: "text-slate-300" },
	{ code: 1, name: "READY" as const, bg: "bg-emerald-500", text: "text-emerald-300" },
	{ code: 2, name: "BUSY" as const, bg: "bg-amber-500", text: "text-amber-300" },
	{ code: 3, name: "WAITING" as const, bg: "bg-sky-500", text: "text-sky-300" },
	{ code: 4, name: "DONE" as const, bg: "bg-slate-500", text: "text-slate-300" },
	{ code: 5, name: "RESOLVED" as const, bg: "bg-violet-500", text: "text-violet-300" },
	{ code: 6, name: "ERROR" as const, bg: "bg-red-500", text: "text-red-300" },
] as const;

export type StatusName = (typeof STATUSES)[number]["name"];

export const STATUS_NAME_BY_CODE: Record<number, StatusName> =
	Object.fromEntries(STATUSES.map((row) => [row.code, row.name])) as Record<
		number,
		StatusName
	>;

export const STATUS_BG_BY_CODE: Record<number, string> = Object.fromEntries(
	STATUSES.map((row) => [row.code, row.bg]),
);

export const STATUS_TEXT_BY_CODE: Record<number, string> = Object.fromEntries(
	STATUSES.map((row) => [row.code, row.text]),
);

export function statusName(code: number): string {
	return STATUS_NAME_BY_CODE[code] ?? `S${code}`;
}
