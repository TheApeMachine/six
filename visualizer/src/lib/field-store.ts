import { createStore } from "@tanstack/react-store";
import type { StoredValue, TelemetryState } from "./value-frame";
import {
	buildGraphSnapshot,
	decodeValueFrame,
	formatValueId,
	inspectFromStored,
	storedValueFromDecoded,
	valueFrameExpired,
} from "./value-frame";
import type { RawValueFrame } from "./wire";

export interface FieldStoreState {
	values: Map<string, StoredValue>;
	selectedId: string | null;
	graphSeq: number;
	connectionError: string | null;
}

export const fieldStore = createStore<FieldStoreState>({
	values: new Map(),
	selectedId: null,
	graphSeq: 0,
	connectionError: null,
});

let snapshotCache:
	| {
			values: ReadonlyMap<string, StoredValue>;
			graphSeq: number;
			snapshot: TelemetryState["snapshot"];
	  }
	| undefined;

let statsCache:
	| {
			size: number;
			stats: TelemetryState["stats"];
	  }
	| undefined;

let selectionCache:
	| {
			selectedId: string | null;
			value: StoredValue | undefined;
			selection: TelemetryState["selection"];
	  }
	| undefined;

export function selectFieldSnapshot(
	state: FieldStoreState,
): TelemetryState["snapshot"] {
	if (
		snapshotCache &&
		snapshotCache.values === state.values &&
		snapshotCache.graphSeq === state.graphSeq
	) {
		return snapshotCache.snapshot;
	}

	const snapshot = buildGraphSnapshot(state.values, state.graphSeq);
	snapshotCache = {
		values: state.values,
		graphSeq: state.graphSeq,
		snapshot,
	};

	return snapshot;
}

export function selectFieldStats(state: FieldStoreState): TelemetryState["stats"] {
	if (statsCache?.size === state.values.size) {
		return statsCache.stats;
	}

	const stats = { values: state.values.size };
	statsCache = { size: state.values.size, stats };

	return stats;
}

export function selectFieldSelection(
	state: FieldStoreState,
): TelemetryState["selection"] {
	const value = state.selectedId ? state.values.get(state.selectedId) : undefined;

	if (
		selectionCache &&
		selectionCache.selectedId === state.selectedId &&
		selectionCache.value === value
	) {
		return selectionCache.selection;
	}

	const selection = value ? inspectFromStored(value) : null;
	selectionCache = {
		selectedId: state.selectedId,
		value,
		selection,
	};

	return selection;
}

export function getFieldTelemetryState(): TelemetryState {
	const state = fieldStore.get();
	const selection = selectFieldSelection(state);

	return {
		selection,
		selectedId: selection ? state.selectedId : null,
		snapshot: selectFieldSnapshot(state),
		stats: selectFieldStats(state),
	};
}

export function applyValueFrames(frames: RawValueFrame[]) {
	if (frames.length === 0) {
		return;
	}

	fieldStore.setState((state) => {
		let values = state.values;
		let selectedId = state.selectedId;
		let changed = false;

		for (const frame of frames) {
			const rawId = formatValueId(frame.valueId);

			if (!rawId) {
				continue;
			}

			const decoded = decodeValueFrame(frame.bytes);

			if (valueFrameExpired(decoded)) {
				if (!changed) {
					values = new Map(values);
					changed = true;
				}

				values.delete(rawId);
				if (selectedId === rawId) {
					selectedId = null;
				}
				continue;
			}

			const previous = values.get(rawId);
			const next = storedValueFromDecoded(rawId, decoded, previous);

			if (!changed) {
				values = new Map(values);
				changed = true;
			}

			values.set(rawId, next);
		}

		if (!changed) {
			return state;
		}

		return {
			...state,
			values,
			selectedId,
			graphSeq: state.graphSeq + 1,
		};
	});
}

export function setFieldConnectionError(connectionError: string | null) {
	fieldStore.setState((state) => ({
		...state,
		connectionError,
	}));
}

export function selectFieldValueById(id: string): boolean {
	const state = fieldStore.get();
	const selectedId = id && state.values.has(id) ? id : null;

	fieldStore.setState((current) => ({
		...current,
		selectedId,
	}));

	return selectedId !== null;
}

export function resetFieldStore() {
	fieldStore.setState(() => ({
		values: new Map(),
		selectedId: null,
		graphSeq: 0,
		connectionError: null,
	}));
	snapshotCache = undefined;
	statsCache = undefined;
	selectionCache = undefined;
}
