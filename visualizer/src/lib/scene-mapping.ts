import * as THREE from "three";
import { PROPERTY_WORD, VALUE_ROLE } from "./propertiesGenerated";
import type { StoredValue } from "./value-frame";

/*
scene-mapping turns the dashboard's StoredValue objects into the per-
instance state the 3D scene needs. The visual contract is intentionally
dense: the eye should be able to read role, status, community, recent
activity, and chain membership without expanding the inspector.

  geometry kind  ← role / recruiter status (the "what is this")
  color          ← chosen color mode (status / community / role / firmware)
  brightness     ← ticksSinceTouch (recent commits glow, then fade)
  scale          ← selection / recruiter / BUSY (transitional state pops)
  outline ring   ← SELECTED status, plus a global halo on the user-pick
  prev/next      ← always-on subtle line graph between linked Values
  preset edges   ← louder relationships matching the current scene preset

Position derives from the affinity hash so two Values whose content the
SimHash projects nearby end up nearby in 3D space — the spatial layout
is the closest the visualiser gets to "see the field" in one glance.
ID is mixed in to break perfect collisions when two Values share an
affinity prefix (common right after mint, before community recruitment
disperses them).
*/

const FIELD_RADIUS = 65;
const COMMUNITY_LAYOUT_RADIUS = 55;
const COMMUNITY_LOCAL_BASE_RADIUS = 6;
const QUERY_RECRUITER_RADIUS = 50;
const QUERY_LOCAL_RADIUS = 6;
const PROMPT_ROW_Y = -40;
const PROMPT_RECRUITER_Y = 35;
const PROMPT_SPACING = 22;
const PROMPT_LOCAL_RADIUS = 6;
const STATUS_WORD = PROPERTY_WORD("STATUS");
const COMMUNITY_WORD = PROPERTY_WORD("COMMUNITY");
const REFERENCE_WORD = PROPERTY_WORD("REFERENCE");
const TARGET_WORD = PROPERTY_WORD("TARGET");
const ROLE_WORD = PROPERTY_WORD("ROLE");
const CONTINUATION_WORD = PROPERTY_WORD("CONTINUATION");

const STATUS_BUSY = 2;
const STATUS_SELECTED = 4;

export type ColorMode = "status" | "community" | "role" | "firmware";
export type ScenePreset = "all" | "queries" | "recruitment" | "prompts";

export type GeometryKind =
	| "sphere"
	| "torus"
	| "octahedron"
	| "cube"
	| "cone";

const STATUS_COLORS: Record<number, THREE.Color> = {
	0: new THREE.Color(0x475569), // PENDING — slate
	1: new THREE.Color(0x10b981), // READY — emerald
	2: new THREE.Color(0xf59e0b), // BUSY — amber
	3: new THREE.Color(0x0ea5e9), // WAITING — sky
	4: new THREE.Color(0x06b6d4), // SELECTED — cyan
	5: new THREE.Color(0x64748b), // DONE — slate
	6: new THREE.Color(0x8b5cf6), // RESOLVED — violet
	7: new THREE.Color(0xef4444), // ERROR — red
};

const ROLE_COLORS: Record<number, THREE.Color> = {
	0: new THREE.Color(0x334155),
	1: new THREE.Color(0xf97316), // Programmer — orange
	2: new THREE.Color(0x14b8a6), // Learner — teal
	3: new THREE.Color(0xfacc15), // Readout — yellow
	4: new THREE.Color(0xa855f7), // Association — purple
	[VALUE_ROLE.Prompt]: new THREE.Color(0xff2bd6), // Prompt — magenta
};

const ORPHAN_COLOR = new THREE.Color(0x1f2937);

export interface InstanceState {
	id: string;
	kind: GeometryKind;
	position: THREE.Vector3;
	color: THREE.Color;
	scale: number;
	dimmed: boolean;
	statusCode: number;
	hasContinuation: boolean;
}

export interface EdgeState {
	from: THREE.Vector3;
	to: THREE.Vector3;
	color: THREE.Color;
}

export interface SceneSnapshot {
	instances: InstanceState[];
	chainEdges: EdgeState[];
	accentEdges: EdgeState[];
	selectedPosition: THREE.Vector3 | null;
}

/*
positionForValue maps a Value to a stable 3D point in affinity space.
Affinity words 0/1/2 each give a 64-bit pseudo-random projection of the
content; we hash the ID into the mix so identical-affinity siblings
(common during mint) still spread instead of overlapping. Output sits
inside a sphere of radius FIELD_RADIUS so the camera framing stays
predictable. This is the "all" layout and the fallback used by every
other layout for Values that have no preset-specific home.
*/
export function positionForValue(stored: StoredValue): THREE.Vector3 {
	if (!stored.decoded) {
		return new THREE.Vector3(0, 0, 0);
	}

	const aff = stored.decoded.regions.affinity.words;
	const idWord = stored.decoded.regions.id.words[0] ?? 0n;

	const xn = wordToUnit(aff[0] ^ rotateLeft64(idWord, 7));
	const yn = wordToUnit(aff[1] ^ rotateLeft64(idWord, 23));
	const zn = wordToUnit(aff[2] ^ rotateLeft64(idWord, 41));

	return new THREE.Vector3(
		(xn - 0.5) * 2 * FIELD_RADIUS,
		(yn - 0.5) * 2 * FIELD_RADIUS,
		(zn - 0.5) * 2 * FIELD_RADIUS,
	);
}

/*
fibonacciSphere spreads N points on a sphere using the golden-angle
spiral. The result is visually even at any N — much better than naive
random sampling for showing relationships, because it avoids the eye-
catching clumps and gaps that hash-based positions produce.
*/
function fibonacciSphere(
	index: number,
	total: number,
	radius: number,
): THREE.Vector3 {
	const safeTotal = Math.max(1, total);
	const phi = Math.PI * (Math.sqrt(5) - 1);
	const y = 1 - (index / safeTotal) * 2;
	const planeRadius = Math.sqrt(Math.max(0, 1 - y * y));
	const theta = phi * index;

	return new THREE.Vector3(
		Math.cos(theta) * planeRadius * radius,
		y * radius,
		Math.sin(theta) * planeRadius * radius,
	);
}

function communityIdHex(stored: StoredValue): string {
	if (!stored.decoded) {
		return "";
	}

	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
	if (community === 0n) {
		return "";
	}

	return community.toString(16).padStart(16, "0");
}

/*
layoutByCommunity arranges members tightly around their recruiter and
spreads recruiters on the field shell, so distinct communities read as
separate clusters at a glance. Cluster-local placement uses fibonacci
spread so even small communities are visually legible. Orphans (no
community stamp) sit on the outer shell at their affinity coordinate so
they remain spatially meaningful but stay out of the cluster
foreground.
*/
function layoutByCommunity(
	values: ReadonlyMap<string, StoredValue>,
): Map<string, THREE.Vector3> {
	const positions = new Map<string, THREE.Vector3>();
	const buckets = new Map<string, StoredValue[]>();
	const orphans: StoredValue[] = [];

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		const communityHex = communityIdHex(stored);
		if (!communityHex) {
			orphans.push(stored);
			continue;
		}

		let bucket = buckets.get(communityHex);
		if (!bucket) {
			bucket = [];
			buckets.set(communityHex, bucket);
		}

		bucket.push(stored);
	}

	const sortedCommunityIds = [...buckets.keys()].sort();
	const communityCenters = new Map<string, THREE.Vector3>();

	for (let i = 0; i < sortedCommunityIds.length; i++) {
		communityCenters.set(
			sortedCommunityIds[i],
			fibonacciSphere(i, sortedCommunityIds.length, COMMUNITY_LAYOUT_RADIUS),
		);
	}

	for (const [communityHex, bucket] of buckets) {
		const center = communityCenters.get(communityHex);
		if (!center) {
			continue;
		}

		bucket.sort((a, b) => a.id.localeCompare(b.id));
		const localRadius =
			COMMUNITY_LOCAL_BASE_RADIUS + Math.log2(bucket.length + 2) * 1.6;

		let memberIndex = 0;
		for (const stored of bucket) {
			if (stored.id === communityHex) {
				positions.set(stored.id, center.clone());
				continue;
			}

			const offset = fibonacciSphere(memberIndex, bucket.length, localRadius);
			positions.set(stored.id, center.clone().add(offset));
			memberIndex++;
		}
	}

	for (const orphan of orphans) {
		const aff = positionForValue(orphan);
		aff.normalize().multiplyScalar(FIELD_RADIUS + 12);
		positions.set(orphan.id, aff);
	}

	return positions;
}

/*
layoutAroundReference (queries preset) tells the "who is selecting
whom" story. Recruiters anchor a fibonacci sphere; SELECTED Values
cluster on a small local sphere around the recruiter named in their
`reference` lane. Everything else falls back to its affinity position
so the user can still see where uninvolved Values would be without
losing the story foreground.
*/
function layoutAroundReference(
	values: ReadonlyMap<string, StoredValue>,
): Map<string, THREE.Vector3> {
	const positions = new Map<string, THREE.Vector3>();
	const recruiters: StoredValue[] = [];

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		if (communityIsRecruiter(stored)) {
			recruiters.push(stored);
		}
	}

	recruiters.sort((a, b) => a.id.localeCompare(b.id));
	const recruiterCenters = new Map<string, THREE.Vector3>();

	for (let i = 0; i < recruiters.length; i++) {
		const center = fibonacciSphere(
			i,
			Math.max(1, recruiters.length),
			QUERY_RECRUITER_RADIUS,
		);
		recruiterCenters.set(recruiters[i].id, center);
		positions.set(recruiters[i].id, center.clone());
	}

	const queriesByRef = new Map<string, StoredValue[]>();

	for (const stored of values.values()) {
		if (!stored.decoded || positions.has(stored.id)) {
			continue;
		}

		const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		if (status !== STATUS_SELECTED) {
			continue;
		}

		const reference = stored.decoded.words[REFERENCE_WORD] ?? 0n;
		if (reference === 0n) {
			continue;
		}

		const refHex = reference.toString(16).padStart(16, "0");
		if (!recruiterCenters.has(refHex)) {
			continue;
		}

		let bucket = queriesByRef.get(refHex);
		if (!bucket) {
			bucket = [];
			queriesByRef.set(refHex, bucket);
		}
		bucket.push(stored);
	}

	for (const [refHex, bucket] of queriesByRef) {
		const center = recruiterCenters.get(refHex);
		if (!center) {
			continue;
		}

		bucket.sort((a, b) => a.id.localeCompare(b.id));
		for (let j = 0; j < bucket.length; j++) {
			const offset = fibonacciSphere(j, bucket.length, QUERY_LOCAL_RADIUS);
			positions.set(bucket[j].id, center.clone().add(offset));
		}
	}

	for (const stored of values.values()) {
		if (positions.has(stored.id)) {
			continue;
		}
		positions.set(stored.id, positionForValue(stored));
	}

	return positions;
}

/*
layoutPromptRow lays prompts in a horizontal row so the user can scan
ingress at a glance, lifts the recruiters they target onto a parallel
row above, and clusters each recruiter's community around it. Prompts
without a resolvable target community are still placed in the row so
none disappear; their accent edge simply has nowhere to land.
Bystanders fall back to affinity coordinates pushed onto the back wall
so the lower-foreground space stays free for the prompts.
*/
function layoutPromptRow(
	values: ReadonlyMap<string, StoredValue>,
): Map<string, THREE.Vector3> {
	const positions = new Map<string, THREE.Vector3>();
	const prompts: StoredValue[] = [];
	const recruiterById = new Map<string, StoredValue>();

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
		if (role === VALUE_ROLE.Prompt) {
			prompts.push(stored);
		}

		if (communityIsRecruiter(stored)) {
			recruiterById.set(stored.id, stored);
		}
	}

	prompts.sort((a, b) => a.id.localeCompare(b.id));

	const promptOffsetX = -((prompts.length - 1) / 2) * PROMPT_SPACING;
	const orderedRecruiterIds: string[] = [];
	const recruiterOrderIndex = new Map<string, number>();

	for (let i = 0; i < prompts.length; i++) {
		const stored = prompts[i];
		positions.set(
			stored.id,
			new THREE.Vector3(promptOffsetX + i * PROMPT_SPACING, PROMPT_ROW_Y, 0),
		);

		if (!stored.decoded) {
			continue;
		}

		const targetWord = stored.decoded.words[TARGET_WORD] ?? 0n;
		const communityWord = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
		const targetHex =
			(targetWord !== 0n
				? targetWord.toString(16).padStart(16, "0")
				: communityWord !== 0n
					? communityWord.toString(16).padStart(16, "0")
					: "");

		if (targetHex && recruiterById.has(targetHex) && !recruiterOrderIndex.has(targetHex)) {
			recruiterOrderIndex.set(targetHex, orderedRecruiterIds.length);
			orderedRecruiterIds.push(targetHex);
		}
	}

	for (const stored of recruiterById.values()) {
		if (!recruiterOrderIndex.has(stored.id)) {
			recruiterOrderIndex.set(stored.id, orderedRecruiterIds.length);
			orderedRecruiterIds.push(stored.id);
		}
	}

	const recOffsetX =
		-((orderedRecruiterIds.length - 1) / 2) * PROMPT_SPACING;

	for (let i = 0; i < orderedRecruiterIds.length; i++) {
		positions.set(
			orderedRecruiterIds[i],
			new THREE.Vector3(
				recOffsetX + i * PROMPT_SPACING,
				PROMPT_RECRUITER_Y,
				0,
			),
		);
	}

	const memberCounter = new Map<string, number>();
	const memberTotals = new Map<string, number>();

	for (const stored of values.values()) {
		if (!stored.decoded || positions.has(stored.id)) {
			continue;
		}

		const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
		if (community === 0n) {
			continue;
		}

		const recHex = community.toString(16).padStart(16, "0");
		if (!positions.has(recHex)) {
			continue;
		}

		memberTotals.set(recHex, (memberTotals.get(recHex) ?? 0) + 1);
	}

	for (const stored of values.values()) {
		if (!stored.decoded || positions.has(stored.id)) {
			continue;
		}

		const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
		if (community !== 0n) {
			const recHex = community.toString(16).padStart(16, "0");
			const recCenter = positions.get(recHex);
			const total = memberTotals.get(recHex) ?? 1;

			if (recCenter) {
				const idx = memberCounter.get(recHex) ?? 0;
				memberCounter.set(recHex, idx + 1);
				const offset = fibonacciSphere(idx, total, PROMPT_LOCAL_RADIUS);
				positions.set(stored.id, recCenter.clone().add(offset));
				continue;
			}
		}

		const aff = positionForValue(stored);
		aff.multiplyScalar(0.55);
		aff.z -= FIELD_RADIUS * 0.9;
		positions.set(stored.id, aff);
	}

	return positions;
}

/*
layoutForPreset is the dispatcher. Each preset paints a different
spatial story, but they all return the same Map<id, Vector3> shape so
the rest of the snapshot pipeline does not branch on preset.
*/
function layoutForPreset(
	values: ReadonlyMap<string, StoredValue>,
	preset: ScenePreset,
): Map<string, THREE.Vector3> {
	if (preset === "recruitment") {
		return layoutByCommunity(values);
	}

	if (preset === "queries") {
		return layoutAroundReference(values);
	}

	if (preset === "prompts") {
		return layoutPromptRow(values);
	}

	const positions = new Map<string, THREE.Vector3>();
	for (const stored of values.values()) {
		positions.set(stored.id, positionForValue(stored));
	}
	return positions;
}

function rotateLeft64(word: bigint, amount: number): bigint {
	const mask = (1n << 64n) - 1n;
	const shift = BigInt(amount & 63);

	return ((word << shift) | (word >> (64n - shift))) & mask;
}

function wordToUnit(word: bigint): number {
	const lo = Number(word & 0xffffffffn);
	const hi = Number((word >> 32n) & 0xffffffffn);

	return ((lo ^ hi) >>> 0) / 0x1_0000_0000;
}

/*
colorForCommunity hashes a stable communityId into a deterministic hue
on the HSL ring. Recruiters (Values whose community equals their own
id) deserve a slightly brighter rendering so the eye can spot the
header among its members; communityIsRecruiter() is the cheap test.
*/
export function colorForCommunity(communityId: number): THREE.Color {
	if (communityId <= 0) {
		return ORPHAN_COLOR.clone();
	}

	const hue = (Math.imul(communityId, 2654435761) >>> 0) / 0xffffffff;

	return new THREE.Color().setHSL(hue, 0.65, 0.55);
}

function communityIsRecruiter(stored: StoredValue): boolean {
	if (!stored.decoded) {
		return false;
	}

	const id = stored.decoded.regions.id.words[0] ?? 0n;
	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;

	return community !== 0n && community === id;
}

function classificationHashColor(name: string): THREE.Color {
	if (!name) {
		return ORPHAN_COLOR.clone();
	}

	let hash = 2166136261;
	for (let idx = 0; idx < name.length; idx++) {
		hash ^= name.charCodeAt(idx);
		hash = Math.imul(hash, 16777619);
	}

	const hue = (hash >>> 0) / 0xffffffff;

	return new THREE.Color().setHSL(hue, 0.5, 0.6);
}

function colorForValue(
	stored: StoredValue,
	mode: ColorMode,
	isRecruiter: boolean,
): THREE.Color {
	if (!stored.decoded) {
		return ORPHAN_COLOR.clone();
	}

	if (mode === "status") {
		const code = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		return (STATUS_COLORS[code] ?? ORPHAN_COLOR).clone();
	}

	if (mode === "role") {
		const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
		return (ROLE_COLORS[role] ?? ORPHAN_COLOR).clone();
	}

	if (mode === "firmware") {
		return classificationHashColor(
			stored.classification.program || stored.classification.category,
		);
	}

	const color = colorForCommunity(stored.communityId);
	if (isRecruiter) {
		color.offsetHSL(0, 0, 0.15);
	}

	return color;
}

/*
geometryKindForValue picks one of five primitive shapes per Value so
the eye can read role at a glance, before resolving color or position.

  torus       ← recruiter (community == id) — the "header" of a community
  octahedron  ← Prompt role — incoming probe, distinct from anything else
  cube        ← Programmer / Learner / Readout — kernel-side actors
  cone        ← Association role — residue / linker frames
  sphere      ← everything else (default)
*/
function geometryKindForValue(
	stored: StoredValue,
	isRecruiter: boolean,
): GeometryKind {
	if (isRecruiter) {
		return "torus";
	}

	if (!stored.decoded) {
		return "sphere";
	}

	const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
	if (role === VALUE_ROLE.Prompt) {
		return "octahedron";
	}

	if (
		role === VALUE_ROLE.Programmer ||
		role === VALUE_ROLE.Learner ||
		role === VALUE_ROLE.Readout
	) {
		return "cube";
	}

	if (role === VALUE_ROLE.Association) {
		return "cone";
	}

	return "sphere";
}

/*
applyActivityGlow brightens a base color when the Value committed
recently. ticksSinceTouch == 0 means "this Value just changed at the
cursor's tick" — full glow. The brightness fades linearly across
GLOW_FADE_TICKS so a streak of consecutive activity reads as a comet
trail when the user plays through history. A subtle desaturation on
older Values helps the active subset stand forward.
*/
const GLOW_FADE_TICKS = 4;

function applyActivityGlow(
	color: THREE.Color,
	ticksSinceTouch: number,
): THREE.Color {
	if (ticksSinceTouch <= 0) {
		const out = color.clone();
		out.offsetHSL(0, 0.1, 0.18);
		return out;
	}

	if (ticksSinceTouch >= GLOW_FADE_TICKS) {
		const out = color.clone();
		const hsl = { h: 0, s: 0, l: 0 };
		out.getHSL(hsl);
		out.setHSL(hsl.h, hsl.s * 0.65, hsl.l * 0.85);
		return out;
	}

	const intensity = 1 - ticksSinceTouch / GLOW_FADE_TICKS;
	const out = color.clone();
	out.offsetHSL(0, intensity * 0.05, intensity * 0.12);
	return out;
}

/*
buildSnapshot is the only function the React component calls to turn
view-values output into the renderer's dataset. The returned snapshot
is a flat record by design: instance arrays are bucketed by geometry
in the renderer, not here, so this function stays free to change shape
heuristics without touching three.js code.
*/
export function buildSnapshot(
	values: ReadonlyMap<string, StoredValue>,
	ticksSinceTouch: ReadonlyMap<string, number>,
	mode: ColorMode,
	preset: ScenePreset,
	selectedId: string | null,
): SceneSnapshot {
	const instances: InstanceState[] = [];
	const chainEdges: EdgeState[] = [];
	const accentEdges: EdgeState[] = [];
	let selectedPosition: THREE.Vector3 | null = null;

	const positions = layoutForPreset(values, preset);

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		const isRecruiter = communityIsRecruiter(stored);
		const baseColor = colorForValue(stored, mode, isRecruiter);
		const sinceTouch = ticksSinceTouch.get(stored.id) ?? GLOW_FADE_TICKS;
		const color = applyActivityGlow(baseColor, sinceTouch);
		const position = positions.get(stored.id) ?? new THREE.Vector3();
		const dimmed = !matchesPreset(stored, preset);
		const statusCode = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		const hasContinuation =
			(stored.decoded.words[CONTINUATION_WORD] ?? 0n) !== 0n;

		let scale = 1.0;
		if (stored.id === selectedId) {
			scale = 2.4;
			selectedPosition = position.clone();
		} else if (isRecruiter) {
			scale = 1.7;
		} else if (statusCode === STATUS_BUSY) {
			scale = 1.35;
		} else if (statusCode === STATUS_SELECTED) {
			scale = 1.2;
		}

		instances.push({
			id: stored.id,
			kind: geometryKindForValue(stored, isRecruiter),
			position,
			color,
			scale,
			dimmed,
			statusCode,
			hasContinuation,
		});
	}

	collectChainEdges(chainEdges, values, positions);
	collectSelectedEdges(accentEdges, values, positions, selectedId);
	collectPresetEdges(accentEdges, values, positions, preset);

	return { instances, chainEdges, accentEdges, selectedPosition };
}

function matchesPreset(stored: StoredValue, preset: ScenePreset): boolean {
	if (preset === "all" || !stored.decoded) {
		return true;
	}

	const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
	const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;

	if (preset === "queries") {
		return status === STATUS_SELECTED || communityIsRecruiter(stored);
	}

	if (preset === "recruitment") {
		return community !== 0n;
	}

	if (preset === "prompts") {
		return role === VALUE_ROLE.Prompt;
	}

	return true;
}

/*
collectChainEdges always runs: the prev/next graph is the substrate's
own causal scaffold and the visualiser should never hide it. We dedupe
by id-pair so a back-link and a forward-link between the same pair only
draw one line.
*/
function collectChainEdges(
	out: EdgeState[],
	values: ReadonlyMap<string, StoredValue>,
	positions: Map<string, THREE.Vector3>,
) {
	const seen = new Set<string>();
	const color = new THREE.Color(0x4b5563);

	for (const stored of values.values()) {
		const from = positions.get(stored.id);
		if (!from || !stored.decoded) {
			continue;
		}

		for (const neighbour of [stored.decoded.prevId, stored.decoded.nextId]) {
			if (!neighbour) {
				continue;
			}

			const target = positions.get(neighbour);
			if (!target) {
				continue;
			}

			const key =
				stored.id < neighbour
					? `${stored.id}|${neighbour}`
					: `${neighbour}|${stored.id}`;
			if (seen.has(key)) {
				continue;
			}
			seen.add(key);

			out.push({ from, to: target, color });
		}
	}
}

function collectSelectedEdges(
	out: EdgeState[],
	values: ReadonlyMap<string, StoredValue>,
	positions: Map<string, THREE.Vector3>,
	selectedId: string | null,
) {
	if (!selectedId) {
		return;
	}

	const stored = values.get(selectedId);
	if (!stored?.decoded) {
		return;
	}

	const from = positions.get(selectedId);
	if (!from) {
		return;
	}

	const link = (idHex: string, color: THREE.Color) => {
		if (!idHex) {
			return;
		}

		const target = positions.get(idHex);
		if (!target) {
			return;
		}

		out.push({ from, to: target, color });
	};

	const referenceWord = stored.decoded.words[REFERENCE_WORD] ?? 0n;
	if (referenceWord !== 0n) {
		link(referenceWord.toString(16).padStart(16, "0"), new THREE.Color(0x06b6d4));
	}

	const targetWord = stored.decoded.words[TARGET_WORD] ?? 0n;
	if (targetWord !== 0n) {
		link(targetWord.toString(16).padStart(16, "0"), new THREE.Color(0xff2bd6));
	}

	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
	if (community !== 0n) {
		const recruiterHex = community.toString(16).padStart(16, "0");
		if (recruiterHex !== selectedId) {
			link(recruiterHex, new THREE.Color(0xfacc15));
		}
	}
}

/*
collectPresetEdges draws the in-band relationships that explain a
preset even before any Value is clicked. Recruitment shows every
member→recruiter spoke; queries show every SELECTED Value pointing at
the recruiter recorded in its `reference` lane; prompts trace each
prompt Value to the recruiter behind its target community.
*/
function collectPresetEdges(
	out: EdgeState[],
	values: ReadonlyMap<string, StoredValue>,
	positions: Map<string, THREE.Vector3>,
	preset: ScenePreset,
) {
	if (preset === "all") {
		return;
	}

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		const from = positions.get(stored.id);
		if (!from) {
			continue;
		}

		if (preset === "recruitment") {
			const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
			if (community === 0n) {
				continue;
			}

			const recruiterHex = community.toString(16).padStart(16, "0");
			if (recruiterHex === stored.id) {
				continue;
			}

			const target = positions.get(recruiterHex);
			if (!target) {
				continue;
			}

			out.push({
				from,
				to: target,
				color: colorForCommunity(stored.communityId),
			});
			continue;
		}

		if (preset === "queries") {
			const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
			if (status !== STATUS_SELECTED) {
				continue;
			}

			const reference = stored.decoded.words[REFERENCE_WORD] ?? 0n;
			if (reference === 0n) {
				continue;
			}

			const refHex = reference.toString(16).padStart(16, "0");
			const target = positions.get(refHex);
			if (!target) {
				continue;
			}

			out.push({ from, to: target, color: new THREE.Color(0x06b6d4) });
			continue;
		}

		if (preset === "prompts") {
			const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
			if (role !== VALUE_ROLE.Prompt) {
				continue;
			}

			const targetWord = stored.decoded.words[TARGET_WORD] ?? 0n;
			if (targetWord !== 0n) {
				const targetHex = targetWord.toString(16).padStart(16, "0");
				const targetPos = positions.get(targetHex);
				if (targetPos) {
					out.push({
						from,
						to: targetPos,
						color: new THREE.Color(0xff2bd6),
					});
				}
			}

			const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
			if (community !== 0n) {
				const recruiterHex = community.toString(16).padStart(16, "0");
				const recruiterPos = positions.get(recruiterHex);
				if (recruiterPos) {
					out.push({
						from,
						to: recruiterPos,
						color: new THREE.Color(0xfde68a),
					});
				}
			}
		}
	}
}
