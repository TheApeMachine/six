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
	(default start word 64), so the viz has to read from the same words or the
	chain preview lies about what's staged.
	*/
	ASSET_PREV: 64,
	ASSET_NEXT: 65,
	PREV: 120,
	NEXT: 121,
	ID: 122,
	AFFINITY_0: 123,
} as const;

/*
VALUE_WORD_COUNT matches primitive.Value ([128]uint64). The viz binary always
ships a full little-endian image when publishing WireFrameValue.
*/
export const VALUE_WORD_COUNT = 128;

const WORD_BYTES = 8;

export const VALUE_FRAME_BYTE_LENGTH = VALUE_WORD_COUNT * WORD_BYTES;

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
