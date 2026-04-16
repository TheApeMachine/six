/*
Region layout matches pkg/core/config.go Value.Region defaults (128 × uint64
LE). That is the layout the runtime actually uses: primitive.*Region.WordExtent()
reads through core.Cfg, the orchestrator stages prev/next into Asset[0,1], the
mesh writes its community id at Properties[0]+communityIDOffset (absolute w56),
and everything that reads the wire frame threads through those same offsets.

The kernel package (pkg/compute/kernel/layout.go) defines AssetStartWord = 72
for an ALU program-lowering scratch convention that does not reach the wire —
the viz deliberately does not mirror those constants.
*/

import {
	chainIdFromWord,
	formatWordHex64,
	readWordU64LE,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
	WORD,
} from "./valueLayout";

/** Canonical region keys aligned with kernel naming. */
export type ValueRegionName =
	| "tokens"
	| "program"
	| "signals"
	| "context"
	| "gradient"
	| "properties"
	| "asset"
	| "prev"
	| "next"
	| "id"
	| "affinity";

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
REGION_SPECS mirrors the runtime config (see pkg/core/config.go and
pkg/primitive/value.go):

  tokens   w0..w15   (1024 bits — Morton slab)
  program  w16..w23  (512 bits)
  signals  w24..w31  (512 bits)
  context  w32..w39  (512 bits)
  gradient w40..w47  (512 bits)
  properties w48..w55 (512 bits — the canonical band the orchestrator reads)
  asset    w56..w119 (4096 bits — chain staging + peer S/C/G/P + ALU scratch)
  prev     w120
  next     w121
  id       w122
  affinity w123..w127 (257 bits rounded up to 5 words)

The mesh layer writes the community id at absolute word 56 via
(propsStart + communityIDOffset=8). That word lives at the start of the Asset
region per the config layout, so the visualizer labels it explicitly in the
inspector. Do NOT change asset.startWord unless pkg/core/config.go changes.
*/
export const REGION_SPECS: ReadonlyArray<{
	name: ValueRegionName;
	startWord: number;
	wordCount: number;
}> = [
	{ name: "tokens", startWord: 0, wordCount: 16 },
	{ name: "program", startWord: 16, wordCount: 8 },
	{ name: "signals", startWord: 24, wordCount: 8 },
	{ name: "context", startWord: 32, wordCount: 8 },
	{ name: "gradient", startWord: 40, wordCount: 8 },
	{ name: "properties", startWord: 48, wordCount: 8 },
	{ name: "asset", startWord: 56, wordCount: 64 },
	{ name: "prev", startWord: 120, wordCount: 1 },
	{ name: "next", startWord: 121, wordCount: 1 },
	{ name: "id", startWord: 122, wordCount: 1 },
	{ name: "affinity", startWord: 123, wordCount: 5 },
] as const;

/** Program sub-indices relative to program region base (word 16). */
export const PROGRAM = {
	OPCODE: 0,
	ROT_TAB: 1,
	MODE: 2,
	SRC_A: 3,
	SRC_B: 4,
	DST: 5,
} as const;

export interface UnpackedRegionRef {
	start: number;
	span: number;
}

/*
UnpackRegionRef mirrors kernel.UnpackRegionRef — low 32 bits start, high 32 span.
*/
export function unpackRegionRef(word: bigint): UnpackedRegionRef {
	return {
		start: Number(word & 0xffff_ffffn),
		span: Number((word >> 32n) & 0xffff_ffffn),
	};
}

export interface DecodedProgramWire {
	/** Low 8 bits of program opcode word (boolean / routing lane). */
	opcodeLow: number;
	/** Full first program word (opcode byte + geometric gate bits). */
	opcodeWord: bigint;
	modeWord: bigint;
	srcA: UnpackedRegionRef;
	srcB: UnpackedRegionRef;
	dst: UnpackedRegionRef;
}

/*
DecodeProgramWire interprets the eight-word program region per kernel layout.
*/
export function decodeProgramWire(program: ValueRegionSlice): DecodedProgramWire {
	const w = program.words;
	const opcodeWord = w[PROGRAM.OPCODE] ?? 0n;
	const low = Number(opcodeWord & 0xffn);

	return {
		opcodeLow: low,
		opcodeWord,
		modeWord: w[PROGRAM.MODE] ?? 0n,
		srcA: unpackRegionRef(w[PROGRAM.SRC_A] ?? 0n),
		srcB: unpackRegionRef(w[PROGRAM.SRC_B] ?? 0n),
		dst: unpackRegionRef(w[PROGRAM.DST] ?? 0n),
	};
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
DecodeValueRegions maps a full 128-word image into named slices. Pass
Value.Bytes–length buffers via wordsFromFrame first.
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
WordsFromFrame builds the 128-word bigint lane from a raw LE wire frame.
*/
export function wordsFromFrame(frame: Uint8Array): bigint[] {
	if (frame.byteLength < VALUE_FRAME_BYTE_LENGTH) {
		return Array.from({ length: VALUE_WORD_COUNT }, () => 0n);
	}

	return Array.from({ length: VALUE_WORD_COUNT }, (_, wordIndex) =>
		readWordU64LE(frame, wordIndex),
	);
}

/*
DecodeValueRegionsFromFrame is the ergonomic single entry for raw bytes.
*/
export function decodeValueRegionsFromFrame(frame: Uint8Array): DecodedValueRegions {
	return decodeValueRegions(wordsFromFrame(frame));
}

/*
FormatWordHexAt resolves one absolute word index to display hex, preferring
pre-sliced regions when present so UI code does not duplicate readWordU64LE.
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
				return formatWordHex64(
					slice.words[wordIndex - slice.startWord],
				);
			}
		}
	}

	if (!frame || frame.byteLength < VALUE_FRAME_BYTE_LENGTH) {
		return null;
	}

	return formatWordHex64(readWordU64LE(frame, wordIndex));
}

/*
AffinityHexFromRegions matches valueLayout.affinityHexWords without re-reading bytes.
*/
export function affinityHexFromRegions(regions: DecodedValueRegions): string {
	return regions.affinity.words.map((word) => formatWordHex64(word)).join("");
}

export interface ChainPreview {
	prevCommitted: string;
	nextCommitted: string;
	prevStaged: string;
	nextStaged: string;
	idHex: string;
}

/*
ChainPreviewFromRegions mirrors ValueInspector chain logic using region slices.
*/
function chainPreviewFromRegions(regions: DecodedValueRegions): ChainPreview {
	return {
		prevCommitted: chainIdFromWord(regions.prev.words[0]),
		nextCommitted: chainIdFromWord(regions.next.words[0]),
		prevStaged: chainIdFromWord(regions.asset.words[0]),
		nextStaged: chainIdFromWord(regions.asset.words[1]),
		idHex: formatWordHex64(regions.id.words[0]),
	};
}

/*
ChainPreviewFromFrame is the frame-only path when wireRegions is unavailable.
*/
function chainPreviewFromFrame(frame: Uint8Array): ChainPreview {
	return {
		prevCommitted: chainIdFromWord(readWordU64LE(frame, WORD.PREV)),
		nextCommitted: chainIdFromWord(readWordU64LE(frame, WORD.NEXT)),
		prevStaged: chainIdFromWord(readWordU64LE(frame, WORD.ASSET_PREV)),
		nextStaged: chainIdFromWord(readWordU64LE(frame, WORD.ASSET_NEXT)),
		idHex: formatWordHex64(readWordU64LE(frame, WORD.ID)),
	};
}

/*
ChainPreview resolves chain + id display fields from regions or raw frame.
*/
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
