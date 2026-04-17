import assert from "node:assert/strict";
import test from "node:test";
import { VALUE_FRAME_BYTE_LENGTH, VALUE_WORD_COUNT } from "./valueLayout";
import {
	chainPreview,
	decodeProgramWire,
	decodeValueRegionsFromFrame,
	formatWordHexAt,
	REGION_SPECS,
	unpackRegionRef,
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

test("unpackRegionRef matches kernel PackRegionRef layout", () => {
	const word = 0x0000_0005n | (0x0000_0003n << 32n);
	const u = unpackRegionRef(word);

	assert.equal(u.start, 5);
	assert.equal(u.span, 3);
});

test("decodeProgramWire reads opcode and region refs", () => {
	const program = {
		name: "program" as const,
		startWord: 16,
		wordCount: 8,
		words: [
			0x06n,
			0n,
			0n,
			0x0000_0010n | (0x0000_0004n << 32n),
			0n,
			0n,
			0n,
			0n,
		],
	};

	const decoded = decodeProgramWire(program);

	assert.equal(decoded.opcodeLow, 0x06);
	assert.equal(decoded.srcA.start, 16);
	assert.equal(decoded.srcA.span, 4);
});

test("decodeValueRegionsFromFrame slices named regions", () => {
	const frame = new Uint8Array(VALUE_FRAME_BYTE_LENGTH);
	writeWord(frame, 122, 0xbeefn);

	const regions = decodeValueRegionsFromFrame(frame);

	assert.equal(regions.id.words[0], 0xbeefn);
	assert.equal(regions.tokens.wordCount, 16);
	assert.equal(regions.properties.wordCount, 16);
	assert.equal(regions.asset.wordCount, 56);
	assert.equal(regions.asset.startWord, 64);
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
