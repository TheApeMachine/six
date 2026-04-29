/*
Morton decoder mirrors pkg/primitive/morton.go:DecodeInterleaved8x8 and
the Value packing in NewValue. The Go side packs each input byte as the
y-coordinate of an 8x8 Z-order code and the 1-indexed input position as
x. Code zero is the empty-slot sentinel, so a position offset of +1 is
applied at encode time and stripped here.

Each 64-bit token word holds four 16-bit codes packed low-to-high
(slot 0 = bits 0-15, slot 3 = bits 48-63). Codes within a Value are
written in input order with duplicates skipped, so this decoder collects
(position, byte) pairs across all token words, sorts by position, and
joins the bytes into a UTF-8 string. That mirrors what the tokenizer
saw for the segment, even when one token region is split across more
than one Value (each segment carries its own slice of input).
*/

const SLOTS_PER_WORD = 4;
const SLOT_MASK = 0xffffn;
const POSITION_OFFSET = 1;

export function decodeInterleaved8x8(code: number): { x: number; y: number } {
	let x = 0;
	let y = 0;

	for (let bit = 0; bit < 8; bit++) {
		x |= ((code >> (2 * bit)) & 1) << bit;
		y |= ((code >> (2 * bit + 1)) & 1) << bit;
	}

	return { x, y };
}

export function decodeTokenWords(words: readonly bigint[]): string {
	const pairs: Array<{ position: number; byte: number }> = [];

	for (const word of words) {
		for (let slot = 0; slot < SLOTS_PER_WORD; slot++) {
			const code = Number((word >> BigInt(slot * 16)) & SLOT_MASK);

			if (code === 0) {
				continue;
			}

			const { x, y } = decodeInterleaved8x8(code);
			const pos = x - POSITION_OFFSET;

			if (pos < 0) {
				console.error("morton: negative reconstructed position after decode", {
					code,
					x,
					POSITION_OFFSET,
				});
				continue;
			}

			pairs.push({ position: pos, byte: y });
		}
	}

	if (pairs.length === 0) {
		return "";
	}

	pairs.sort((a, b) => a.position - b.position);

	const bytes = new Uint8Array(pairs.length);
	for (let idx = 0; idx < pairs.length; idx++) {
		bytes[idx] = pairs[idx].byte;
	}

	return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
}
