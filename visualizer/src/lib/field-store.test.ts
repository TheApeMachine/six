import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import {
	applyValueFrames,
	fieldStore,
	getFieldTelemetryState,
	resetFieldStore,
} from "./field-store";
import {
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
	SIGNALS_START_WORD,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
} from "./layoutGenerated";
import { PROPERTY_WORD, VALUE_ROLE } from "./propertiesGenerated";
import { decodeValueFrame, formatValueId } from "./value-frame";
import { decodeValueWireMessage } from "./wire";

/*
Property word indices mirror what value-frame.ts reads from
propertiesGenerated.ts. We re-derive them through PROPERTY_WORD here so
the tests stay anchored to pkg/primitive/properties.go via the generator —
any drift in the iota or the PROPERTIES_START_WORD anchor trips this
table instead of silently passing.
*/
const PROPERTIES_LABELS_WORD = PROPERTY_WORD("LABELS");
const PROPERTIES_COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const PROPERTIES_NOISE_WORD = PROPERTY_WORD("NOISE");
const PROPERTIES_ROLE_WORD = PROPERTY_WORD("ROLE");
const PROPERTIES_TTL_WORD = PROPERTY_WORD("TTL");
const PROPERTIES_REFUTATION_TARGET_WORD = PROPERTY_WORD("TARGET");
const SIGNALS_FALSIFIED_WORD = SIGNALS_START_WORD + 7;
const ASSET_GRADIENT_WORD = 88; // kernel AssetStartWord + 16
const CONTEXT_START_WORD = 40;
const TTL_EXPIRED_SENTINEL_WORD = (1n << 64n) - 1n;
const TELEMETRY_RUN_MARKER_MAGIC = 0x73697872756e3031n;

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
	labels?: bigint;
	communityId?: bigint;
	role?: bigint;
	refutationTarget?: bigint;
	noise?: bigint;
	ttl?: bigint;
	signalsWord7?: bigint;
	gradientWord?: bigint;
	contextWord?: bigint;
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

	if (init?.content) {
		writeTokenBytes(frame, init.content);
	}

	if (init?.labels !== undefined) {
		writeWord(frame, PROPERTIES_LABELS_WORD, init.labels);
	}

	if (init?.communityId !== undefined) {
		writeWord(frame, PROPERTIES_COMMUNITY_WORD, init.communityId);
	}

	if (init?.role !== undefined) {
		writeWord(frame, PROPERTIES_ROLE_WORD, init.role);
	}

	if (init?.refutationTarget !== undefined) {
		writeWord(frame, PROPERTIES_REFUTATION_TARGET_WORD, init.refutationTarget);
	}

	if (init?.noise !== undefined) {
		writeWord(frame, PROPERTIES_NOISE_WORD, init.noise);
	}

	if (init?.ttl !== undefined) {
		writeWord(frame, PROPERTIES_TTL_WORD, init.ttl);
	}

	if (init?.signalsWord7 !== undefined) {
		writeWord(frame, SIGNALS_FALSIFIED_WORD, init.signalsWord7);
	}

	if (init?.gradientWord !== undefined) {
		writeWord(frame, ASSET_GRADIENT_WORD, init.gradientWord);
	}

	if (init?.contextWord !== undefined) {
		writeWord(frame, CONTEXT_START_WORD, init.contextWord);
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

function currentSnapshot() {
	return getFieldTelemetryState().snapshot;
}

function labelWord(...labels: number[]) {
	let word = 0n;

	for (let slot = 0; slot < labels.length && slot < 4; slot++) {
		word |= BigInt(labels[slot] & 0xffff) << BigInt(slot * 16);
	}

	return word;
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

test("fieldStore creates a stored value directly from a raw frame", () => {
	const frame = makeValueFrame({
		id: 0x44n,
		prev: 0x11n,
		next: 0x22n,
		content: "tok",
	});

	const value = applyFrame(0x44n, frame);

	assert.equal(storeSize(), 1);
	assert.equal(value?.id, "0000000000000044");
	assert.equal(value?.decoded?.content, "tok");
	assert.equal(value?.decoded?.prevId, "0000000000000011");
	assert.equal(value?.decoded?.nextId, "0000000000000022");
});

test("fieldStore updates the stored frame when a newer wire image arrives", () => {

	applyFrame(0xffn, makeValueFrame({ id: 0xffn, content: "one" }));
	applyFrame(0xffn, makeValueFrame({ id: 0xffn, content: "two" }));

	const value = storedValue("00000000000000ff");

	assert.ok(value);
	assert.equal(value?.decoded?.content, "two");
});

test("fieldStore keys updates by ValueID instead of creating decoded-id copies", () => {
	applyFrame(0x44n, makeValueFrame({ id: 0x44n, content: "one" }));
	applyFrame(0x44n, makeValueFrame({ id: 0x99n, content: "two" }));

	const value = storedValue("0000000000000044");

	assert.equal(storeSize(), 1);
	assert.ok(value);
	assert.equal(value?.id, "0000000000000044");
	assert.equal(value?.decoded?.id, "0000000000000099");
	assert.equal(value?.decoded?.content, "two");
	assert.equal(storedValue("0000000000000099"), undefined);
});

test("fieldStore clears stale Values when a run marker arrives", () => {
	applyFrame(0x1n, makeValueFrame({ id: 0x1n, content: "old" }));
	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 0x20n }));

	assert.equal(storeSize(), 2);

	applyValueFrames([{ valueId: 0n, bytes: makeRunMarkerFrame() }]);

	assert.equal(storeSize(), 0);
	assert.equal(currentSnapshot().orphanValues.length, 0);
	assert.equal(currentSnapshot().fields.length, 0);
});

test("fieldStore reads community id from the on-wire properties word", () => {

	applyFrame(0x1n, makeValueFrame({ id: 0x1n, communityId: 7n }));

	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 7n }));

	applyFrame(0x3n, makeValueFrame({ id: 0x3n, communityId: 42n }));

	const snapshot = currentSnapshot();

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

	applyFrame(0x1n, makeValueFrame({ id: 0x1n, content: "payload" }));

	const snapshot = currentSnapshot();

	assert.equal(snapshot.fields.length, 0);
	assert.equal(snapshot.orphanValues.length, 1);
});

test("Settled empty frames are not rendered as data orphans", () => {
	applyFrame(0x1n, makeValueFrame({ id: 0x1n }));

	const snapshot = currentSnapshot();

	assert.equal(storeSize(), 1);
	assert.equal(snapshot.fields.length, 0);
	assert.equal(snapshot.orphanValues.length, 0);
});

test("A Value that joins a community is not also rendered as an orphan", () => {

	applyFrame(0x1n, makeValueFrame({ id: 0x1n }));
	applyFrame(0x1n, makeValueFrame({ id: 0x1n, communityId: 7n }));

	const snapshot = currentSnapshot();
	const rendered =
		snapshot.orphanValues.length +
		snapshot.fields.reduce((sum, field) => sum + field.members.length, 0);

	assert.equal(snapshot.totalValues, 1);
	assert.equal(rendered, snapshot.totalValues);
	assert.equal(snapshot.orphanValues.length, 0);
	assert.equal(snapshot.fields[0]?.members[0]?.id, "0000000000000001");
});

test("A raw expired-ttl frame removes the Value from the visualizer", () => {

	applyFrame(0x2n, makeValueFrame({ id: 0x2n, communityId: 9n }));

	let snapshot = currentSnapshot();
	assert.equal(storeSize(), 1);
	assert.equal(snapshot.fields.length, 1);
	assert.equal(snapshot.fields[0]?.members.length, 1);

	applyFrame(
		0x2n,
		makeValueFrame({
			id: 0x2n,
			communityId: 9n,
			ttl: TTL_EXPIRED_SENTINEL_WORD,
		}),
	);

	snapshot = currentSnapshot();

	assert.equal(storeSize(), 0);
	assert.equal(snapshot.totalValues, 0);
	assert.equal(snapshot.fields.length, 0);
	assert.equal(snapshot.orphanValues.length, 0);
});

test("Completed community recruiters stay visible after their program clears", () => {

	applyFrame(
		0x30n,
		makeValueFrame({
			id: 0x30n,
			communityId: 0x30n,
			role: BigInt(VALUE_ROLE.Programmer),
		}),
	);

	const stored = storedValue("0000000000000030");

	assert.ok(stored);
	assert.equal(stored?.classification.program, "recruit_community");
	assert.equal(stored?.classification.category, "recruiter");
});

test("Completed community recruiters do not require a role side channel", () => {

	applyFrame(
		0x31n,
		makeValueFrame({
			id: 0x31n,
			communityId: 0x31n,
		}),
	);

	const stored = storedValue("0000000000000031");

	assert.ok(stored);
	assert.equal(stored?.classification.program, "recruit_community");
	assert.equal(stored?.classification.category, "recruiter");
});

test("readCausalState surfaces hypothesizing when refutation target is armed", () => {

	applyFrame(
		0x10n,
		makeValueFrame({
			id: 0x10n,
			communityId: 3n,
			refutationTarget: 0xdeadbeefn,
		}),
	);

	const stored = storedValue("0000000000000010");
	assert.ok(stored);
	assert.equal(stored?.causal.hypothesizing, true);
	assert.equal(stored?.causal.falsified, false);
	assert.equal(stored?.causal.intervening, false);
});

test("readCausalState surfaces falsified when NOISE carries a witness", () => {

	applyFrame(
		0x11n,
		makeValueFrame({
			id: 0x11n,
			communityId: 3n,
			noise: 0x123n,
		}),
	);

	const stored = storedValue("0000000000000011");
	assert.ok(stored);
	assert.equal(stored?.causal.falsified, true);
	// hypothesizing/intervening independent — a frame may be falsified
	// without the target being re-armed, so these must stay false under a
	// bare noise-only write.
	assert.equal(stored?.causal.hypothesizing, false);
	assert.equal(stored?.causal.intervening, false);
});

test("readCausalState ignores generic signals[7] residue", () => {

	applyFrame(
		0x16n,
		makeValueFrame({
			id: 0x16n,
			communityId: 3n,
			signalsWord7: 0x123n,
		}),
	);

	const stored = storedValue("0000000000000016");

	assert.ok(stored);
	assert.equal(stored?.causal.falsified, false);
});

test("readCausalState surfaces intervening only when gradient+context are set and prev is zero", () => {

	// Intervening frame: gradient word set, context word set, prev = 0.
	applyFrame(
		0x12n,
		makeValueFrame({
			id: 0x12n,
			communityId: 3n,
			gradientWord: 0xf0f0n,
			contextWord: 0x0f0fn,
		}),
	);
	const intervening = storedValue("0000000000000012");
	assert.ok(intervening);
	assert.equal(intervening?.causal.intervening, true);

	// Same bits but prev present: do_intervention did NOT sever history,
	// so the intervening residue must stay false.
	applyFrame(
		0x13n,
		makeValueFrame({
			id: 0x13n,
			communityId: 3n,
			prev: 0x9n,
			gradientWord: 0xf0f0n,
			contextWord: 0x0f0fn,
		}),
	);
	const linked = storedValue("0000000000000013");
	assert.ok(linked);
	assert.equal(linked?.causal.intervening, false);

	// Context missing: the kernel XOR into local context never landed, so
	// we refuse to report intervening even with a severed chain.
	applyFrame(
		0x14n,
		makeValueFrame({
			id: 0x14n,
			communityId: 3n,
			gradientWord: 0xf0f0n,
		}),
	);
	const gradientOnly = storedValue("0000000000000014");
	assert.ok(gradientOnly);
	assert.equal(gradientOnly?.causal.intervening, false);
});

test("A blank frame reports no causal residues at all", () => {

	applyFrame(0x15n, makeValueFrame({ id: 0x15n, communityId: 3n }));

	const stored = storedValue("0000000000000015");
	assert.ok(stored);
	assert.equal(stored?.causal.hypothesizing, false);
	assert.equal(stored?.causal.falsified, false);
	assert.equal(stored?.causal.intervening, false);
});

test("FieldSnapshot derives crystallization from raw Value label slots", () => {

	applyFrame(
		0x20n,
		makeValueFrame({
			id: 0x20n,
			communityId: 5n,
			labels: labelWord(7),
		}),
	);
	applyFrame(
		0x21n,
		makeValueFrame({
			id: 0x21n,
			communityId: 5n,
			labels: labelWord(7),
			refutationTarget: 0x1n,
		}),
	);

	const snapshot = currentSnapshot();
	const field = snapshot.fields.find((candidate) => candidate.id === 5);

	assert.ok(field);
	assert.equal(field?.coverage, 1);
	assert.equal(field?.consensus, 1);
	assert.equal(field?.labelDensity, 0.25);
	assert.equal(field?.crystallization, 0.25);
	assert.equal(field?.dominantRatio, 1);
	assert.equal(field?.modeCount, 1);
	assert.equal(field?.pressureMult, 0);
	assert.equal(field?.saturated, false);
	// Legacy alias stays wired to crystallization so old HUD widgets
	// keep reading the same "is the field crystallising" axis.
	assert.equal(field?.saturation, 0.25);
	// Per-community causal tallies are derived from member state and
	// must reflect the one Value with a refutation target armed.
	assert.equal(field?.hypothesizingCount, 1);
	assert.equal(field?.falsifiedCount, 0);
	assert.equal(field?.interveningCount, 0);
});

test("FieldSnapshot reports zero crystallization for unlabeled raw Values", () => {

	applyFrame(0x23n, makeValueFrame({ id: 0x23n, communityId: 11n }));

	const snapshot = currentSnapshot();
	const field = snapshot.fields.find((candidate) => candidate.id === 11);

	assert.ok(field);
	assert.equal(field?.coverage, 0);
	assert.equal(field?.consensus, 0);
	assert.equal(field?.labelDensity, 0);
	assert.equal(field?.crystallization, 0);
	assert.equal(field?.dominantRatio, 0);
	assert.equal(field?.modeCount, 0);
	assert.equal(field?.pressureMult, 0);
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
