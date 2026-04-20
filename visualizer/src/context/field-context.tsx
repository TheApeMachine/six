import {
	createContext,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import {
	telemetryHttpBase,
	telemetryWebSocketURL,
} from "@/features/telemetry/endpoint";
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
	sendPrompt: (text: string) => void;
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

		function sync() {
			const nextState = store.getState();
			setStats(nextState.stats);
			setSnapshot(nextState.snapshot);
			setSelection(nextState.selection);
		}

		function connect() {
			if (destroyed) {
				return;
			}

			const socket = new WebSocket("ws://localhost:6600/ws");
			socket.binaryType = "arraybuffer";
			socketRef.current = socket;

			socket.onopen = () => {
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

				if (decoded.length > 0) {
					store.applyWireFrames(decoded);
				}

				sync();
			};

			socket.onerror = () => {
				setConnectionError("telemetry bridge unavailable");
			};

			socket.onclose = () => {
				if (destroyed) {
					return;
				}

				setConnectionError("telemetry bridge disconnected");
				retryTimerRef.current = window.setTimeout(connect, 1000);
			};
		}

		connect();
		sync();

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

	const sendPrompt = useCallback(async (text: string) => {
		const trimmed = text.trim();
		if (!trimmed) {
			return;
		}

		try {
			const response = await fetch(`${telemetryHttpBase()}/api/prompt`, {
				body: JSON.stringify({ prompt: trimmed }),
				headers: { "content-type": "application/json" },
				method: "POST",
			});

			if (!response.ok) {
				const payload = (await response.json().catch(() => null)) as {
					error?: string;
				} | null;
				throw new Error(
					payload?.error || `prompt request failed: ${response.status}`,
				);
			}

			setConnectionError(null);
		} catch (error) {
			setConnectionError(
				error instanceof Error ? error.message : String(error),
			);
		}
	}, []);

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
				sendPrompt,
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
