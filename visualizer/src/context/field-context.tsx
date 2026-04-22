import {
	createContext,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { telemetryWebSocketURL } from "@/features/telemetry/endpoint";
import type {
	VizGraphSnapshot,
	VizInspectSnapshot,
	VizRuntimeStats,
} from "@/features/telemetry/types";
import { ValueStore } from "@/lib/value-store";
import { decodeValueWireMessage } from "@/lib/wire";

interface FieldContextValue {
	stats: VizRuntimeStats | null;
	snapshot: VizGraphSnapshot;
	selection: VizInspectSnapshot | null;
	connectionError: string | null;
	selectValueById: (id: string) => boolean;
}

const FieldContext = createContext<FieldContextValue | null>(null);

export function FieldProvider({ children }: { children: React.ReactNode }) {
	const store = useMemo(() => new ValueStore(), []);
	const [stats, setStats] = useState<VizRuntimeStats | null>(
		() => store.getState().stats,
	);
	const [snapshot, setSnapshot] = useState<VizGraphSnapshot>(
		() => store.getState().snapshot,
	);
	const [selection, setSelection] = useState<VizInspectSnapshot | null>(null);
	const [connectionError, setConnectionError] = useState<string | null>(null);
	const socketRef = useRef<WebSocket | null>(null);
	const retryTimerRef = useRef<number | null>(null);

	useEffect(() => {
		let destroyed = false;

		function applyTelemetry(nextState: ReturnType<ValueStore["getState"]>) {
			setStats(nextState.stats);
			setSnapshot(nextState.snapshot);
			setSelection(nextState.selection);
		}

		function connect() {
			if (destroyed) {
				return;
			}

			const socket = new WebSocket(telemetryWebSocketURL());
			socket.binaryType = "arraybuffer";
			socketRef.current = socket;

			socket.onopen = () => {
				console.log("telemetry bridge connected");
				setConnectionError(null);
			};

			socket.onmessage = (message) => {
				if (!(message.data instanceof ArrayBuffer)) {
					return;
				}

				/*
				The bridge multiplexes two message shapes on this socket:
				raw Value frames (one or more contiguous 1024-byte images)
				and structured telemetry envelopes (FieldMetrics today,
				CausalEvent tomorrow). decodeValueWireMessage forks on the
				magic bytes so we can dispatch both in one sync pass.
				*/
				const decoded = decodeValueWireMessage(message.data);

				const nextState =
					decoded.length > 0
						? store.applyWireFrames(decoded)
						: store.getState();
				applyTelemetry(nextState);
			};

			socket.onerror = (e) => {
				console.log("telemetry bridge error", e);
				setConnectionError("telemetry bridge unavailable");
			};

			socket.onclose = () => {
				console.log("telemetry bridge disconnected");

				if (destroyed) {
					return;
				}

				setConnectionError("telemetry bridge disconnected");
				retryTimerRef.current = window.setTimeout(connect, 1000);
			};
		}

		connect();
		applyTelemetry(store.getState());

		return () => {
			destroyed = true;
			socketRef.current?.close();
			socketRef.current = null;
			if (retryTimerRef.current !== null) {
				window.clearTimeout(retryTimerRef.current);
				retryTimerRef.current = null;
			}
		};
	}, [store]);

	const selectValueById = useCallback(
		(id: string) => {
			const nextState = store.selectValueById(id);
			setSelection(nextState.selection);
			setSnapshot(nextState.snapshot);
			setStats(nextState.stats);

			return nextState.selection !== null;
		},
		[store],
	);

	return (
		<FieldContext.Provider
			value={{
				stats,
				snapshot,
				selection,
				connectionError,
				selectValueById,
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
