/*
Region layout mirrors the authoritative runtime config in cmd/cfg/config.yml
(loaded via pkg/core/config.go → viper) and the kernel word addresses in
pkg/compute/kernel/layout.go:

  tokens   w0..w15    (1024 bits)
  program  w16..w23   (512 bits)
  signals  w24..w31   (512 bits)
  context  w32..w39   (512 bits)
  gradient w40..w47   (512 bits)
  properties w48..w63 (1024 bits — community id at w56, firmware status at w57,
                       TTL at w51, etc.)
  asset    w64..w119 (3584 bits — chain staging + peer S/C/G/P + scratch)
  prev     w120
  next     w121
  id       w122
  affinity w123..w127 (257 bits rounded up to 5 words)

pkg/core/config.go NewConfig() defaults match cmd/cfg/config.yml (properties 1024b,
asset 3584b). Keep REGION_SPECS aligned with that yaml — that is what flows on the
wire the visualizer consumes.
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
REGION_SPECS mirrors the runtime layout declared in cmd/cfg/config.yml and
consumed by pkg/primitive via core.Cfg:

  tokens   w0..w15    (1024 bits — Morton slab)
  program  w16..w23   (512 bits)
  signals  w24..w31   (512 bits)
  context  w32..w39   (512 bits)
  gradient w40..w47   (512 bits)
  properties w48..w63 (1024 bits — canonical band; community id at
                       w56 = properties[8], firmware status at w57 = properties[9])
  asset    w64..w119  (3584 bits — peer S+C+G+P staging + scratch + scheduler)
  prev     w120
  next     w121
  id       w122
  affinity w123..w127 (257 bits rounded up to 5 words)

mesh.Field stamps the community id at absolute word 56, which sits inside
PROPERTIES (not ASSET) — the inspector surfaces it alongside the rest of the
properties labels. Do NOT change these offsets without updating config.yml
in lockstep or StageAssetFrom will mis-stage peer state.
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
	{ name: "properties", startWord: 48, wordCount: 16 },
	{ name: "asset", startWord: 64, wordCount: 56 },
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
