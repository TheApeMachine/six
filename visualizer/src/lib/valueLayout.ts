import {
	AFFINITY_START_WORD,
	ASSET_START_WORD,
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
} from "./layoutGenerated";

export {
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
} from "./layoutGenerated";

/*
Word indices match the runtime layout the orchestrator and mesh actually use:
pkg/core/config.go Value.Region defaults (which primitive.*Region.WordExtent()
reads through). pkg/compute/kernel/layout.go also uses AssetStartWord for
program-lowering scratch; the *staging* path (orchestrator → link firmware)
uses the same config-aligned Asset region. Mirroring the config is what lets
the visualizer see the same chain staging the rule engine sees.
*/

export const WORD = {
	/*
	The orchestrator stages predecessor/successor IDs into the first two Asset
	words of each Value before the link firmware copies them into Prev/Next.
	pkg/vm/orchestrator.go resolves that start from core.Cfg.Value.Region.Asset
	(default start word 72), so the viz has to read from the same words or the
	chain preview lies about what's staged.
	*/
	ASSET_PREV: ASSET_START_WORD,
	ASSET_NEXT: ASSET_START_WORD + 1,
	PREV: PREV_START_WORD,
	NEXT: NEXT_START_WORD,
	ID: ID_START_WORD,
	AFFINITY_0: AFFINITY_START_WORD,
} as const;

const WORD_BYTES = 8;

export function readWordU64LE(buf: Uint8Array, wordIndex: number): bigint {
	const offset = wordIndex * WORD_BYTES;

	if (offset + WORD_BYTES > buf.length) return 0n;

	const dv = new DataView(buf.buffer, buf.byteOffset + offset, WORD_BYTES);

	return dv.getBigUint64(0, true);
}

export function formatWordHex64(word: bigint): string {
	return word.toString(16).padStart(16, "0").toLowerCase();
}

/*
chainIdFromWord turns a non-zero link word into the same 16-nibble form as viz meta.
*/
export function chainIdFromWord(word: bigint): string {
	if (word === 0n) return "";

	return formatWordHex64(word);
}

/*
affinityHexWords concatenates five affinity lanes (same layout as viz.AffinityHexFromFrame).
*/
export function affinityHexWords(buf: Uint8Array): string {
	let out = "";

	for (let w = 0; w < 5; w++) {
		out += formatWordHex64(readWordU64LE(buf, WORD.AFFINITY_0 + w));
	}

	return out;
}
