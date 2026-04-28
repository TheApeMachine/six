import {
	ASSET_START_WORD,
	ID_START_WORD,
	NEXT_START_WORD,
	PREV_START_WORD,
	VALUE_WORD_COUNT,
} from "./layoutGenerated";
import { decodeTokenWords } from "./morton";
import {
	type ClassifiedProgram,
	classifyInstructionStream,
} from "./programClassifier";
import { PROPERTY_WORD, VALUE_ROLE } from "./propertiesGenerated";
import {
	affinityHexFromRegions,
	chainIdFromWord,
	type DecodedValueRegions,
	decodeProgramWire,
	decodeValueRegions,
	readWordU64LE,
} from "./valueRegions";

export interface DecodedValueFrame {
	id: string;
	prevId: string;
	nextId: string;
	words: bigint[];
	frame: Uint8Array;
	regions: DecodedValueRegions;
}

/*
StoredValue is the dashboard's per-Value record. The TanStack store
upserts these by id; the dashboard projects directly off the Map so
there is no intermediate snapshot type.
*/
export interface StoredValue {
	id: string;
	role?: "data" | "action" | "reaction" | "prompt";
	frame: Uint8Array | null;
	decoded: DecodedValueFrame | null;
	receivedAtMs: number;
	communityId: number;
	affinityHex: string;
	classification: ClassifiedProgram;
	tokenText: string;
}

const PROPERTIES_COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const PROPERTIES_ROLE_WORD = PROPERTY_WORD("ROLE");
const PROPERTIES_TTL_WORD = PROPERTY_WORD("TTL");
const VALUE_ROLE_PROMPT_WORD = BigInt(VALUE_ROLE.Prompt);
const TTL_EXPIRED_SENTINEL_WORD = (1n << 64n) - 1n;

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
		tokenText: decodeTokenWords(decoded.regions.tokens.words),
	};
}

/*
classifyFrameProgram resolves a Value's program region to a named
firmware by matching the decoded instruction stream against the
generated PROGRAM_SIGNATURES table. Unknown payloads still get a
category so the grid can render them.
*/
function classifyFrameProgram(decoded: DecodedValueFrame): ClassifiedProgram {
	return classifyInstructionStream(decodeProgramWire(decoded.regions.program));
}
