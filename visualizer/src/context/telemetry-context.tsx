import { type ReactNode, useEffect, useRef } from "react";
import { telemetryWebSocketURL } from "@/features/telemetry/endpoint";
import {
	applyValueFrames,
	drainQueuedValueFrames,
	queueValueFrames,
	setFieldConnectionError,
} from "@/lib/field-store";
import { decodeValueWireMessage, type RawValueFrame } from "@/lib/wire";

/*
TelemetryConnection owns the websocket lifecycle for the dashboard.
Frames are queued by ID per animation frame and flushed into the
TanStack store in one batch, so an inflight burst of telemetry causes
exactly one render pass per RAF tick.
*/
export function TelemetryConnection({ children }: { children: ReactNode }) {
	const socketRef = useRef<WebSocket | null>(null);
	const retryTimerRef = useRef<number | null>(null);
	const flushFrameRef = useRef<number | null>(null);
	const pendingFramesRef = useRef<Map<string, RawValueFrame>>(new Map());
	const retryDelayRef = useRef(1000);
	const connectionFailedRef = useRef(false);

	useEffect(() => {
		let destroyed = false;

		function jitteredDelay(ms: number) {
			return Math.floor(ms * (0.85 + Math.random() * 0.15));
		}

		function flushFrames() {
			flushFrameRef.current = null;

			const frames = drainQueuedValueFrames(pendingFramesRef.current);

			if (frames.length === 0) {
				return;
			}

			applyValueFrames(frames);
		}

		function scheduleFlush() {
			if (flushFrameRef.current !== null) {
				return;
			}

			flushFrameRef.current = window.requestAnimationFrame(flushFrames);
		}

		function connect() {
			if (destroyed) {
				return;
			}

			const socket = new WebSocket(telemetryWebSocketURL());
			socket.binaryType = "arraybuffer";
			socketRef.current = socket;

			socket.onopen = () => {
				retryDelayRef.current = 1000;
				connectionFailedRef.current = false;
				setFieldConnectionError(null);
			};

			socket.onmessage = (message) => {
				if (!(message.data instanceof ArrayBuffer)) {
					return;
				}

				const decoded = decodeValueWireMessage(message.data);
				queueValueFrames(pendingFramesRef.current, decoded);
				scheduleFlush();
			};

			socket.onerror = () => {
				connectionFailedRef.current = true;
				console.error("telemetry websocket error", {
					targetUrl: socket.url ?? telemetryWebSocketURL(),
				});
			};

			socket.onclose = () => {
				if (destroyed) {
					return;
				}

				if (connectionFailedRef.current) {
					setFieldConnectionError("telemetry bridge unavailable");
				} else {
					setFieldConnectionError("telemetry bridge disconnected");
				}

				const wait = jitteredDelay(retryDelayRef.current);
				retryDelayRef.current = Math.min(retryDelayRef.current * 2, 30000);
				connectionFailedRef.current = false;

				retryTimerRef.current = window.setTimeout(connect, wait);
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

			if (flushFrameRef.current !== null) {
				window.cancelAnimationFrame(flushFrameRef.current);
				flushFrameRef.current = null;
			}

			pendingFramesRef.current.clear();
		};
	}, []);

	return children;
}
