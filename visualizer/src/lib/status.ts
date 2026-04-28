/*
Status enum mirror — pkg/primitive/properties.go:StatusType. Word 61
(properties[5]) carries the live status. Tailwind class strings are
co-located so the dashboard does not have to rebuild the color map at
every render.
*/
export type StatusName =
	| "PENDING"
	| "READY"
	| "BUSY"
	| "WAITING"
	| "DONE"
	| "RESOLVED"
	| "ERROR";

export const STATUS_NAME_BY_CODE: Record<number, StatusName> = {
	0: "PENDING",
	1: "READY",
	2: "BUSY",
	3: "WAITING",
	4: "DONE",
	5: "RESOLVED",
	6: "ERROR",
};

export const STATUS_BG_BY_CODE: Record<number, string> = {
	0: "bg-slate-700",
	1: "bg-emerald-500",
	2: "bg-amber-500",
	3: "bg-sky-500",
	4: "bg-slate-500",
	5: "bg-violet-500",
	6: "bg-red-500",
};

export const STATUS_TEXT_BY_CODE: Record<number, string> = {
	0: "text-slate-300",
	1: "text-emerald-300",
	2: "text-amber-300",
	3: "text-sky-300",
	4: "text-slate-300",
	5: "text-violet-300",
	6: "text-red-300",
};

export function statusName(code: number): string {
	return STATUS_NAME_BY_CODE[code] ?? `S${code}`;
}
