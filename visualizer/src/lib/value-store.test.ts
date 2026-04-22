import assert from "node:assert/strict";
import test from "node:test";
import { VALUE_FRAME_BYTE_LENGTH, VALUE_WORD_COUNT } from "./layoutGenerated";
import type { FieldMetricsPayload } from "./value-store";
import { decodeValueFrame, ValueStore } from "./value-store";
import { WORD } from "./valueLayout";
import { decodeValueWireMessage } from "./wire";

/*
PROPERTIES_COMMUNITY_WORD is absolute word 64, computed on the Go side as
PropertiesStartWord (56) + COMMUNITY offset (8). mesh.Field.Write stamps
the leaf field's ID here directly onto the visitor's wire frame before
forwarding it through the post-routing telemetry pulse — the visualizer
just reads the same byte off the wire to recover the assignment.
*/
const PROPERTIES_COMMUNITY_WORD = 64;

/*
Causal residue word indices mirror value-store.ts — keeping them here keeps
the tests decoupled from the module under test so a silent drift in either
place trips an assertion rather than passing silently. See pkg/compute/kernel
for the Go-side source of truth (PropertiesStartWord = 56, AssetStartWord =
72, ContextStartWord = 40).
*/
const PROPERTIES_REFUTATION_TARGET_WORD = 57;
const PROPERTIES_NOISE_WORD = 60;
const ASSET_GRADIENT_WORD = 88; // kernel AssetStartWord + 16
const CONTEXT_START_WORD = 40;
const FALSIFIED_BIT = 1n << 62n;

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
	refutationTarget?: bigint;
	noise?: bigint;
	gradientWord?: bigint;
	contextWord?: bigint;
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

	if (init?.refutationTarget !== undefined) {
		writeWord(frame, PROPERTIES_REFUTATION_TARGET_WORD, init.refutationTarget);
	}

	if (init?.noise !== undefined) {
		writeWord(frame, PROPERTIES_NOISE_WORD, init.noise);
	}

	if (init?.gradientWord !== undefined) {
		writeWord(frame, ASSET_GRADIENT_WORD, init.gradientWord);
	}

	if (init?.contextWord !== undefined) {
		writeWord(frame, CONTEXT_START_WORD, init.contextWord);
	}

	return frame;
}

const blankMetrics: FieldMetricsPayload = {
	communityIdx: 0,
	memberCount: 0,
	labeledCount: 0,
	slotSum: 0,
	coverage: 0,
	consensus: 0,
	labelDensity: 0,
	crystallization: 0,
	dominantRatio: 0,
	modeCount: 0,
	pressureMult: 1,
	saturated: false,
};

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
	store.applyWireFrame(0x1n, makeValueFrame({ id: 0x1n, communityId: 7n }));

	store.ensure("0000000000000002");
	store.applyWireFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 7n }));

	store.ensure("0000000000000003");
	store.applyWireFrame(0x3n, makeValueFrame({ id: 0x3n, communityId: 42n }));

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

test("readCausalState surfaces hypothesizing when refutation target is armed", () => {
	const store = new ValueStore();

	store.ensure("0000000000000010");
	store.applyWireFrame(
		0x10n,
		makeValueFrame({
			id: 0x10n,
			communityId: 3n,
			refutationTarget: 0xdeadbeefn,
		}),
	);

	const stored = store.get("0000000000000010");
	assert.ok(stored);
	assert.equal(stored?.causal.hypothesizing, true);
	assert.equal(stored?.causal.falsified, false);
	assert.equal(stored?.causal.intervening, false);
});

test("readCausalState surfaces falsified when FalsifiedBit is set in the noise word", () => {
	const store = new ValueStore();

	store.ensure("0000000000000011");
	store.applyWireFrame(
		0x11n,
		makeValueFrame({
			id: 0x11n,
			communityId: 3n,
			noise: FALSIFIED_BIT | 0x123n,
		}),
	);

	const stored = store.get("0000000000000011");
	assert.ok(stored);
	assert.equal(stored?.causal.falsified, true);
	// hypothesizing/intervening independent — a frame may be falsified
	// without the target being re-armed after ApplyRefutationProbe clears
	// it, so these must stay false under a bare noise-only write.
	assert.equal(stored?.causal.hypothesizing, false);
	assert.equal(stored?.causal.intervening, false);
});

test("readCausalState surfaces intervening only when gradient+context are set and prev is zero", () => {
	const store = new ValueStore();

	// Intervening frame: gradient word set, context word set, prev = 0.
	store.ensure("0000000000000012");
	store.applyWireFrame(
		0x12n,
		makeValueFrame({
			id: 0x12n,
			communityId: 3n,
			gradientWord: 0xf0f0n,
			contextWord: 0x0f0fn,
		}),
	);
	const intervening = store.get("0000000000000012");
	assert.ok(intervening);
	assert.equal(intervening?.causal.intervening, true);

	// Same bits but prev present: do_intervention did NOT sever history,
	// so the intervening residue must stay false.
	store.ensure("0000000000000013");
	store.applyWireFrame(
		0x13n,
		makeValueFrame({
			id: 0x13n,
			communityId: 3n,
			prev: 0x9n,
			gradientWord: 0xf0f0n,
			contextWord: 0x0f0fn,
		}),
	);
	const linked = store.get("0000000000000013");
	assert.ok(linked);
	assert.equal(linked?.causal.intervening, false);

	// Context missing: the kernel XOR into local context never landed, so
	// we refuse to report intervening even with a severed chain.
	store.ensure("0000000000000014");
	store.applyWireFrame(
		0x14n,
		makeValueFrame({
			id: 0x14n,
			communityId: 3n,
			gradientWord: 0xf0f0n,
		}),
	);
	const gradientOnly = store.get("0000000000000014");
	assert.ok(gradientOnly);
	assert.equal(gradientOnly?.causal.intervening, false);
});

test("A blank frame reports no causal residues at all", () => {
	const store = new ValueStore();

	store.ensure("0000000000000015");
	store.applyWireFrame(0x15n, makeValueFrame({ id: 0x15n, communityId: 3n }));

	const stored = store.get("0000000000000015");
	assert.ok(stored);
	assert.equal(stored?.causal.hypothesizing, false);
	assert.equal(stored?.causal.falsified, false);
	assert.equal(stored?.causal.intervening, false);
});

test("applyFieldMetricsEnvelope merges crystallization into the matching FieldSnapshot", () => {
	const store = new ValueStore();

	store.ensure("0000000000000020");
	store.applyWireFrame(0x20n, makeValueFrame({ id: 0x20n, communityId: 5n }));
	store.ensure("0000000000000021");
	store.applyWireFrame(
		0x21n,
		makeValueFrame({
			id: 0x21n,
			communityId: 5n,
			refutationTarget: 0x1n,
		}),
	);

	store.applyFieldMetricsEnvelope({
		...blankMetrics,
		communityIdx: 5,
		memberCount: 2,
		coverage: 0.8,
		consensus: 0.9,
		labelDensity: 0.5,
		crystallization: 0.36,
		dominantRatio: 0.75,
		modeCount: 2,
		pressureMult: 1.25,
		saturated: true,
	});

	const snapshot = store.getState().snapshot;
	const field = snapshot.fields.find((candidate) => candidate.id === 5);

	assert.ok(field);
	assert.equal(field?.coverage, 0.8);
	assert.equal(field?.consensus, 0.9);
	assert.equal(field?.labelDensity, 0.5);
	assert.equal(field?.crystallization, 0.36);
	assert.equal(field?.dominantRatio, 0.75);
	assert.equal(field?.modeCount, 2);
	assert.equal(field?.pressureMult, 1.25);
	assert.equal(field?.saturated, true);
	// Legacy alias stays wired to crystallization so old HUD widgets
	// keep reading the same "is the field crystallising" axis.
	assert.equal(field?.saturation, 0.36);
	// Per-community causal tallies are derived from member state, not
	// the envelope, and must reflect the one Value with a refutation
	// target armed.
	assert.equal(field?.hypothesizingCount, 1);
	assert.equal(field?.falsifiedCount, 0);
	assert.equal(field?.interveningCount, 0);
});

test("applyFieldMetricsEnvelope with negative communityIdx leaves the cache untouched", () => {
	const store = new ValueStore();

	store.ensure("0000000000000022");
	store.applyWireFrame(0x22n, makeValueFrame({ id: 0x22n, communityId: 9n }));

	store.applyFieldMetricsEnvelope({
		...blankMetrics,
		communityIdx: 9,
		crystallization: 0.5,
	});

	store.applyFieldMetricsEnvelope({
		...blankMetrics,
		communityIdx: -1,
		crystallization: 0.1,
	});

	const snapshot = store.getState().snapshot;
	const field = snapshot.fields.find((candidate) => candidate.id === 9);

	assert.ok(field);
	// The second call carried the sentinel communityIdx the Go side
	// emits when metrics aren't ready yet; the cached value for
	// community 9 must survive that no-op.
	assert.equal(field?.crystallization, 0.5);
});

test("FieldSnapshot defaults to zero metrics when no envelope has arrived", () => {
	const store = new ValueStore();

	store.ensure("0000000000000023");
	store.applyWireFrame(0x23n, makeValueFrame({ id: 0x23n, communityId: 11n }));

	const snapshot = store.getState().snapshot;
	const field = snapshot.fields.find((candidate) => candidate.id === 11);

	assert.ok(field);
	assert.equal(field?.coverage, 0);
	assert.equal(field?.consensus, 0);
	assert.equal(field?.labelDensity, 0);
	assert.equal(field?.crystallization, 0);
	assert.equal(field?.modeCount, 0);
	assert.equal(field?.saturated, false);
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
