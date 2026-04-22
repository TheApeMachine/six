import { ID_START_WORD, VALUE_FRAME_BYTE_LENGTH } from "./layoutGenerated";
import { readWordU64LE } from "./valueRegions";

export interface RawValueFrame {
	valueId: bigint;
	bytes: Uint8Array;
}

/*
decodeValueWireMessage splits a WebSocket binary message into one or
more raw Value frames. Each frame in the message must be exactly
VALUE_FRAME_BYTE_LENGTH bytes; messages whose length is not a whole
multiple of that frame size are rejected with an empty result so a
malformed bridge frame cannot corrupt downstream state.
*/
export const decodeValueWireMessage = (
	data: ArrayBuffer | Uint8Array,
): RawValueFrame[] => {
	const u8 = data instanceof ArrayBuffer ? new Uint8Array(data) : data;

	const wlen = VALUE_FRAME_BYTE_LENGTH;
	const n = u8.byteLength;

	if (n === 0 || n % wlen !== 0) {
		return [];
	}

	const frames: RawValueFrame[] = [];

	for (let off = 0; off < n; off += wlen) {
		const bytes = u8.subarray(off, off + wlen);
		const valueId = readWordU64LE(bytes, ID_START_WORD);

		frames.push({ valueId, bytes: new Uint8Array(bytes) });
	}

	return frames;
};
