import assert from "node:assert/strict";
import test from "node:test";
import { decodeValueFrame, ValueStore } from "./value-store";
import { VALUE_FRAME_BYTE_LENGTH, VALUE_WORD_COUNT, WORD } from "./valueLayout";

/*
PROPERTIES_COMMUNITY_WORD is absolute word 56, computed on the Go side as
propsStart (48) + communityIDOffset (8). The mesh routing layer stamps the
community index here via an ephemeral CopyMaskMerge program — the visualizer
just reads the same byte off the wire to recover the assignment.
*/
const PROPERTIES_COMMUNITY_WORD = 56;

function writeWord(frame: Uint8Array, wordIndex: number, word: bigint) {
	const offset = wordIndex * 8;
	const view = new DataView(frame.buffer, frame.byteOffset + offset, 8);
	view.setBigUint64(0, word, true);
}

function encodeInterleaved8x8(x: number, y: number): number {
	let code = 0;

	for (let bit = 0; bit < 8; bit++) {
		code |= ((x >> bit) & 1) << (2 * bit);
		code |= ((y >> bit) & 1) << (2 * bit + 1);
	}

	return code;
}

function writeTokenBytes(frame: Uint8Array, text: string) {
	let wordIndex = 0;
	let shift = 0n;
	let word = 0n;
	let ordinal = 0;

	for (const byte of new TextEncoder().encode(text)) {
		const code = BigInt(encodeInterleaved8x8(byte, ordinal));
		word |= code << shift;
		shift += 16n;
		ordinal++;

		if (shift === 64n) {
			writeWord(frame, wordIndex, word);
			wordIndex++;
			shift = 0n;
			word = 0n;
		}
	}

	if (shift > 0n) {
		writeWord(frame, wordIndex, word);
	}
}

function makeValueFrame(init?: {
	id?: bigint;
	prev?: bigint;
	next?: bigint;
	content?: string;
	communityId?: bigint;
}) {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);

	if (init?.id !== undefined) {
		writeWord(frame, WORD.ID, init.id);
	}

	if (init?.prev !== undefined) {
		writeWord(frame, WORD.PREV, init.prev);
	}

	if (init?.next !== undefined) {
		writeWord(frame, WORD.NEXT, init.next);
	}

	if (init?.content) {
		writeTokenBytes(frame, init.content);
	}

	if (init?.communityId !== undefined) {
		writeWord(frame, PROPERTIES_COMMUNITY_WORD, init.communityId);
	}

	return frame;
}

test("decodeValueFrame reads id, chain ids, and token text from raw Value bytes", () => {
	const frame = makeValueFrame({
		id: 0xabn,
		prev: 0x10n,
		next: 0x20n,
		content: "hello",
	});

	const decoded = decodeValueFrame(frame);

	assert.equal(decoded.id, "00000000000000ab");
	assert.equal(decoded.prevId, "0000000000000010");
	assert.equal(decoded.nextId, "0000000000000020");
	assert.equal(decoded.content, "hello");
	assert.equal(decoded.words.length, VALUE_WORD_COUNT);
	assert.equal(decoded.regions.id.words[0], 0xabn);
	assert.equal(decoded.regions.program.startWord, 16);
});

test("ValueStore attaches a pending frame when the value is created later", () => {
	const store = new ValueStore();
	const frame = makeValueFrame({
		id: 0x44n,
		prev: 0x11n,
		next: 0x22n,
		content: "tok",
	});

	store.applyWireFrame(0x44n, frame);
	store.ensure("0000000000000044", { role: "data" });

	const value = store.get("0000000000000044");

	assert.ok(value);
	assert.equal(value?.decoded?.content, "tok");
	assert.equal(value?.decoded?.prevId, "0000000000000011");
	assert.equal(value?.decoded?.nextId, "0000000000000022");
});

test("ValueStore updates the stored frame when a newer wire image arrives", () => {
	const store = new ValueStore();

	store.ensure("00000000000000ff", { role: "data" });
	store.applyWireFrame(0xffn, makeValueFrame({ id: 0xffn, content: "one" }));
	store.applyWireFrame(0xffn, makeValueFrame({ id: 0xffn, content: "two" }));

	const value = store.get("00000000000000ff");

	assert.ok(value);
	assert.equal(value?.decoded?.content, "two");
});

test("ValueStore reads community id from the on-wire properties word", () => {
	const store = new ValueStore();

	store.ensure("0000000000000001");
	store.applyWireFrame(
		0x1n,
		makeValueFrame({ id: 0x1n, communityId: 7n }),
	);

	store.ensure("0000000000000002");
	store.applyWireFrame(
		0x2n,
		makeValueFrame({ id: 0x2n, communityId: 7n }),
	);

	store.ensure("0000000000000003");
	store.applyWireFrame(
		0x3n,
		makeValueFrame({ id: 0x3n, communityId: 42n }),
	);

	const snapshot = store.getState().snapshot;

	assert.equal(snapshot.fields.length, 2);
	assert.equal(snapshot.orphanValues.length, 0);

	const field7 = snapshot.fields.find((field) => field.id === 7);
	const field42 = snapshot.fields.find((field) => field.id === 42);

	assert.ok(field7);
	assert.ok(field42);
	assert.equal(field7?.members.length, 2);
	assert.equal(field42?.members.length, 1);
});

test("Values without a community word land in orphanValues", () => {
	const store = new ValueStore();

	store.ensure("0000000000000001");
	store.applyWireFrame(0x1n, makeValueFrame({ id: 0x1n }));

	const snapshot = store.getState().snapshot;

	assert.equal(snapshot.fields.length, 0);
	assert.equal(snapshot.orphanValues.length, 1);
});
