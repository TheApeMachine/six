import type {
	FieldSnapshot,
	FieldValueSnapshot,
	ValueRole,
	VizGraphSnapshot,
	VizInspectSnapshot,
} from "@/features/telemetry/types";
import {
	type ClassifiedProgram,
	classifyProgramWire,
	ROLE_BY_CATEGORY,
} from "./programClassifier";
import {
	chainIdFromWord,
	readWordU64LE,
	VALUE_FRAME_BYTE_LENGTH,
	VALUE_WORD_COUNT,
	WORD,
} from "./valueLayout";
import type { DecodedValueRegions } from "./valueRegions";
import {
	affinityHexFromRegions,
	decodeProgramWire,
	decodeValueRegions,
	REGION_SPECS,
} from "./valueRegions";
import type { RawValueFrame } from "./wire";

/*
PROPERTIES_COMMUNITY_WORD is the absolute word index where the mesh
routing layer stamps the community id via an ephemeral CopyMaskMerge
program. Must match kernel.PropertiesCommunityWord (Go side) = 56.
*/
const PROPERTIES_COMMUNITY_WORD = 56;

export interface DecodedValueFrame {
	id: string;
	prevId: string;
	nextId: string;
	content: string;
	words: bigint[];
	frame: Uint8Array;
	/** Named slices for tokens, program, asset, … — same layout as pkg/compute/kernel. */
	regions: DecodedValueRegions;
}

export interface StoredValue {
	id: string;
	role?: "data" | "action" | "reaction" | "prompt";
	frame: Uint8Array | null;
	decoded: DecodedValueFrame | null;
	/** Last time a wire frame was applied (ms since epoch). */
	receivedAtMs: number;
	/** -1 until the wire frame carries a non-zero community word. */
	communityId: number;
	/** Hex of the Value's own 5-word affinity tag, for inspector display. */
	affinityHex: string;
	/** Program + category resolved from the wire frame's program region. */
	classification: ClassifiedProgram;
	/** Signals popcount for the most recent frame, used as a readout-energy
		cue in the canvas. Zero for Values whose signals are still blank. */
	signalEnergy: number;
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

/** React-facing state: wire table + selection + graph snapshot for the canvas. */
export interface TelemetryState {
	selection: VizInspectSnapshot | null;
	selectedId: string | null;
	snapshot: VizGraphSnapshot;
	stats: { values: number };
}

function layoutPosition(id: string, role: ValueRole): { x: number; y: number } {
	let seed = 0;

	for (let index = 0; index < id.length; index++) {
		seed = (seed * 33 + id.charCodeAt(index)) >>> 0;
	}

	const ring = role === "prompt" ? 120 : 220 + (seed % 7) * 40;
	const angle = (seed % 360) * (Math.PI / 180);

	return {
		x: Math.cos(angle) * ring,
		y: Math.sin(angle) * ring,
	};
}

function fieldSnapshotFromStored(
	stored: StoredValue,
	communityAffinityHex: string,
): FieldValueSnapshot {
	const decoded = stored.decoded;
	/*
	Role is derived from the installed program's category rather than a
	caller-supplied hint: a Value running beam_swarm_step IS an action,
	a classify_readout IS a reaction, a bare unsupervised frame IS data.
	An explicit stored.role override still wins so the bridge can mark
	prompts at ingestion before the ALU has run.
	*/
	const role: ValueRole =
		stored.role ?? ROLE_BY_CATEGORY[stored.classification.category];

	return {
		id: stored.id,
		role,
		program: stored.classification.program,
		firmwareSteps: [],
		communityId: stored.communityId,
		label: "",
		content: decoded?.content ?? "",
		/*
		Signals popcount divided by the full 512-bit width gives a 0..1
		readout-energy that FieldLiveCanvas consumes as `resonance` — a
		fully lit signals region pulses at full brightness, blank
		signals render cold.
		*/
		resonance: Math.min(1, stored.signalEnergy / 512),
		gap: 1,
		resolved: false,
		actionResonance: 0,
		prevId: decoded?.prevId ?? "",
		nextId: decoded?.nextId ?? "",
		communityAffinityHex,
		wireFrame: decoded?.frame ?? null,
		wireRegions: null,
		telemetry: null,
	};
}

function inspectFromStored(
	stored: StoredValue,
	communityAffinityHex: string,
): VizInspectSnapshot {
	const role = stored.role ?? "data";
	const base = fieldSnapshotFromStored(stored, communityAffinityHex);

	return {
		...base,
		pos: layoutPosition(stored.id, role),
	};
}

/*
buildGraphSnapshot groups Values by their on-wire community id (stamped
by the mesh routing layer into properties[8]) and emits FieldSnapshot
entries for the canvas. Values whose community word is still zero land
in orphanValues — they haven't been processed by a community field yet.
*/
function buildGraphSnapshot(store: ValueStore): VizGraphSnapshot {
	const communityMap = new Map<
		number,
		{ members: FieldValueSnapshot[]; affinityHex: string }
	>();
	const orphanValues: FieldValueSnapshot[] = [];

	for (const stored of store.list()) {
		if (stored.communityId < 0) {
			orphanValues.push(fieldSnapshotFromStored(stored, ""));
			continue;
		}

		let bucket = communityMap.get(stored.communityId);

		if (!bucket) {
			bucket = { members: [], affinityHex: stored.affinityHex };
			communityMap.set(stored.communityId, bucket);
		}

		bucket.members.push(
			fieldSnapshotFromStored(stored, bucket.affinityHex),
		);
	}

	const fields: FieldSnapshot[] = [];

	for (const [id, bucket] of communityMap) {
		fields.push({
			id,
			memberCount: bucket.members.length,
			saturated: false,
			saturation: 0,
			lastAction: "",
			actionCount: 0,
			reactionCount: 0,
			affinityHex: bucket.affinityHex,
			concentration:
				store.size > 0 ? bucket.members.length / store.size : 0,
			members: bucket.members,
		});
	}

	fields.sort((left, right) => left.id - right.id);
	orphanValues.sort((left, right) => left.id.localeCompare(right.id));

	return {
		timestamp: Date.now(),
		fields,
		orphanValues,
		totalValues: store.size,
		totalCommunities: fields.length,
	};
}

export function decodeValueFrame(frame: Uint8Array): DecodedValueFrame {
	const wire = cloneFrame(frame);
	const words = Array.from({ length: VALUE_WORD_COUNT }, (_, wordIndex) =>
		readWordU64LE(wire, wordIndex),
	);

	const prevCommitted = chainIdFromWord(words[WORD.PREV]);
	const nextCommitted = chainIdFromWord(words[WORD.NEXT]);
	const prevStaged = chainIdFromWord(words[WORD.ASSET_PREV]);
	const nextStaged = chainIdFromWord(words[WORD.ASSET_NEXT]);

	const regions = decodeValueRegions(words);

	return {
		id: formatValueId(words[WORD.ID]),
		prevId: prevCommitted || prevStaged,
		nextId: nextCommitted || nextStaged,
		content: decodeValueContent(wire),
		words,
		frame: wire,
		regions,
	};
}

export class ValueStore {
	private readonly values = new Map<string, StoredValue>();
	private readonly pendingFrames = new Map<string, Uint8Array>();
	private selectedId: string | null = null;

	get(id: string): StoredValue | undefined {
		return this.values.get(id);
	}

	has(id: string): boolean {
		return this.values.has(id);
	}

	get size(): number {
		return this.values.size;
	}

	list(): StoredValue[] {
		return [...this.values.values()];
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
				receivedAtMs: 0,
				communityId: -1,
				affinityHex: "",
				classification: classifyProgramWire(null),
				signalEnergy: 0,
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
		value.receivedAtMs = Date.now();
		value.affinityHex = affinityHexFromRegions(decoded.regions);

		// Read the community id directly from the wire frame. The mesh
		// routing layer stamps this via an ephemeral CopyMaskMerge program
		// so the value arrives on-wire already tagged.
		const communityWord = decoded.words[PROPERTIES_COMMUNITY_WORD] ?? 0n;
		value.communityId = communityWord !== 0n ? Number(communityWord) : -1;

		/*
		Program identity is recovered from the program region's compiled
		frame descriptor — see programClassifier for the signature table.
		The classifier never guesses past its signature set, so Values
		carrying an unrecognised descriptor land in the "unknown" bucket
		instead of being tagged with whichever program happens to be
		first in the list.
		*/
		value.classification = classifyProgramWire(
			decodeProgramWire(decoded.regions.program),
		);

		/*
		Signals popcount is a cheap proxy for "how loud is this Value
		right now". A classify_readout just finished => one bit set in
		signals[0]; a full-span XOR accumulate => up to 512. The canvas
		scales glow intensity off this, so fresh readouts pop visually.
		*/
		let energy = 0;

		for (const word of decoded.regions.signals.words) {
			let bits = word;
			while (bits !== 0n) {
				bits &= bits - 1n;
				energy++;
			}
		}

		value.signalEnergy = energy;
	}

	private rebuildUiState(): TelemetryState {
		const snapshot = buildGraphSnapshot(this);

		let selected: VizInspectSnapshot | null = null;

		if (this.selectedId && this.has(this.selectedId)) {
			const stored = this.get(this.selectedId) as StoredValue;
			selected = inspectFromStored(stored, stored.affinityHex);
		}

		return {
			selection: selected,
			selectedId: selected ? this.selectedId : null,
			snapshot,
			stats: { values: this.size },
		};
	}

	getState(): TelemetryState {
		return this.rebuildUiState();
	}

	applyWireFrames(frames: RawValueFrame[]): TelemetryState {
		for (const frame of frames) {
			const id = formatValueId(frame.valueId);

			if (!id) {
				continue;
			}

			this.ensure(id);
			this.applyWireFrame(frame.valueId, frame.bytes);
		}

		return this.rebuildUiState();
	}

	selectValueById(id: string): TelemetryState {
		if (!id || !this.has(id)) {
			this.selectedId = null;
			return this.rebuildUiState();
		}

		this.selectedId = id;
		return this.rebuildUiState();
	}
}
