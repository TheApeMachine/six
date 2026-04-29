import { createStore } from "@tanstack/react-store";
import { ID_START_WORD } from "./layoutGenerated";
import {
	decodeValueFrame,
	type StoredValue,
	storedValueFromDecoded,
	valueFrameExpired,
} from "./value-frame";
import { readWordU64LE } from "./valueRegions";
import type { RawValueFrame } from "./wire";

/*
TimelineEvent is one frame the bridge committed to the dashboard. The
visualiser no longer overwrites Value state in place — every applied
frame is appended here so the user can scrub backward, play forward,
and jump to the exact tick a Value changed. tick is monotonic and
increments once per flush batch (not per frame), so a coordinated burst
of writes (e.g. recruiter accepting eight candidates in one ALU pass)
shares a single tick index. tombstone frames (TTL expired sentinels)
record the Value's removal so playback shows it disappear at the
correct tick instead of remaining stuck on its last live image.
*/
export interface TimelineEvent {
	tick: number;
	recvAtMs: number;
	valueId: string;
	frame: Uint8Array;
	tombstone: boolean;
}

export interface TimelineState {
	events: TimelineEvent[];
	tickCount: number;
	cursorTick: number | null;
	playing: boolean;
	playbackRate: number;
	maxEvents: number;
	/*
	lastTouchAtHead is the per-Value latest tick at which a frame
	committed at the live head. Activity glow at head reads it directly
	(no event walk); cursor-mode renders rebuild equivalent state by
	projecting through the event log.
	*/
	lastTouchAtHead: Map<string, number>;
}

const DEFAULT_MAX_EVENTS = 50000;
const DEFAULT_PLAYBACK_RATE = 30;

export const timelineStore = createStore<TimelineState>({
	events: [],
	tickCount: 0,
	cursorTick: null,
	playing: false,
	playbackRate: DEFAULT_PLAYBACK_RATE,
	maxEvents: DEFAULT_MAX_EVENTS,
	lastTouchAtHead: new Map(),
});

interface RecordedFrame {
	rawId: string;
	frame: Uint8Array;
	tombstone: boolean;
}

/*
recordTick appends one batch of frames to the timeline as a single
tick. The callers (field-store) hand over already-classified frames
(rawId resolved, tombstone flag set) so this store does not duplicate
decode work. Empty batches do not advance tickCount; that keeps the
tick axis a true measure of bridge progress, not RAF rate.
*/
export function recordTick(frames: RecordedFrame[]) {
	if (frames.length === 0) {
		return;
	}

	timelineStore.setState((state) => {
		const tick = state.tickCount;
		const recvAtMs = Date.now();
		const events = state.events.slice();
		const lastTouchAtHead = new Map(state.lastTouchAtHead);

		for (const frame of frames) {
			events.push({
				tick,
				recvAtMs,
				valueId: frame.rawId,
				frame: frame.frame,
				tombstone: frame.tombstone,
			});

			if (frame.tombstone) {
				lastTouchAtHead.delete(frame.rawId);
			} else {
				lastTouchAtHead.set(frame.rawId, tick);
			}
		}

		const cap = state.maxEvents;
		const trim = events.length > cap ? events.length - cap : 0;
		const trimmedEvents = trim > 0 ? events.slice(trim) : events;

		let cursorTick =
			state.cursorTick !== null && state.cursorTick > tick
				? tick
				: state.cursorTick;

		if (trim > 0 && trimmedEvents.length > 0 && cursorTick !== null) {
			const oldestTick = trimmedEvents[0].tick;

			if (cursorTick < oldestTick) {
				cursorTick = oldestTick;
			}
		}

		return {
			...state,
			events: trimmedEvents,
			tickCount: tick + 1,
			cursorTick,
			lastTouchAtHead,
		};
	});
}

export function resetTimeline() {
	timelineStore.setState((state) => ({
		...state,
		events: [],
		tickCount: 0,
		cursorTick: null,
		playing: false,
		lastTouchAtHead: new Map(),
	}));
}

/*
setCursorTick pins the dashboard view to a specific tick. Passing null
returns to live (head) mode; the 3D scene and detail panel re-render
from whichever source matches.
*/
export function setCursorTick(tick: number | null) {
	timelineStore.setState((state) => {
		if (tick === null) {
			return { ...state, cursorTick: null, playing: false };
		}

		const clamped = Math.max(0, Math.min(state.tickCount - 1, tick));

		return { ...state, cursorTick: clamped };
	});
}

export function stepCursor(direction: 1 | -1) {
	timelineStore.setState((state) => {
		const head = state.tickCount - 1;
		const current = state.cursorTick ?? head;
		const next = Math.max(0, Math.min(head, current + direction));

		return { ...state, cursorTick: next, playing: false };
	});
}

export function setPlaying(playing: boolean) {
	timelineStore.setState((state) => ({ ...state, playing }));
}

export function setPlaybackRate(rate: number) {
	timelineStore.setState((state) => ({
		...state,
		playbackRate: Math.max(1, Math.min(240, Math.round(rate))),
	}));
}

/*
projectValuesAtCursor walks the event log up to the cursor tick and
returns the Value snapshot the dashboard should render. When cursor is
null we treat the head tick as the cutoff, which produces the same map
as the live field-store but lets every renderer use a single source.

The walk cost is O(events scanned), which is fine for the cap (50k);
adjacent ticks during scrubbing reuse last-cursor cache via a closure
in the hook below.
*/
export interface ProjectionResult {
	values: Map<string, StoredValue>;
	lastTouch: Map<string, number>;
	cutoff: number;
}

export function projectValuesAtCursor(
	events: readonly TimelineEvent[],
	cutoff: number,
): ProjectionResult {
	const values = new Map<string, StoredValue>();
	const lastTouch = new Map<string, number>();

	if (cutoff < 0 || events.length === 0) {
		return { values, lastTouch, cutoff };
	}

	const lastFrames = new Map<string, RawValueFrame>();

	for (const event of events) {
		if (event.tick > cutoff) {
			break;
		}

		if (event.tombstone) {
			lastFrames.delete(event.valueId);
			lastTouch.delete(event.valueId);
			continue;
		}

		lastFrames.set(event.valueId, {
			valueId: readWordU64LE(event.frame, ID_START_WORD),
			bytes: event.frame,
		});
		lastTouch.set(event.valueId, event.tick);
	}

	for (const [rawId, raw] of lastFrames) {
		const decoded = decodeValueFrame(raw.bytes);

		if (valueFrameExpired(decoded)) {
			lastTouch.delete(rawId);
			continue;
		}

		values.set(rawId, storedValueFromDecoded(rawId, decoded));
	}

	return { values, lastTouch, cutoff };
}

/*
collectValueHistory walks the event log for one Value id and returns
its tick → frame trail, ascending. The detail panel uses this to draw a
per-Value mini-timeline that highlights every tick where the substrate
touched the frame, and to let the user jump to those ticks.
*/
export function collectValueHistory(
	state: TimelineState,
	valueId: string,
): TimelineEvent[] {
	return state.events.filter((event) => event.valueId === valueId);
}
