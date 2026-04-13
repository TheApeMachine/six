import {
	createContext,
	useCallback,
	useContext,
	useEffect,
	useRef,
	useState,
} from "react";
import {
	initEngine,
	type VizCallbacks,
	type VizGraphSnapshot,
	type VizInspectSnapshot,
	type VizRuntimeStats,
} from "@/lib/engine";
import type { VizEvent } from "@/lib/wire";

const MAX_HISTORY = 240;
const MAX_EVENTS = 500;

interface FieldContextValue {
	stats: VizRuntimeStats | null;
	snapshot: VizGraphSnapshot | null;
	snapshotHistory: VizGraphSnapshot[];
	selection: VizInspectSnapshot | null;
	events: VizEvent[];
	sendPrompt: (text: string) => void;
	/*
	setEngineContainer registers the HTMLDivElement that the engine's 2D canvas
	should be mounted inside. Call this once from FieldViewer's "Live" tab div
	ref. The engine is initialised (or re-initialised) as soon as the element
	is available, and torn down when it changes to null.
	*/
	setEngineContainer: (el: HTMLDivElement | null) => void;
}

const FieldContext = createContext<FieldContextValue | null>(null);

export function FieldProvider({ children }: { children: React.ReactNode }) {
	const engineRef = useRef<ReturnType<typeof initEngine> | null>(null);
	const [stats, setStats] = useState<VizRuntimeStats | null>(null);
	const [snapshot, setSnapshot] = useState<VizGraphSnapshot | null>(null);
	const [snapshotHistory, setSnapshotHistory] = useState<VizGraphSnapshot[]>(
		[],
	);
	const [selection, setSelection] = useState<VizInspectSnapshot | null>(null);
	const [events, setEvents] = useState<VizEvent[]>([]);
	const [engineContainer, setEngineContainerState] =
		useState<HTMLDivElement | null>(null);

	// Raw event ring buffer — updated synchronously on every event, flushed to
	// React state at ~10 Hz so the stream panel stays live without flooding the
	// React scheduler.
	const eventsRef = useRef<VizEvent[]>([]);
	const flushPending = useRef(false);

	const setEngineContainer = useCallback((el: HTMLDivElement | null) => {
		setEngineContainerState(el);
	}, []);

	useEffect(() => {
		if (!engineContainer) return;

		const callbacks: VizCallbacks = {
			onEvent: (ev: VizEvent) => {
				eventsRef.current = [ev, ...eventsRef.current.slice(0, MAX_EVENTS - 1)];

				if (!flushPending.current) {
					flushPending.current = true;
					setTimeout(() => {
						setEvents(eventsRef.current.slice());
						flushPending.current = false;
					}, 100);
				}
			},
			onStats: setStats,
			onSelection: setSelection,
			onGraphSnapshot: (snap) => {
				setSnapshot(snap);
				if (snap.totalValues === 0 && snap.totalCommunities === 0) return;
				setSnapshotHistory((prev) => {
					const next = [...prev, snap];
					return next.length > MAX_HISTORY
						? next.slice(next.length - MAX_HISTORY)
						: next;
				});
			},
		};

		engineRef.current = initEngine(engineContainer, callbacks);

		return () => {
			engineRef.current?.destroy();
			engineRef.current = null;
		};
	}, [engineContainer]);

	const sendPrompt = useCallback((text: string) => {
		engineRef.current?.sendPrompt(text);
	}, []);

	return (
		<FieldContext.Provider
			value={{
				stats,
				snapshot,
				snapshotHistory,
				selection,
				events,
				sendPrompt,
				setEngineContainer,
			}}
		>
			{children}
		</FieldContext.Provider>
	);
}

export function useField() {
	const ctx = useContext(FieldContext);
	if (!ctx) throw new Error("useField must be used within FieldProvider");
	return ctx;
}
