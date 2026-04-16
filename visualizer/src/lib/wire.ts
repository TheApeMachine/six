/*
Binary WebSocket payloads from the bridge are raw primitive.Value wire images:
one or more contiguous frames (128 × uint64 LE each). No VZB envelope.
*/

import { readWordU64LE, VALUE_FRAME_BYTE_LENGTH, WORD } from "./valueLayout";

export interface RawValueFrame {
	valueId: bigint;
	bytes: Uint8Array;
}

/*
DecodeValueWireMessage splits a WebSocket binary message into full Value frames
and reads the id word (122) for each.
*/
export function decodeValueWireMessage(
	data: ArrayBuffer | Uint8Array,
): RawValueFrame[] {
	const u8 = data instanceof ArrayBuffer ? new Uint8Array(data) : data;
	const wlen = VALUE_FRAME_BYTE_LENGTH;
	const n = u8.byteLength;

	if (n === 0 || n % wlen !== 0) {
		return [];
	}

	const out: RawValueFrame[] = [];

	for (let off = 0; off < n; off += wlen) {
		const bytes = u8.subarray(off, off + wlen);
		const valueId = readWordU64LE(bytes, WORD.ID);

		out.push({ valueId, bytes: new Uint8Array(bytes) });
	}

	return out;
}
