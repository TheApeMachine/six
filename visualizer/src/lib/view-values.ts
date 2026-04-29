import { useSelector } from "@tanstack/react-store";
import { useMemo } from "react";
import { fieldStore } from "./field-store";
import { projectValuesAtCursor, timelineStore } from "./timeline-store";
import type { StoredValue } from "./value-frame";

/*
useViewValues is the single read path for the dashboard. When the user
is in live mode (cursorTick === null) it returns the head-state Values
the field-store maintains plus the timeline-store's lastTouchAtHead so
no event walk runs on the hot path. When the user has scrubbed back, it
projects the values and lastTouch maps from the timeline event log so
every panel — the 3D scene, the detail inspector, the chips at the
top — agrees on the exact tick the user is looking at.

ticksSinceTouch is the activity input the 3D scene uses to brighten
recently changed instances. 0 means "this Value committed at the cursor
tick" — the brightest glow — and rises monotonically as the touch
recedes into history.
*/
export interface ViewValues {
	values: ReadonlyMap<string, StoredValue>;
	ticksSinceTouch: ReadonlyMap<string, number>;
	cursorTick: number | null;
	tickCount: number;
	atHead: boolean;
}

export function useViewValues(): ViewValues {
	const liveValues = useSelector(fieldStore, (state) => state.values);
	const cursorTick = useSelector(
		timelineStore,
		(state) => state.cursorTick,
	);
	const tickCount = useSelector(timelineStore, (state) => state.tickCount);
	const events = useSelector(timelineStore, (state) => state.events);
	const lastTouchAtHead = useSelector(
		timelineStore,
		(state) => state.lastTouchAtHead,
	);

	return useMemo<ViewValues>(() => {
		if (cursorTick === null) {
			const head = tickCount - 1;
			const ticksSinceTouch = new Map<string, number>();
			for (const [id, lastTick] of lastTouchAtHead) {
				ticksSinceTouch.set(id, Math.max(0, head - lastTick));
			}

			return {
				values: liveValues,
				ticksSinceTouch,
				cursorTick: null,
				tickCount,
				atHead: true,
			};
		}

		const projection = projectValuesAtCursor(events, cursorTick);
		const ticksSinceTouch = new Map<string, number>();
		for (const [id, tick] of projection.lastTouch) {
			ticksSinceTouch.set(id, Math.max(0, projection.cutoff - tick));
		}

		return {
			values: projection.values,
			ticksSinceTouch,
			cursorTick,
			tickCount,
			atHead: false,
		};
	}, [cursorTick, tickCount, events, liveValues, lastTouchAtHead]);
}
