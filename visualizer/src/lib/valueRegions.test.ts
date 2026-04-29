import assert from "node:assert/strict";
import test from "node:test";
import { VALUE_FRAME_BYTE_LENGTH, VALUE_WORD_COUNT } from "./layoutGenerated";
import {
	chainPreview,
	decodeInstructionWord,
	decodeProgramWire,
	decodeValueRegionsFromFrame,
	formatWordHexAt,
	REGION_SPECS,
	wordsFromFrame,
} from "./valueRegions";

function writeWord(frame: Uint8Array, wordIndex: number, word: bigint) {
	const offset = wordIndex * 8;
	const view = new DataView(frame.buffer, frame.byteOffset + offset, 8);
	view.setBigUint64(0, word, true);
}

test("REGION_SPECS covers 128 words contiguously", () => {
	let expected = 0;

	for (const spec of REGION_SPECS) {
		assert.equal(spec.startWord, expected);
		expected += spec.wordCount;
	}

	assert.equal(expected, VALUE_WORD_COUNT);
});

/*
encodeInstruction mirrors pkg/compute/program/compiler.go's
EncodeInstruction. We re-derive the bit packing here in the test so any
drift between the compiler and the decoder trips an assertion instead
of silently misclassifying programs in the visualiser.
*/
function encodeInstruction(opts: {
	aStart?: number;
	aSpan?: number;
	bStart?: number;
	bSpan?: number;
	dstStart?: number;
	dstSpan?: number;
	opcode?: number;
	mode?: number;
	topology?: number;
	predStart?: number;
	predCond?: number;
	aInd?: number;
	bType?: number;
	predicate?: number;
	emit?: number;
	srcAFromB?: number;
	stage?: number;
	popEnd?: number;
}): bigint {
	const f = (v: number) => BigInt(v) & 0x7fn;
	const op = BigInt(opts.opcode ?? 0) & 0xfn;
	const mode = BigInt(opts.mode ?? 0) & 0x3n;
	const topology = BigInt(opts.topology ?? 0) & 0x3n;
	const predStart = BigInt(opts.predStart ?? 0) & 0x7fn;
	const predCond = BigInt(opts.predCond ?? 0) & 0x7n;
	const predicate = BigInt(opts.predicate ?? 0) & 0x1n;
	const emit = BigInt(opts.emit ?? 0) & 0x1n;
	const srcAFromB = BigInt(opts.srcAFromB ?? 0) & 0x1n;
	const stage = BigInt(opts.stage ?? 0) & 0x1n;
	const popEnd = BigInt(opts.popEnd ?? 0) & 0x1n;

	return (
		op |
		(f(opts.aStart ?? 0) << 4n) |
		(f((opts.aSpan ?? 1) - 1) << 11n) |
		(f(opts.bStart ?? 0) << 18n) |
		(f((opts.bSpan ?? 1) - 1) << 25n) |
		(f(opts.dstStart ?? 0) << 32n) |
		(f((opts.dstSpan ?? 1) - 1) << 39n) |
		(predStart << 46n) |
		(mode << 53n) |
		(emit << 54n) |
		(topology << 55n) |
		(predicate << 57n) |
		(predCond << 58n) |
		(srcAFromB << 61n) |
		(stage << 62n) |
		(popEnd << 63n)
	);
}

test("decodeInstructionWord round-trips the packed compiler format", () => {
	const word = encodeInstruction({
		aStart: 16,
		aSpan: 4,
		bStart: 48,
		bSpan: 8,
		dstStart: 32,
		dstSpan: 1,
		opcode: 0x6,
		mode: 2,
		emit: 1,
		topology: 0,
		predStart: 0,
		predCond: 0,
		aInd: 0,
		bType: 0,
	});

	const decoded = decodeInstructionWord(word);

	assert.deepEqual(decoded, {
		aStart: 16,
		aSpan: 4,
		bStart: 48,
		bSpan: 8,
		dstStart: 32,
		dstSpan: 1,
		opcode: 0x6,
		mode: 2,
		topology: 0,
		predStart: 0,
		predCond: 0,
		aInd: 0,
		bType: 0,
		predicate: 0,
		emit: 1,
		srcAFromB: 0,
		stage: 0,
		popEnd: 0,
	});
});

test("decodeInstructionWord preserves raw geometric opcodes", () => {
	const decoded = decodeInstructionWord(0x10n);

	assert.deepEqual(decoded, {
		aStart: 0,
		aSpan: 0,
		bStart: 0,
		bSpan: 0,
		dstStart: 0,
		dstSpan: 0,
		opcode: 0x10,
		mode: 0,
		topology: 0,
		predStart: 0,
		predCond: 0,
		aInd: 0,
		bType: 0,
		predicate: 0,
		emit: 0,
		srcAFromB: 0,
		stage: 0,
		popEnd: 0,
	});
});

test("decodeProgramWire stops at the first zero word", () => {
	const first = encodeInstruction({
		aStart: 0,
		aSpan: 8,
		bStart: 8,
		bSpan: 8,
		dstStart: 32,
		dstSpan: 4,
		opcode: 0x6,
		mode: 0,
	});
	const second = encodeInstruction({
		aStart: 0,
		aSpan: 8,
		bStart: 8,
		bSpan: 8,
		dstStart: 36,
		dstSpan: 4,
		opcode: 0x1,
		mode: 0,
	});

	const program = {
		name: "program" as const,
		startWord: 16,
		wordCount: 8,
		words: [first, second, 0n, 0n, 0n, 0n, 0n, 0n],
	};

	const decoded = decodeProgramWire(program);

	assert.equal(decoded.length, 2);
	assert.equal(decoded[0].opcode, 0x6);
	assert.equal(decoded[0].dstSpan, 4);
	assert.equal(decoded[1].opcode, 0x1);
});

test("decodeValueRegionsFromFrame slices named regions", () => {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);
	writeWord(frame, 122, 0xbeefn);

	const regions = decodeValueRegionsFromFrame(frame);

	assert.equal(regions.id.words[0], 0xbeefn);
	assert.equal(regions.tokens.wordCount, 16);
	assert.equal(regions.properties.wordCount, 16);
	assert.equal(regions.asset.wordCount, 48);
	assert.equal(regions.asset.startWord, 72);
	assert.equal(regions.all.length, REGION_SPECS.length);
});

test("wordsFromFrame zero-fills short buffers", () => {
	const short = new Uint8Array(8);
	const words = wordsFromFrame(short);

	assert.equal(words.length, VALUE_WORD_COUNT);
	assert.equal(words[0], 0n);
});

test("formatWordHexAt uses region slices when provided", () => {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);
	writeWord(frame, 122, 0xfeedn);
	const regions = decodeValueRegionsFromFrame(frame);

	assert.equal(formatWordHexAt(regions, frame, 122), "000000000000feed");
	assert.equal(formatWordHexAt(null, frame, 122), "000000000000feed");
});

test("chainPreview matches frame reads when regions absent", () => {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);
	writeWord(frame, 120, 0x10n);
	writeWord(frame, 121, 0x20n);
	writeWord(frame, 122, 0x30n);

	const fromRegions = chainPreview(
		decodeValueRegionsFromFrame(frame),
		frame,
		true,
	);
	const fromFrameOnly = chainPreview(null, frame, true);

	assert.equal(fromRegions.prevCommitted, "0000000000000010");
	assert.equal(fromFrameOnly.prevCommitted, "0000000000000010");
});
