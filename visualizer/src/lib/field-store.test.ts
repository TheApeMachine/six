import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import {
	applyValueFrames,
	drainQueuedValueFrames,
	fieldStore,
	isTelemetryRunMarker,
	queueValueFrames,
	resetFieldStore,
} from "./field-store";
import {
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
} from "./layoutGenerated";
import { PROPERTY_WORD } from "./propertiesGenerated";
import { decodeValueFrame, formatValueId } from "./value-frame";
import { decodeValueWireMessage, type RawValueFrame } from "./wire";

const PROPERTIES_COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const PROPERTIES_TTL_WORD = PROPERTY_WORD("TTL");
const TTL_EXPIRED_SENTINEL_WORD = (1n << 64n) - 1n;
const TELEMETRY_RUN_MARKER_MAGIC = 0x73697872756e3031n;

function writeWord(frame: Uint8Array, wordIndex: number, word: bigint) {
	const offset = wordIndex * 8;
	const view = new DataView(frame.buffer, frame.byteOffset + offset, 8);
	view.setBigUint64(0, word, true);
}

function makeValueFrame(init?: {
	id?: bigint;
	prev?: bigint;
	next?: bigint;
	communityId?: bigint;
	ttl?: bigint;
}) {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);

	if (init?.id !== undefined) {
		writeWord(frame, ID_START_WORD, init.id);
	}

	if (init?.prev !== undefined) {
		writeWord(frame, PREV_START_WORD, init.prev);
	}

	if (init?.next !== undefined) {
		writeWord(frame, NEXT_START_WORD, init.next);
	}

	if (init?.communityId !== undefined) {
		writeWord(frame, PROPERTIES_COMMUNITY_WORD, init.communityId);
	}

	if (init?.ttl !== undefined) {
		writeWord(frame, PROPERTIES_TTL_WORD, init.ttl);
	}

	return frame;
}

function makeRunMarkerFrame() {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);
	writeWord(frame, 0, TELEMETRY_RUN_MARKER_MAGIC);

	return frame;
}

beforeEach(resetFieldStore);

function applyFrame(valueId: bigint, bytes: Uint8Array) {
	applyValueFrames([{ valueId, bytes }]);

	return fieldStore.get().values.get(formatValueId(valueId));
}

function storedValue(id: string) {
	return fieldStore.get().values.get(id);
}

function storeSize() {
	return fieldStore.get().values.size;
}

test("decodeValueFrame reads id and chain ids from raw Value bytes", () => {
	const frame = makeValueFrame({ id: 0xabn, prev: 0x10n, next: 0x20n });
	const decoded = decodeValueFrame(frame);

	assert.equal(decoded.id, "00000000000000ab");
	assert.equal(decoded.prevId, "0000000000000010");
	assert.equal(decoded.nextId, "0000000000000020");
	assert.equal(decoded.words.length, VALUE_WORD_COUNT);
	assert.equal(decoded.regions.id.words[0], 0xabn);
	assert.equal(decoded.regions.program.startWord, 16);
});

test("fieldStore creates a stored value directly from a raw frame", () => {
	const frame = makeValueFrame({ id: 0x44n, prev: 0x11n, next: 0x22n });
	const value = applyFrame(0x44n, frame);

	assert.equal(storeSize(), 1);
	assert.equal(value?.id, "0000000000000044");
	assert.equal(value?.decoded?.prevId, "0000000000000011");
	assert.equal(value?.decoded?.nextId, "0000000000000022");
});

test("fieldStore overwrites the stored frame when a newer wire image arrives", () => {
	applyFrame(0xffn, makeValueFrame({ id: 0xffn, communityId: 1n }));
	applyFrame(0xffn, makeValueFrame({ id: 0xffn, communityId: 2n }));

	const value = storedValue("00000000000000ff");

	assert.ok(value);
	assert.equal(value?.communityId, 2);
});

test("fieldStore keys updates by ValueID instead of creating decoded-id copies", () => {
	applyFrame(0x44n, makeValueFrame({ id: 0x44n }));
	applyFrame(0x44n, makeValueFrame({ id: 0x99n }));

	const value = storedValue("0000000000000044");

	assert.equal(storeSize(), 1);
	assert.equal(value?.id, "0000000000000044");
	assert.equal(value?.decoded?.id, "0000000000000099");
	assert.equal(storedValue("0000000000000099"), undefined);
});

test("fieldStore clears stale Values when a run marker arrives", () => {
	applyFrame(0x1n, makeValueFrame({ id: 0x1n }));
	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 0x20n }));

	assert.equal(storeSize(), 2);

	applyValueFrames([{ valueId: 0n, bytes: makeRunMarkerFrame() }]);

	assert.equal(storeSize(), 0);
});

test("queueValueFrames keeps only the newest pending frame per ValueID", () => {
	const queue = new Map<string, RawValueFrame>();
	const first = makeValueFrame({ id: 0x1n, communityId: 1n });
	const next = makeValueFrame({ id: 0x1n, communityId: 2n });
	const other = makeValueFrame({ id: 0x2n });

	queueValueFrames(queue, [
		{ valueId: 0x1n, bytes: first },
		{ valueId: 0x2n, bytes: other },
	]);
	queueValueFrames(queue, [{ valueId: 0x1n, bytes: next }]);

	const frames = drainQueuedValueFrames(queue);

	assert.equal(frames.length, 2);
	assert.equal(frames[0].valueId, 0x1n);
	assert.equal(frames[0].bytes, next);
	assert.equal(frames[1].valueId, 0x2n);
	assert.equal(queue.size, 0);
});

test("queueValueFrames keeps run markers ordered ahead of the next run state", () => {
	const queue = new Map<string, RawValueFrame>();
	const old = makeValueFrame({ id: 0x1n });
	const next = makeValueFrame({ id: 0x2n });
	const marker = { valueId: 0n, bytes: makeRunMarkerFrame() };

	queueValueFrames(queue, [
		{ valueId: 0x1n, bytes: old },
		marker,
		{ valueId: 0x2n, bytes: next },
	]);

	const frames = drainQueuedValueFrames(queue);

	assert.equal(frames.length, 2);
	assert.ok(isTelemetryRunMarker(frames[0]));
	assert.equal(frames[1].valueId, 0x2n);
	assert.equal(frames[1].bytes, next);
});

test("fieldStore reads community id from the on-wire properties word", () => {
	applyFrame(0x1n, makeValueFrame({ id: 0x1n, communityId: 7n }));
	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 7n }));
	applyFrame(0x3n, makeValueFrame({ id: 0x3n, communityId: 42n }));

	assert.equal(storedValue("0000000000000001")?.communityId, 7);
	assert.equal(storedValue("0000000000000002")?.communityId, 7);
	assert.equal(storedValue("0000000000000003")?.communityId, 42);
});

test("Values without a community word land in the orphan bucket", () => {
	applyFrame(0x1n, makeValueFrame({ id: 0x1n }));

	assert.equal(storedValue("0000000000000001")?.communityId, -1);
});

test("A raw expired-ttl frame removes the Value from the store", () => {
	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 9n }));
	assert.equal(storeSize(), 1);

	applyFrame(
		0x2n,
		makeValueFrame({
			id: 0x2n,
			communityId: 9n,
			ttl: TTL_EXPIRED_SENTINEL_WORD,
		}),
	);

	assert.equal(storeSize(), 0);
});

test("decodeValueWireMessage still splits raw 1024-byte value frames", () => {
	const a = makeValueFrame({ id: 0x40n });
	const b = makeValueFrame({ id: 0x41n });
	const joined = new Uint8Array(a.byteLength + b.byteLength);
	joined.set(a, 0);
	joined.set(b, a.byteLength);

	const decoded = decodeValueWireMessage(joined);

	assert.equal(decoded.length, 2);
	assert.equal(decoded[0].valueId, 0x40n);
	assert.equal(decoded[1].valueId, 0x41n);
});
