import { createStore } from "@tanstack/react-store";
import {
	decodeValueFrame,
	formatValueId,
	type StoredValue,
	storedValueFromDecoded,
	valueFrameExpired,
} from "./value-frame";
import { readWordU64LE } from "./valueRegions";
import type { RawValueFrame } from "./wire";

export interface FieldStoreState {
	values: Map<string, StoredValue>;
	selectedId: string | null;
	connectionError: string | null;
}

/*
fieldStore is the single source of truth for the dashboard. The
TanStack store dedupes by Value id (Map), so the wire side only has
to deliver raw frames — ones we have not seen are inserted, ones we
have are overwritten in-place, expired frames (TTL sentinel) drop out.
*/
export const fieldStore = createStore<FieldStoreState>({
	values: new Map(),
	selectedId: null,
	connectionError: null,
});

const TELEMETRY_RUN_MARKER_MAGIC = 0x73697872756e3031n;
const TELEMETRY_RUN_MARKER_QUEUE_KEY = "run-marker";

export function applyValueFrames(frames: RawValueFrame[]) {
	if (frames.length === 0) {
		return;
	}

	fieldStore.setState((state) => {
		let values = state.values;
		let selectedId = state.selectedId;
		let changed = false;

		for (const frame of frames) {
			if (isTelemetryRunMarker(frame)) {
				values = new Map();
				selectedId = null;
				changed = true;
				continue;
			}

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
		};
	});
}

export function queueValueFrames(
	queue: Map<string, RawValueFrame>,
	frames: RawValueFrame[],
) {
	for (const frame of frames) {
		if (isTelemetryRunMarker(frame)) {
			queue.clear();
			queue.set(TELEMETRY_RUN_MARKER_QUEUE_KEY, frame);
			continue;
		}

		const rawId = formatValueId(frame.valueId);

		if (!rawId) {
			continue;
		}

		queue.set(rawId, frame);
	}
}

export function drainQueuedValueFrames(queue: Map<string, RawValueFrame>) {
	const frames = Array.from(queue.values());
	queue.clear();

	return frames;
}

export function isTelemetryRunMarker(frame: RawValueFrame): boolean {
	return (
		frame.valueId === 0n &&
		frame.bytes.byteLength >= 8 &&
		readWordU64LE(frame.bytes, 0) === TELEMETRY_RUN_MARKER_MAGIC
	);
}

export function setFieldConnectionError(connectionError: string | null) {
	fieldStore.setState((state) => ({
		...state,
		connectionError,
	}));
}

/*
selectFieldValueById flips the selection to id when known, clears it
when not. Returns whether the selection landed on a real Value so the
caller can decide whether to show "no such value" feedback.
*/
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
		connectionError: null,
	}));
}
