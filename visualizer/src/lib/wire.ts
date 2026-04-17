/*
Binary WebSocket payloads from the bridge take one of two shapes:

 1. Raw primitive.Value wire images: one or more contiguous frames (128 ×
    uint64 LE each). These are the per-Value lifecycle frames every
    firmware tick tees out.
 2. Structured envelopes: 8-byte header (magic "VZB1" + uint32 kind LE)
    followed by a JSON body. Envelopes carry field-level metrics and
    causal events that would otherwise need a side channel.

The decoder forks on the first four bytes — the magic — so legacy
producers that only emit Value frames still round-trip correctly.
*/

import { readWordU64LE, VALUE_FRAME_BYTE_LENGTH, WORD } from "./valueLayout";

export interface RawValueFrame {
	valueId: bigint;
	bytes: Uint8Array;
}

export const ENVELOPE_MAGIC = [0x56, 0x5a, 0x42, 0x31] as const; // "VZB1"
export const ENVELOPE_HEADER_BYTES = 8;

export const ENVELOPE_KIND_FIELD_METRICS = 1;
export const ENVELOPE_KIND_CAUSAL_EVENT = 2;

export interface FieldMetricsEnvelope {
	kind: typeof ENVELOPE_KIND_FIELD_METRICS;
	payload: FieldMetricsPayload;
}

export interface CausalEventEnvelope {
	kind: typeof ENVELOPE_KIND_CAUSAL_EVENT;
	payload: CausalEventPayload;
}

export type TelemetryEnvelope = FieldMetricsEnvelope | CausalEventEnvelope;

export interface FieldMetricsPayload {
	communityIdx: number;
	memberCount: number;
	labeledCount: number;
	slotSum: number;
	coverage: number;
	consensus: number;
	labelDensity: number;
	crystallization: number;
	dominantRatio: number;
	modeCount: number;
	pressureMult: number;
	saturated: boolean;
}

export interface CausalEventPayload {
	valueId: number; // uint64 arrives as Number; precision loss is acceptable for display
	kind: string;
	timestamp: number;
}

export interface DecodedWireMessage {
	frames: RawValueFrame[];
	envelope: TelemetryEnvelope | null;
}

function magicMatches(u8: Uint8Array): boolean {
	return (
		u8.byteLength >= ENVELOPE_HEADER_BYTES &&
		u8[0] === ENVELOPE_MAGIC[0] &&
		u8[1] === ENVELOPE_MAGIC[1] &&
		u8[2] === ENVELOPE_MAGIC[2] &&
		u8[3] === ENVELOPE_MAGIC[3]
	);
}

function decodeEnvelope(u8: Uint8Array): TelemetryEnvelope | null {
	const kind =
		(u8[4] | (u8[5] << 8) | (u8[6] << 16) | (u8[7] << 24)) >>> 0;

	// JSON body may be NUL-terminated when the encoder padded away a
	// 1024-byte collision; JSON.parse fails on trailing NULs so trim
	// them off before decoding.
	let end = u8.byteLength;

	while (end > ENVELOPE_HEADER_BYTES && u8[end - 1] === 0) {
		end--;
	}

	const body = u8.subarray(ENVELOPE_HEADER_BYTES, end);
	const text = new TextDecoder().decode(body);

	try {
		const payload = JSON.parse(text);

		if (kind === ENVELOPE_KIND_FIELD_METRICS) {
			return {
				kind: ENVELOPE_KIND_FIELD_METRICS,
				payload: payload as FieldMetricsPayload,
			};
		}

		if (kind === ENVELOPE_KIND_CAUSAL_EVENT) {
			return {
				kind: ENVELOPE_KIND_CAUSAL_EVENT,
				payload: payload as CausalEventPayload,
			};
		}
	} catch {
		// A malformed envelope must not take down the pipeline — every
		// Cycle emits a fresh envelope so a single corrupt message is
		// replaced within milliseconds.
	}

	return null;
}

/*
decodeValueWireMessage splits a WebSocket binary message into full Value
frames (when the message is a multiple of 1024 bytes and has no magic
prefix) or extracts a structured telemetry envelope (when the magic
bytes are present). Returns both in one pass so callers dispatch on a
single object.
*/
export function decodeValueWireMessage(
	data: ArrayBuffer | Uint8Array,
): DecodedWireMessage {
	const u8 = data instanceof ArrayBuffer ? new Uint8Array(data) : data;

	if (magicMatches(u8)) {
		return { frames: [], envelope: decodeEnvelope(u8) };
	}

	const wlen = VALUE_FRAME_BYTE_LENGTH;
	const n = u8.byteLength;

	if (n === 0 || n % wlen !== 0) {
		return { frames: [], envelope: null };
	}

	const frames: RawValueFrame[] = [];

	for (let off = 0; off < n; off += wlen) {
		const bytes = u8.subarray(off, off + wlen);
		const valueId = readWordU64LE(bytes, WORD.ID);

		frames.push({ valueId, bytes: new Uint8Array(bytes) });
	}

	return { frames, envelope: null };
}
