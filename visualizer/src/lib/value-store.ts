import {
	chainIdFromWord,
	readWordU64LE,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
	WORD,
} from "./valueLayout";

export interface DecodedValueFrame {
	id: string;
	prevId: string;
	nextId: string;
	content: string;
	words: bigint[];
	frame: Uint8Array;
}

export interface StoredValue {
	id: string;
	role?: "data" | "action" | "reaction" | "prompt";
	frame: Uint8Array | null;
	decoded: DecodedValueFrame | null;
}

function formatValueId(word: bigint): string {
	if (word === 0n) {
		return "";
	}

	return word.toString(16).padStart(16, "0").toLowerCase();
}

function decodeInterleaved8x8(code: number): { x: number; y: number } {
	let x = 0;
	let y = 0;

	for (let bit = 0; bit < 8; bit++) {
		x |= ((code >> (2 * bit)) & 1) << bit;
		y |= ((code >> (2 * bit + 1)) & 1) << bit;
	}

	return { x, y };
}

function cloneFrame(frame: Uint8Array): Uint8Array {
	return new Uint8Array(frame);
}

function decodeValueContent(frame: Uint8Array): string {
	const out: number[] = [];

	for (let wordIndex = 0; wordIndex < 16; wordIndex++) {
		const word = readWordU64LE(frame, wordIndex);

		for (let slot = 0; slot < 4; slot++) {
			const code = Number((word >> BigInt(slot * 16)) & 0xffffn);

			if (code === 0) {
				continue;
			}

			const { x } = decodeInterleaved8x8(code);
			out.push(x);
		}
	}

	return new TextDecoder().decode(new Uint8Array(out));
}

export function decodeValueFrame(frame: Uint8Array): DecodedValueFrame {
	const wire = cloneFrame(frame);
	const words = Array.from(
		{ length: VALUE_WORD_COUNT },
		(_, wordIndex) => readWordU64LE(wire, wordIndex),
	);

	const prevCommitted = chainIdFromWord(words[WORD.PREV]);
	const nextCommitted = chainIdFromWord(words[WORD.NEXT]);
	const prevStaged = chainIdFromWord(words[WORD.ASSET_PREV]);
	const nextStaged = chainIdFromWord(words[WORD.ASSET_NEXT]);

	return {
		id: formatValueId(words[WORD.ID]),
		prevId: prevCommitted || prevStaged,
		nextId: nextCommitted || nextStaged,
		content: decodeValueContent(wire),
		words,
		frame: wire,
	};
}

export class ValueStore {
	private readonly values = new Map<string, StoredValue>();
	private readonly pendingFrames = new Map<string, Uint8Array>();

	get(id: string): StoredValue | undefined {
		return this.values.get(id);
	}

	has(id: string): boolean {
		return this.values.has(id);
	}

	ensure(
		id: string,
		init?: {
			role?: StoredValue["role"];
		},
	): StoredValue {
		const normalized = id.toLowerCase();
		let value = this.values.get(normalized);

		if (!value) {
			value = {
				id: normalized,
				role: init?.role,
				frame: null,
				decoded: null,
			};
			this.values.set(normalized, value);
		}

		if (init?.role !== undefined) {
			value.role = init.role;
		}

		const pending = this.pendingFrames.get(normalized);

		if (pending) {
			this.attachFrame(value, pending);
			this.pendingFrames.delete(normalized);
		}

		return value;
	}

	applyWireFrame(valueID: bigint, frame: Uint8Array): StoredValue | undefined {
		const id = formatValueId(valueID);

		if (!id) {
			return undefined;
		}

		const value = this.values.get(id);

		if (!value) {
			this.pendingFrames.set(id, cloneFrame(frame));
			return undefined;
		}

		this.attachFrame(value, frame);
		return value;
	}

	private attachFrame(value: StoredValue, frame: Uint8Array) {
		const decoded = decodeValueFrame(frame);

		value.id = decoded.id || value.id;
		value.frame = decoded.frame;
		value.decoded = decoded;
	}
}

export function isFullValueFrame(frame: Uint8Array): boolean {
	return frame.byteLength >= VALUE_FRAME_BYTE_LENGTH;
}
