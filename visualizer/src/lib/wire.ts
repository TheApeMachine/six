/*
Binary WebSocket payloads from the bridge take the shape of raw
primitive.Value wire images: one or more contiguous frames
(128 × uint64 LE each). These are the per-Value lifecycle frames
every firmware tick tees out.
*/
import { VALUE_FRAME_BYTE_LENGTH } from "./valueLayout";
import { decodeValueRegionsFromFrame } from "./valueRegions";

export interface Value {
	id: bigint;
	prev: bigint;
	next: bigint;
	tokens: bigint[];
	assets: bigint[];
	program: bigint[];
	signals: bigint[];
	context: bigint[];
	gradient: bigint[];
	properties: bigint[];
	affinity: bigint[];
}

/*
decodeValueWireMessage splits a WebSocket binary message into one or
more fully-decoded Values. Each frame in the message must be exactly
VALUE_FRAME_BYTE_LENGTH bytes; messages whose length is not a whole
multiple of that frame size are rejected with an empty result so a
malformed bridge frame cannot corrupt downstream state.
*/
export const decodeValueWireMessage = (
	data: ArrayBuffer | Uint8Array,
): Value[] => {
	const u8 = data instanceof ArrayBuffer ? new Uint8Array(data) : data;

	const wlen = VALUE_FRAME_BYTE_LENGTH;
	const n = u8.byteLength;

	if (n === 0 || n % wlen !== 0) {
		return [];
	}

	const values: Value[] = [];

	for (let off = 0; off < n; off += wlen) {
		const regions = decodeValueRegionsFromFrame(u8.subarray(off, off + wlen));

		values.push({
			id: regions.id.words[0],
			prev: regions.prev.words[0],
			next: regions.next.words[0],
			tokens: regions.tokens.words,
			assets: regions.asset.words,
			program: regions.program.words,
			signals: regions.signals.words,
			context: regions.context.words,
			gradient: regions.gradient.words,
			properties: regions.properties.words,
			affinity: regions.affinity.words,
		});
	}

	return values;
}
