import {
	type ReactNode,
	useCallback,
	useEffect,
	useRef,
} from "react";
import { useSelector } from "@tanstack/react-store";
import { telemetryWebSocketURL } from "@/features/telemetry/endpoint";
import type {
	VizGraphSnapshot,
	VizInspectSnapshot,
	VizRuntimeStats,
} from "@/features/telemetry/types";
import {
	applyValueFrames,
	fieldStore,
	selectFieldSelection,
	selectFieldSnapshot,
	selectFieldStats,
	selectFieldValueById,
	setFieldConnectionError,
} from "@/lib/field-store";
import { decodeValueWireMessage } from "@/lib/wire";

interface FieldContextValue {
	stats: VizRuntimeStats | null;
	snapshot: VizGraphSnapshot;
	selection: VizInspectSnapshot | null;
	connectionError: string | null;
	selectValueById: (id: string) => boolean;
}

export function FieldProvider({ children }: { children: ReactNode }) {
	const socketRef = useRef<WebSocket | null>(null);
	const retryTimerRef = useRef<number | null>(null);

	useEffect(() => {
		let destroyed = false;

		function connect() {
			if (destroyed) {
				return;
			}

			const socket = new WebSocket(telemetryWebSocketURL());
			socket.binaryType = "arraybuffer";
			socketRef.current = socket;

			socket.onopen = () => {
				console.log("telemetry bridge connected");
				setFieldConnectionError(null);
			};

			socket.onmessage = (message) => {
				if (!(message.data instanceof ArrayBuffer)) {
					return;
				}

				const decoded = decodeValueWireMessage(message.data);
				applyValueFrames(decoded);
			};

			socket.onerror = (e) => {
				console.log("telemetry bridge error", e);
				setFieldConnectionError("telemetry bridge unavailable");
			};

			socket.onclose = () => {
				console.log("telemetry bridge disconnected");

				if (destroyed) {
					return;
				}

				setFieldConnectionError("telemetry bridge disconnected");
				retryTimerRef.current = window.setTimeout(connect, 1000);
			};
		}

		connect();

		return () => {
			destroyed = true;
			socketRef.current?.close();
			socketRef.current = null;
			if (retryTimerRef.current !== null) {
				window.clearTimeout(retryTimerRef.current);
				retryTimerRef.current = null;
			}
		};
	}, []);

	return <>{children}</>;
}

export function useField(): FieldContextValue {
	const stats = useSelector(fieldStore, selectFieldStats);
	const snapshot = useSelector(fieldStore, selectFieldSnapshot);
	const selection = useSelector(fieldStore, selectFieldSelection);
	const connectionError = useSelector(
		fieldStore,
		(state) => state.connectionError,
	);
	const selectValueById = useCallback(
		(id: string) => selectFieldValueById(id),
		[],
	);

	return {
		stats,
		snapshot,
		selection,
		connectionError,
		selectValueById,
	};
}
