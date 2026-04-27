import type {
	CausalState,
	FieldSnapshot,
	FieldValueSnapshot,
	ValueRole,
	VizGraphSnapshot,
	VizInspectSnapshot,
} from "@/features/telemetry/types";
import {
	ASSET_START_WORD,
	CONTEXT_START_WORD,
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
	VALUE_WORD_COUNT,
} from "./layoutGenerated";
import {
	type ClassifiedProgram,
	classifyInstructionStream,
	classifyKnownProgram,
	ROLE_BY_CATEGORY,
} from "./programClassifier";
import { PROPERTY_WORD, VALUE_ROLE } from "./propertiesGenerated";
import type { DecodedValueRegions } from "./valueRegions";
import {
	affinityHexFromRegions,
	chainIdFromWord,
	decodeProgramWire,
	decodeValueRegions,
	readWordU64LE,
} from "./valueRegions";

export interface DecodedValueFrame {
	id: string;
	prevId: string;
	nextId: string;
	content: string;
	words: bigint[];
	frame: Uint8Array;
	regions: DecodedValueRegions;
}

export interface StoredValue {
	id: string;
	role?: "data" | "action" | "reaction" | "prompt";
	frame: Uint8Array | null;
	decoded: DecodedValueFrame | null;
	receivedAtMs: number;
	communityId: number;
	affinityHex: string;
	classification: ClassifiedProgram;
	signalEnergy: number;
	causal: CausalState;
}

export interface TelemetryState {
	selection: VizInspectSnapshot | null;
	selectedId: string | null;
	snapshot: VizGraphSnapshot;
	stats: { values: number };
}

const PROPERTIES_LABELS_WORD = PROPERTY_WORD("LABELS");
const PROPERTIES_COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const PROPERTIES_NOISE_WORD = PROPERTY_WORD("NOISE");
const PROPERTIES_ROLE_WORD = PROPERTY_WORD("ROLE");
const PROPERTIES_TTL_WORD = PROPERTY_WORD("TTL");
const PROPERTIES_REFUTATION_TARGET_WORD = PROPERTY_WORD("TARGET");
const VALUE_ROLE_PROMPT_WORD = BigInt(VALUE_ROLE.Prompt);
const TTL_EXPIRED_SENTINEL_WORD = (1n << 64n) - 1n;
const ASSET_GRADIENT_WORD = ASSET_START_WORD + 16;
const ASSET_GRADIENT_SPAN = 8;
const CONTEXT_SPAN = 8;

interface CommunityData {
	affinityHex: string;
	members: Map<string, FieldValueSnapshot>;
	labeled: number;
	slotSum: number;
	labels: Map<number, number>;
	hypothesizing: number;
	falsified: number;
	intervening: number;
}

export function formatValueId(word: bigint): string {
	if (word === 0n) {
		return "";
	}

	return word.toString(16).padStart(16, "0").toLowerCase();
}

export function decodeValueFrame(frame: Uint8Array): DecodedValueFrame {
	const wire = new Uint8Array(frame);
	const words = Array.from({ length: VALUE_WORD_COUNT }, (_, wordIndex) =>
		readWordU64LE(wire, wordIndex),
	);
	const prevCommitted = chainIdFromWord(words[PREV_START_WORD]);
	const nextCommitted = chainIdFromWord(words[NEXT_START_WORD]);
	const prevStaged = chainIdFromWord(words[ASSET_START_WORD]);
	const nextStaged = chainIdFromWord(words[ASSET_START_WORD + 1]);
	const regions = decodeValueRegions(words);

	return {
		id: formatValueId(words[ID_START_WORD]),
		prevId: prevCommitted || prevStaged,
		nextId: nextCommitted || nextStaged,
		content: decodeValueContent(wire),
		words,
		frame: wire,
		regions,
	};
}

export function valueFrameExpired(decoded: DecodedValueFrame): boolean {
	return (decoded.words[PROPERTIES_TTL_WORD] ?? 0n) === TTL_EXPIRED_SENTINEL_WORD;
}

export function storedValueFromDecoded(
	id: string,
	decoded: DecodedValueFrame,
	previous?: StoredValue,
): StoredValue {
	const roleWord = decoded.words[PROPERTIES_ROLE_WORD] ?? 0n;
	const role =
		roleWord === VALUE_ROLE_PROMPT_WORD
			? "prompt"
			: previous?.role === "prompt"
				? undefined
				: previous?.role;
	const communityWord = decoded.words[PROPERTIES_COMMUNITY_WORD] ?? 0n;

	return {
		id,
		role,
		frame: decoded.frame,
		decoded,
		receivedAtMs: Date.now(),
		communityId: communityWord !== 0n ? Number(communityWord) : -1,
		affinityHex: affinityHexFromRegions(decoded.regions),
		classification: classifyFrameProgram(decoded),
		signalEnergy: signalPopcount(decoded),
		causal: readCausalState(decoded.words),
	};
}

export function buildGraphSnapshot(
	values: ReadonlyMap<string, StoredValue>,
	graphSeq: number,
): VizGraphSnapshot {
	const totalValues = values.size;
	const communityIndex = new Map<number, CommunityData>();
	const fields: FieldSnapshot[] = [];
	const orphanValues: FieldValueSnapshot[] = [];

	for (const stored of values.values()) {
		if (stored.communityId < 0) {
			orphanValues.push(fieldSnapshotFromStored(stored, ""));
			continue;
		}

		let data = communityIndex.get(stored.communityId);

		if (!data) {
			data = {
				affinityHex: stored.affinityHex,
				members: new Map(),
				labeled: 0,
				slotSum: 0,
				labels: new Map(),
				hypothesizing: 0,
				falsified: 0,
				intervening: 0,
			};
			communityIndex.set(stored.communityId, data);
		}

		const snap = fieldSnapshotFromStored(stored, data.affinityHex);
		data.members.set(stored.id, snap);
		applyLabelMetrics(data, stored);
		applyCausalDelta(data, snap.causal, 1);
	}

	for (const [id, data] of communityIndex) {
		const members = Array.from(data.members.values()).sort((left, right) =>
			left.id.localeCompare(right.id),
		);
		const metrics = communityMetrics(data, members.length);

		fields.push({
			id,
			memberCount: members.length,
			saturated: false,
			saturation: metrics.crystallization,
			lastAction: "",
			actionCount: 0,
			reactionCount: 0,
			affinityHex: data.affinityHex,
			concentration: totalValues > 0 ? members.length / totalValues : 0,
			members,
			coverage: metrics.coverage,
			consensus: metrics.consensus,
			labelDensity: metrics.labelDensity,
			crystallization: metrics.crystallization,
			dominantRatio: metrics.dominantRatio,
			modeCount: metrics.modeCount,
			pressureMult: 0,
			hypothesizingCount: data.hypothesizing,
			falsifiedCount: data.falsified,
			interveningCount: data.intervening,
		});
	}

	fields.sort((left, right) => left.id - right.id);
	orphanValues.sort((left, right) => left.id.localeCompare(right.id));

	return {
		timestamp: Date.now(),
		graphSeq,
		fields,
		orphanValues,
		totalValues,
		totalCommunities: fields.length,
	};
}

export function inspectFromStored(stored: StoredValue): VizInspectSnapshot {
	const role = stored.role ?? "data";
	const base = fieldSnapshotFromStored(stored, stored.affinityHex);

	return {
		...base,
		pos: layoutPosition(stored.id, role),
	};
}

function readCausalState(words: bigint[]): CausalState {
	const hypothesizing = words[PROPERTIES_REFUTATION_TARGET_WORD] !== 0n;
	const falsified = words[PROPERTIES_NOISE_WORD] !== 0n;
	const surprisal = Number(words[PROPERTY_WORD("SURPRISAL")] ?? 0n);
	const delta_surprisal = Number(words[PROPERTY_WORD("DELTA_SURPRISAL")] ?? 0n);
	const ttl = Number(words[PROPERTIES_TTL_WORD] ?? 0n);
	const temperature = Number(words[PROPERTY_WORD("TEMPERATURE")] ?? 0n);
	let gradientAcc = 0n;
	let contextAcc = 0n;

	for (let offset = 0; offset < ASSET_GRADIENT_SPAN; offset++) {
		gradientAcc |= words[ASSET_GRADIENT_WORD + offset] ?? 0n;
	}

	for (let offset = 0; offset < CONTEXT_SPAN; offset++) {
		contextAcc |= words[CONTEXT_START_WORD + offset] ?? 0n;
	}

	return {
		hypothesizing,
		falsified,
		intervening:
			gradientAcc !== 0n &&
			contextAcc !== 0n &&
			(words[PREV_START_WORD] ?? 0n) === 0n,
		surprisal,
		delta_surprisal,
		ttl,
		temperature,
	};
}

function classifyFrameProgram(decoded: DecodedValueFrame): ClassifiedProgram {
	const installed = classifyInstructionStream(
		decodeProgramWire(decoded.regions.program),
	);

	if (installed.program) {
		return installed;
	}

	if (isCompletedCommunityRecruiter(decoded)) {
		return classifyKnownProgram("recruit_community");
	}

	return installed;
}

function isCompletedCommunityRecruiter(decoded: DecodedValueFrame): boolean {
	const id = decoded.words[ID_START_WORD] ?? 0n;
	const community = decoded.words[PROPERTIES_COMMUNITY_WORD] ?? 0n;

	return id !== 0n && community === id;
}

function signalPopcount(decoded: DecodedValueFrame): number {
	let energy = 0;

	for (const word of decoded.regions.signals.words) {
		let bits = word;

		while (bits !== 0n) {
			bits &= bits - 1n;
			energy++;
		}
	}

	return energy;
}

function fieldSnapshotFromStored(
	stored: StoredValue,
	communityAffinityHex: string,
): FieldValueSnapshot {
	const decoded = stored.decoded;
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
		resonance: Math.min(1, stored.signalEnergy / 512),
		gap: 1,
		resolved: false,
		actionResonance: 0,
		prevId: decoded?.prevId ?? "",
		nextId: decoded?.nextId ?? "",
		communityAffinityHex,
		wireFrame: decoded?.frame ?? null,
		wireRegions: null,
		frameReceivedAtMs: stored.receivedAtMs,
		telemetry: null,
		causal: { ...stored.causal },
	};
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

function decodeInterleaved8x8(code: number): { x: number; y: number } {
	let x = 0;
	let y = 0;

	for (let bit = 0; bit < 8; bit++) {
		x |= ((code >> (2 * bit)) & 1) << bit;
		y |= ((code >> (2 * bit + 1)) & 1) << bit;
	}

	return { x, y };
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

			out.push(decodeInterleaved8x8(code).x);
		}
	}

	return new TextDecoder().decode(new Uint8Array(out));
}

function applyLabelMetrics(data: CommunityData, stored: StoredValue) {
	const labelsWord = stored.decoded?.words[PROPERTIES_LABELS_WORD] ?? 0n;
	let slots = 0;

	for (let slot = 0; slot < 4; slot++) {
		const label = Number((labelsWord >> BigInt(slot * 16)) & 0xffffn);

		if (label === 0) {
			continue;
		}

		slots++;
		data.labels.set(label, (data.labels.get(label) ?? 0) + 1);
	}

	if (slots === 0) {
		return;
	}

	data.labeled++;
	data.slotSum += slots;
}

function communityMetrics(data: CommunityData, memberCount: number) {
	if (memberCount === 0) {
		return {
			coverage: 0,
			consensus: 0,
			labelDensity: 0,
			crystallization: 0,
			dominantRatio: 0,
			modeCount: 0,
		};
	}

	const coverage = data.labeled / memberCount;
	const labelDensity = data.slotSum / (memberCount * 4);
	const consensus = shannonConsensus(data.labels, data.slotSum);
	const crystallization = coverage * consensus * labelDensity;
	const dominantRatio =
		data.slotSum > 0
			? Math.max(...data.labels.values()) / data.slotSum
			: 0;

	return {
		coverage,
		consensus,
		labelDensity,
		crystallization,
		dominantRatio,
		modeCount: data.labels.size,
	};
}

function shannonConsensus(labels: ReadonlyMap<number, number>, total: number) {
	if (labels.size <= 1) {
		return total > 0 ? 1 : 0;
	}

	let entropy = 0;

	for (const count of labels.values()) {
		const probability = count / total;

		if (probability > 0) {
			entropy -= probability * Math.log2(probability);
		}
	}

	const maxEntropy = Math.log2(labels.size);

	if (maxEntropy === 0) {
		return 1;
	}

	return 1 - entropy / maxEntropy;
}

function applyCausalDelta(
	data: CommunityData,
	causal: CausalState,
	sign: number,
) {
	if (causal.hypothesizing) {
		data.hypothesizing += sign;
	}

	if (causal.falsified) {
		data.falsified += sign;
	}

	if (causal.intervening) {
		data.intervening += sign;
	}
}
