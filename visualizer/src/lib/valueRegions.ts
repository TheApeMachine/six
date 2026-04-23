/*
valueRegions decodes a raw 1024-byte Value wire frame into named slices
that the inspector and classifier consume. Region boundaries come from
layoutGenerated.ts (REGION_SPECS), which the Go-side gen.go writes from
cmd/cfg/config.yml — so the slicing here always agrees with the kernel
addresses the substrate executes against. The instruction-stream
decoder consumes constants from programsGenerated.ts for the same
reason: the bit positions of the packed-uint64 instruction format are
defined once in pkg/compute/program/compiler.go and re-emitted by the
generator, so we never carry a parallel copy.
*/

import {
	AFFINITY_START_WORD,
	ASSET_START_WORD,
	REGION_SPECS as GENERATED_REGION_SPECS,
	type RegionSpec as GeneratedRegionSpec,
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
	type ValueRegionName,
} from "./layoutGenerated";
import {
	type DecodedInstruction,
	INSTR_A_SPAN_SHIFT,
	INSTR_A_START_SHIFT,
	INSTR_B_SPAN_SHIFT,
	INSTR_B_START_SHIFT,
	INSTR_DST_SPAN_SHIFT,
	INSTR_DST_START_SHIFT,
	INSTR_FIELD_MASK,
	INSTR_IMM_MASK,
	INSTR_IMM_SHIFT,
	INSTR_MODE_MASK,
	INSTR_MODE_SHIFT,
	INSTR_OPCODE_MASK,
	INSTR_OPCODE_SHIFT,
} from "./programsGenerated";

export type { ValueRegionName } from "./layoutGenerated";
export type { DecodedInstruction } from "./programsGenerated";

const WORD_BYTES = 8;

export function readWordU64LE(buf: Uint8Array, wordIndex: number): bigint {
	const offset = wordIndex * WORD_BYTES;

	if (offset + WORD_BYTES > buf.length) {
		return 0n;
	}

	const dv = new DataView(buf.buffer, buf.byteOffset + offset, WORD_BYTES);

	return dv.getBigUint64(0, true);
}

export function formatWordHex64(word: bigint): string {
	return word.toString(16).padStart(16, "0").toLowerCase();
}

/*
chainIdFromWord turns a non-zero link word into a 16-nibble id; zero
words are intentionally rendered as the empty string so the inspector
can distinguish "no link" from "id of zero".
*/
export function chainIdFromWord(word: bigint): string {
	if (word === 0n) {
		return "";
	}

	return formatWordHex64(word);
}

export interface ValueRegionSlice {
	name: ValueRegionName;
	/** Inclusive start index into the 128-word Value. */
	startWord: number;
	wordCount: number;
	words: bigint[];
}

export interface DecodedValueRegions {
	tokens: ValueRegionSlice;
	program: ValueRegionSlice;
	signals: ValueRegionSlice;
	context: ValueRegionSlice;
	gradient: ValueRegionSlice;
	properties: ValueRegionSlice;
	asset: ValueRegionSlice;
	prev: ValueRegionSlice;
	next: ValueRegionSlice;
	id: ValueRegionSlice;
	affinity: ValueRegionSlice;
	/** Stable iteration order (tokens → … → affinity). */
	all: ValueRegionSlice[];
}

/*
REGION_SPECS is generated from cmd/cfg/config.yml. The visualizer must
consume that table directly so the wire decoder, program classifier, and
inspector stay aligned with the same layout the kernels execute.
*/
export const REGION_SPECS: ReadonlyArray<
	Pick<GeneratedRegionSpec, "name" | "startWord" | "wordCount">
> = GENERATED_REGION_SPECS;

/*
decodeInstructionWord unpacks one 64-bit DSL instruction. Layout matches
pkg/compute/program/compiler.go EncodeInstruction; see programsGenerated.ts
for the bit shifts (the generator re-exports them so this file and the
runtime never disagree). Span fields are stored as `span - 1` so a fully
zero word means "halt"; we restore the +1 here.
*/
export function decodeInstructionWord(word: bigint): DecodedInstruction {
	return {
		dstSpan:
			Number((word >> BigInt(INSTR_DST_SPAN_SHIFT)) & INSTR_FIELD_MASK) + 1,
		dstStart: Number(
			(word >> BigInt(INSTR_DST_START_SHIFT)) & INSTR_FIELD_MASK,
		),
		bSpan: Number((word >> BigInt(INSTR_B_SPAN_SHIFT)) & INSTR_FIELD_MASK) + 1,
		bStart: Number((word >> BigInt(INSTR_B_START_SHIFT)) & INSTR_FIELD_MASK),
		aSpan: Number((word >> BigInt(INSTR_A_SPAN_SHIFT)) & INSTR_FIELD_MASK) + 1,
		aStart: Number((word >> BigInt(INSTR_A_START_SHIFT)) & INSTR_FIELD_MASK),
		opcode: Number((word >> BigInt(INSTR_OPCODE_SHIFT)) & INSTR_OPCODE_MASK),
		mode: Number((word >> BigInt(INSTR_MODE_SHIFT)) & INSTR_MODE_MASK),
		imm: Number((word >> BigInt(INSTR_IMM_SHIFT)) & INSTR_IMM_MASK),
	};
}

/*
decodeProgramWire walks the program region one packed uint64 word at a
time. The first all-zero word terminates execution per the compiler's
contract, so we stop there; trailing zero words (e.g. an unused tail of
the 16-word program region) never produce phantom instructions.

This used to assume the old "one word per operand" layout and read
opcode / srcA / srcB / dst from separate words; that was wrong as soon
as the runtime moved to the packed format and made every program look
like "ALU WIRE op=0x10" in the inspector. The packed walker keeps the
visualiser consistent with whatever the substrate actually executes.
*/
export function decodeProgramWire(
	program: ValueRegionSlice,
): DecodedInstruction[] {
	const out: DecodedInstruction[] = [];

	for (const word of program.words) {
		if (word === 0n) {
			break;
		}

		out.push(decodeInstructionWord(word));
	}

	return out;
}

function sliceWords(words: bigint[], start: number, count: number): bigint[] {
	const out: bigint[] = [];

	for (let i = 0; i < count; i++) {
		const idx = start + i;

		out.push(idx < words.length ? words[idx] : 0n);
	}

	return out;
}

/*
decodeValueRegions maps a full 128-word image into named slices. Pass
Value.Bytes-length buffers via wordsFromFrame first.
*/
export function decodeValueRegions(words: bigint[]): DecodedValueRegions {
	const slices: ValueRegionSlice[] = [];

	for (const spec of REGION_SPECS) {
		slices.push({
			name: spec.name,
			startWord: spec.startWord,
			wordCount: spec.wordCount,
			words: sliceWords(words, spec.startWord, spec.wordCount),
		});
	}

	const [
		tokens,
		program,
		signals,
		context,
		gradient,
		properties,
		asset,
		prev,
		next,
		id,
		affinity,
	] = slices;

	return {
		tokens,
		program,
		signals,
		context,
		gradient,
		properties,
		asset,
		prev,
		next,
		id,
		affinity,
		all: slices,
	};
}

/*
wordsFromFrame builds the 128-word bigint lane from a raw LE wire
frame. Short buffers zero-fill so callers can render partial frames
(e.g. during reconnect) without throwing.
*/
export function wordsFromFrame(frame: Uint8Array): bigint[] {
	if (frame.byteLength < VALUE_FRAME_BYTE_LENGTH) {
		return Array.from({ length: VALUE_WORD_COUNT }, () => 0n);
	}

	return Array.from({ length: VALUE_WORD_COUNT }, (_, wordIndex) =>
		readWordU64LE(frame, wordIndex),
	);
}

export function decodeValueRegionsFromFrame(
	frame: Uint8Array,
): DecodedValueRegions {
	return decodeValueRegions(wordsFromFrame(frame));
}

/*
formatWordHexAt resolves one absolute word index to display hex,
preferring pre-sliced regions when present so UI code does not duplicate
readWordU64LE.
*/
export function formatWordHexAt(
	regions: DecodedValueRegions | null,
	frame: Uint8Array | null,
	wordIndex: number,
): string | null {
	if (regions) {
		for (const slice of regions.all) {
			if (
				wordIndex >= slice.startWord &&
				wordIndex < slice.startWord + slice.wordCount
			) {
				return formatWordHex64(slice.words[wordIndex - slice.startWord]);
			}
		}
	}

	if (!frame || frame.byteLength < VALUE_FRAME_BYTE_LENGTH) {
		return null;
	}

	return formatWordHex64(readWordU64LE(frame, wordIndex));
}

/*
affinityHexFromRegions concatenates the five 64-bit affinity words into
the same 80-nibble string the Go-side telemetry exporter produces — that
way the inspector's affinity panel matches the field's affinityHex byte
for byte.
*/
export function affinityHexFromRegions(regions: DecodedValueRegions): string {
	return regions.affinity.words.map((word) => formatWordHex64(word)).join("");
}

/*
affinityHexWords reads the affinity words straight off a frame buffer
(without going through a region decode). Useful when callers already
have the raw frame in hand and want the same display string the
inspector shows.
*/
export function affinityHexWords(frame: Uint8Array): string {
	let out = "";

	for (let w = 0; w < 5; w++) {
		out += formatWordHex64(readWordU64LE(frame, AFFINITY_START_WORD + w));
	}

	return out;
}

export interface ChainPreview {
	prevCommitted: string;
	nextCommitted: string;
	prevStaged: string;
	nextStaged: string;
	idHex: string;
}

function chainPreviewFromRegions(regions: DecodedValueRegions): ChainPreview {
	return {
		prevCommitted: chainIdFromWord(regions.prev.words[0]),
		nextCommitted: chainIdFromWord(regions.next.words[0]),
		/*
		The orchestrator stages predecessor/successor IDs into the first
		two Asset words before the link firmware copies them into Prev /
		Next. We expose both so the inspector can show "staged but not
		yet committed" — useful when chasing a chain that hasn't run the
		link program yet.
		*/
		prevStaged: chainIdFromWord(regions.asset.words[0]),
		nextStaged: chainIdFromWord(regions.asset.words[1]),
		idHex: formatWordHex64(regions.id.words[0]),
	};
}

function chainPreviewFromFrame(frame: Uint8Array): ChainPreview {
	return {
		prevCommitted: chainIdFromWord(readWordU64LE(frame, PREV_START_WORD)),
		nextCommitted: chainIdFromWord(readWordU64LE(frame, NEXT_START_WORD)),
		prevStaged: chainIdFromWord(readWordU64LE(frame, ASSET_START_WORD)),
		nextStaged: chainIdFromWord(readWordU64LE(frame, ASSET_START_WORD + 1)),
		idHex: formatWordHex64(readWordU64LE(frame, ID_START_WORD)),
	};
}

export function chainPreview(
	regions: DecodedValueRegions | null,
	frame: Uint8Array | null,
	frameOk: boolean,
): ChainPreview {
	if (regions) {
		return chainPreviewFromRegions(regions);
	}

	if (!frameOk || !frame || frame.byteLength < VALUE_FRAME_BYTE_LENGTH) {
		return {
			prevCommitted: "",
			nextCommitted: "",
			prevStaged: "",
			nextStaged: "",
			idHex: "",
		};
	}

	return chainPreviewFromFrame(frame);
}
