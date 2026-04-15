import { decodeValueFrame, ValueStore } from "@/lib/value-store";
import { type DecodedFrame, EK, type VizEvent } from "@/lib/wire";
import type {
	FieldSnapshot,
	FieldValueSnapshot,
	TelemetryPayloadSnapshot,
	ValueRole,
	VizGraphSnapshot,
	VizInspectSnapshot,
	VizRuntimeStats,
} from "./types";

const MAX_EVENTS = 500;
const MAX_HISTORY = 240;
interface TelemetryValue {
	id: string;
	role: ValueRole;
	program: string;
	communityId: number;
	label: string;
	content: string;
	resonance: number;
	gap: number;
	resolved: boolean;
	actionResonance: number;
	prevId: string;
	nextId: string;
	communityAffinityHex: string;
	wireFrame: Uint8Array | null;
	telemetry: TelemetryPayloadSnapshot | null;
	pos: { x: number; y: number };
}

interface TelemetryCommunity {
	id: number;
	saturated: boolean;
	saturation: number;
	lastAction: string;
	actionCount: number;
	reactionCount: number;
	affinityHex: string;
	concentration: number;
	memberIds: Set<string>;
}

interface TelemetryGraphSnapshot {
	id: number;
	affinity_hex?: string;
	member_ids?: string[];
}

export interface TelemetryState {
	events: VizEvent[];
	selection: VizInspectSnapshot | null;
	selectedId: string | null;
	snapshot: VizGraphSnapshot | null;
	snapshotHistory: VizGraphSnapshot[];
	stats: VizRuntimeStats;
}

export interface TelemetryStore {
	applyFrames: (frames: DecodedFrame[]) => TelemetryState;
	getState: () => TelemetryState;
	selectValueById: (id: string) => TelemetryState;
}

function emptyStats(): VizRuntimeStats {
	return {
		values: 0,
		communities: 0,
		actions: 0,
		reactions: 0,
		dropped: 0,
		bootstrapNodes: 0,
		wireJsonBlobs: 0,
	};
}

function createValuePosition(
	id: string,
	role: ValueRole,
): { x: number; y: number } {
	let seed = 0;

	for (let index = 0; index < id.length; index++) {
		seed = (seed * 33 + id.charCodeAt(index)) >>> 0;
	}

	const ring = role === "prompt" ? 120 : 220 + (seed % 7) * 40;
	const angle = (seed % 360) * (Math.PI / 180);

	return {
		x: Math.cos(angle) * ring,
		y: Math.sin(angle) * ring,
	};
}

function clonePayload(
	payload: TelemetryPayloadSnapshot | null,
): TelemetryPayloadSnapshot | null {
	if (!payload) {
		return null;
	}

	return {
		lbl: payload.lbl,
		src: payload.src,
		tgt: payload.tgt,
		ts: payload.ts,
		meta: { ...payload.meta },
		vals: { ...payload.vals },
	};
}

function snapshotValue(value: TelemetryValue): FieldValueSnapshot {
	return {
		id: value.id,
		role: value.role,
		program: value.program,
		communityId: value.communityId,
		label: value.label,
		content: value.content,
		resonance: value.resonance,
		gap: value.gap,
		resolved: value.resolved,
		actionResonance: value.actionResonance,
		prevId: value.prevId,
		nextId: value.nextId,
		communityAffinityHex: value.communityAffinityHex,
		wireFrame: value.wireFrame ? new Uint8Array(value.wireFrame) : null,
		telemetry: clonePayload(value.telemetry),
	};
}

function inspectValue(value: TelemetryValue): VizInspectSnapshot {
	return {
		...snapshotValue(value),
		pos: { ...value.pos },
	};
}

function ensureCommunity(
	communities: Map<number, TelemetryCommunity>,
	id: number,
): TelemetryCommunity {
	let community = communities.get(id);

	if (!community) {
		community = {
			id,
			saturated: false,
			saturation: 0,
			lastAction: "",
			actionCount: 0,
			reactionCount: 0,
			affinityHex: "",
			concentration: 0,
			memberIds: new Set<string>(),
		};
		communities.set(id, community);
	}

	return community;
}

function ensureValue(
	values: Map<string, TelemetryValue>,
	valueStore: ValueStore,
	id: string,
	role: ValueRole,
): TelemetryValue {
	let value = values.get(id);

	if (!value) {
		value = {
			id,
			role,
			program: role === "data" ? "affinity" : "",
			communityId: -1,
			label: "",
			content: "",
			resonance: role === "data" ? 0.6 : 0.8,
			gap: 1,
			resolved: false,
			actionResonance: 0,
			prevId: "",
			nextId: "",
			communityAffinityHex: "",
			wireFrame: null,
			telemetry: null,
			pos: createValuePosition(id, role),
		};
		values.set(id, value);
	}

	value.role = role;
	valueStore.ensure(id, { role });

	return value;
}

function mergeTelemetry(value: TelemetryValue, event: VizEvent) {
	const previous = value.telemetry ?? {
		lbl: "",
		src: "",
		tgt: "",
		ts: 0,
		meta: {},
		vals: {},
	};

	value.telemetry = {
		lbl: event.lbl,
		meta: { ...previous.meta, ...event.meta },
		src: event.src,
		tgt: event.tgt,
		ts: event.ts,
		vals: { ...previous.vals, ...event.vals },
	};
}

function syncFromFrame(
	value: TelemetryValue,
	valueStore: ValueStore,
	id: string,
) {
	const stored = valueStore.ensure(id, { role: value.role });
	const decoded = stored.decoded;

	if (!decoded) {
		return;
	}

	if (decoded.content) {
		value.content = decoded.content;
	}

	if (decoded.prevId) {
		value.prevId = decoded.prevId;
	}

	if (decoded.nextId) {
		value.nextId = decoded.nextId;
	}

	value.wireFrame = decoded.frame;
}

function buildSnapshot(
	values: Map<string, TelemetryValue>,
	communities: Map<number, TelemetryCommunity>,
): VizGraphSnapshot {
	const fields: FieldSnapshot[] = Array.from(communities.values())
		.sort((left, right) => left.id - right.id)
		.map((community) => {
			const members = Array.from(community.memberIds)
				.map((id) => values.get(id))
				.filter((value): value is TelemetryValue => Boolean(value))
				.map(snapshotValue);

			return {
				id: community.id,
				memberCount: members.length,
				saturated: community.saturated,
				saturation: community.saturation,
				lastAction: community.lastAction,
				actionCount: community.actionCount,
				reactionCount: community.reactionCount,
				affinityHex: community.affinityHex,
				concentration: community.concentration,
				members,
			};
		});

	const orphanValues = Array.from(values.values())
		.filter((value) => value.communityId < 0)
		.map(snapshotValue);

	return {
		timestamp: Date.now(),
		fields,
		orphanValues,
		totalValues: values.size,
		totalCommunities: fields.length,
	};
}

function formatBigIntId(value: bigint): string {
	if (value === 0n) {
		return "";
	}

	return value.toString(16).padStart(16, "0").toLowerCase();
}

function removeValueFromCommunities(
	communities: Map<number, TelemetryCommunity>,
	id: string,
) {
	for (const community of communities.values()) {
		community.memberIds.delete(id);
	}
}

function applyGraphSnapshot(
	values: Map<string, TelemetryValue>,
	communities: Map<number, TelemetryCommunity>,
	valueStore: ValueStore,
	event: VizEvent,
) {
	const payload = event.meta?.communities;
	if (!payload) {
		return;
	}

	let snapshots: TelemetryGraphSnapshot[];

	try {
		snapshots = JSON.parse(payload) as TelemetryGraphSnapshot[];
	} catch {
		return;
	}

	for (const community of communities.values()) {
		community.memberIds.clear();
	}

	for (const value of values.values()) {
		if (value.role === "action" || value.role === "reaction") {
			continue;
		}

		value.communityId = -1;
		value.communityAffinityHex = "";
	}

	for (const snapshot of snapshots) {
		if (typeof snapshot?.id !== "number" || snapshot.id < 0) {
			continue;
		}

		const community = ensureCommunity(communities, snapshot.id);
		community.affinityHex = snapshot.affinity_hex || community.affinityHex;
		community.memberIds.clear();

		for (const memberID of snapshot.member_ids ?? []) {
			if (!memberID) {
				continue;
			}

			const value = ensureValue(
				values,
				valueStore,
				memberID,
				values.get(memberID)?.role || "data",
			);

			value.communityId = snapshot.id;
			value.communityAffinityHex = community.affinityHex;
			community.memberIds.add(memberID);
		}
	}
}

function applyEvent(
	values: Map<string, TelemetryValue>,
	communities: Map<number, TelemetryCommunity>,
	valueStore: ValueStore,
	stats: VizRuntimeStats,
	event: VizEvent,
) {
	if (event.kind === EK.TokenizerEmit) {
		const id = event.meta?.value_id || "";
		if (!id) return;

		const value = ensureValue(values, valueStore, id, "data");
		value.label = event.lbl || value.label;
		value.content = event.meta?.content || value.content;
		value.program = event.meta?.program || value.program || "affinity";
		mergeTelemetry(value, event);
		syncFromFrame(value, valueStore, id);
		return;
	}

	if (event.kind === EK.QueueSubmit) {
		const id = event.meta?.value_id || "";
		if (!id) return;

		const value = ensureValue(values, valueStore, id, "data");
		value.label = event.lbl || value.label;
		value.content = value.content || event.lbl || event.meta?.content || "";
		value.program = event.meta?.program || value.program || "affinity";
		value.prevId = event.meta?.prev_id || value.prevId;
		value.nextId = event.meta?.next_id || value.nextId;
		mergeTelemetry(value, event);
		syncFromFrame(value, valueStore, id);
		return;
	}

	if (event.kind === EK.Prompt) {
		const id = `prompt_${event.ts.toString(16)}`;
		const value = ensureValue(values, valueStore, id, "prompt");
		value.label = "prompt";
		value.content = event.meta?.prompt || "";
		value.program = "prompt";
		value.gap = 0;
		value.resonance = 1;
		value.actionResonance = 1;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.PromptResult) {
		const id = `prompt_result_${event.ts.toString(16)}`;
		const value = ensureValue(values, valueStore, id, "prompt");
		value.label = "result";
		value.content = event.meta?.generation || "";
		value.program = "result";
		value.gap = 0;
		value.resolved = true;
		value.resonance = 1;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.CommunityCreated) {
		const communityId = event.vals?.community_id ?? -1;
		if (communityId < 0) return;

		const community = ensureCommunity(communities, communityId);
		community.affinityHex =
			event.meta?.initial_affinity || community.affinityHex;
		return;
	}

	if (event.kind === EK.ValueJoinedCommunity) {
		const id = event.meta?.value_id || "";
		const communityId = event.vals?.community_id ?? -1;
		if (!id || communityId < 0) return;

		const value = ensureValue(
			values,
			valueStore,
			id,
			values.get(id)?.role || "data",
		);
		const community = ensureCommunity(communities, communityId);
		removeValueFromCommunities(communities, id);
		value.communityId = communityId;
		value.communityAffinityHex = community.affinityHex;
		community.memberIds.add(id);
		mergeTelemetry(value, event);
		syncFromFrame(value, valueStore, id);
		return;
	}

	if (event.kind === EK.CommunityAction) {
		const id = event.meta?.action_id || "";
		const communityId = event.vals?.community_id ?? -1;
		if (!id || communityId < 0) return;

		const value = ensureValue(values, valueStore, id, "action");
		const community = ensureCommunity(communities, communityId);
		value.program = event.lbl || value.program;
		value.communityId = communityId;
		value.actionResonance = event.vals?.resonance ?? value.actionResonance;
		value.resonance = 1;
		community.memberIds.add(id);
		community.actionCount += 1;
		community.lastAction = value.program;
		stats.actions += 1;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.CommunityReaction) {
		const id = event.meta?.reaction_id || "";
		const communityId = event.vals?.community_id ?? -1;
		if (!id || communityId < 0) return;

		const value = ensureValue(values, valueStore, id, "reaction");
		const community = ensureCommunity(communities, communityId);
		value.program = event.lbl || value.program;
		value.communityId = communityId;
		value.resonance = 1;
		community.memberIds.add(id);
		community.reactionCount += 1;
		stats.reactions += 1;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.BeliefGapEvaluated) {
		const id = event.meta?.value_id || "";
		if (!id) return;

		const value = ensureValue(
			values,
			valueStore,
			id,
			values.get(id)?.role || "data",
		);
		value.gap = event.vals?.gap ?? value.gap;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.ValueResolved) {
		const id = event.meta?.value_id || "";
		if (!id) return;

		const value = ensureValue(
			values,
			valueStore,
			id,
			values.get(id)?.role || "data",
		);
		value.gap = event.vals?.gap ?? value.gap;
		value.resolved = true;
		value.resonance = 1;
		mergeTelemetry(value, event);
		return;
	}

	if (event.kind === EK.CommunityEmission) {
		const communityId = event.vals?.community_id ?? -1;
		if (communityId < 0) return;

		const community = ensureCommunity(communities, communityId);
		community.concentration =
			event.vals?.concentration ?? community.concentration;
		return;
	}

	if (event.kind === EK.TrieGraphSnapshot) {
		applyGraphSnapshot(values, communities, valueStore, event);
		return;
	}

	const valueId =
		event.meta?.value_id ||
		event.meta?.action_id ||
		event.meta?.reaction_id ||
		"";

	if (!valueId) {
		return;
	}

	const value = ensureValue(
		values,
		valueStore,
		valueId,
		values.get(valueId)?.role || "data",
	);
	mergeTelemetry(value, event);
}

export function createTelemetryStore(): TelemetryStore {
	const values = new Map<string, TelemetryValue>();
	const communities = new Map<number, TelemetryCommunity>();
	const valueStore = new ValueStore();

	let state: TelemetryState = {
		events: [],
		selection: null,
		selectedId: null,
		snapshot: null,
		snapshotHistory: [],
		stats: emptyStats(),
	};

	function rebuildState() {
		const snapshot = buildSnapshot(values, communities);
		const selected =
			state.selectedId && values.has(state.selectedId)
				? inspectValue(values.get(state.selectedId) as TelemetryValue)
				: null;

		state = {
			...state,
			selection: selected,
			snapshot,
			snapshotHistory:
				snapshot.totalValues === 0 && snapshot.totalCommunities === 0
					? state.snapshotHistory
					: [...state.snapshotHistory, snapshot].slice(-MAX_HISTORY),
			stats: {
				...state.stats,
				values: values.size,
				communities: communities.size,
			},
		};

		return state;
	}

	return {
		applyFrames(frames: DecodedFrame[]) {
			const nextStats = { ...state.stats };
			let nextEvents = state.events.slice();

			for (const frame of frames) {
				if (frame.frameType === "event") {
					nextEvents = [frame.event, ...nextEvents].slice(0, MAX_EVENTS);
					applyEvent(values, communities, valueStore, nextStats, frame.event);
					continue;
				}

				if (frame.frameType === "stats") {
					nextStats.dropped = frame.dropped;
					continue;
				}

				if (frame.frameType === "bootstrap") {
					nextStats.bootstrapNodes = frame.nodes.length;
					continue;
				}

				if (frame.frameType === "json") {
					nextStats.wireJsonBlobs += 1;
					continue;
				}

				if (frame.frameType === "scrub") {
					for (const event of frame.events) {
						nextEvents = [event, ...nextEvents].slice(0, MAX_EVENTS);
						applyEvent(values, communities, valueStore, nextStats, event);
					}
					continue;
				}

				if (frame.frameType === "value") {
					const decoded = decodeValueFrame(frame.bytes);
					const id = decoded.id || formatBigIntId(frame.valueId);
					if (!id) {
						continue;
					}

					const value = ensureValue(
						values,
						valueStore,
						id,
						values.get(id)?.role || "data",
					);
					valueStore.applyWireFrame(frame.valueId, frame.bytes);
					syncFromFrame(value, valueStore, id);
				}
			}

			state = {
				...state,
				events: nextEvents,
				stats: nextStats,
			};

			return rebuildState();
		},
		getState() {
			return state;
		},
		selectValueById(id: string) {
			state = {
				...state,
				selectedId: id && values.has(id) ? id : null,
			};

			return rebuildState();
		},
	};
}
