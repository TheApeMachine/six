import * as THREE from "three";
import {
	PROGRAM_CATEGORIES,
	type ProgramCategory,
} from "./programClassifier";
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
const COMMUNITY_LOCAL_BASE_RADIUS = 5;
const QUERY_ARC_X_HALF = 60;
const QUERY_RECRUITER_Y = 24;
const QUERY_LOCAL_Y = -22;
const PROMPT_COLUMN_X = 50;
const PROMPT_COLUMN_Y_HALF = 38;
const PROMPT_LOCAL_RADIUS = 5;
const COLOR_LIGHTNESS_FLOOR = 0.45;
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

/*
GeometryKind enumerates every 3D primitive the renderer keeps as its
own InstancedMesh. The list is intentionally wide: each named program
category gets its own silhouette so the operator can read what a Value
is doing from any camera angle, before resolving color or label. The
extra kinds cost one draw call apiece; everything else here scales
inside the per-kind instance buffer.
*/
export type GeometryKind =
	| "sphere"
	| "cube"
	| "cone"
	| "cone_down"
	| "octahedron"
	| "tetrahedron"
	| "dodecahedron"
	| "icosahedron"
	| "torus"
	| "torus_knot"
	| "cylinder";

/*
Color palettes are tuned to sit comfortably above the dark background.
Every base color is pushed toward the saturated, neon end of the
spectrum so the renderer reads as a luminous diagram rather than a
muted clay model. Status/role colors keep the same hue identity as the
legend but at full saturation; orphans sit at a dim slate so they
never compete with the foreground.
*/
const STATUS_COLORS: Record<number, THREE.Color> = {
	0: new THREE.Color(0x7dd3fc), // PENDING — sky-300, calm but bright
	1: new THREE.Color(0x34ffb1), // READY — neon emerald
	2: new THREE.Color(0xffb020), // BUSY — vivid amber
	3: new THREE.Color(0x60a5fa), // WAITING — bright blue
	4: new THREE.Color(0x22ffe6), // SELECTED — neon cyan
	5: new THREE.Color(0xc7d2fe), // DONE — pale indigo, alive but receded
	6: new THREE.Color(0xc084fc), // RESOLVED — neon violet
	7: new THREE.Color(0xff5566), // ERROR — vivid red
};

const ROLE_COLORS: Record<number, THREE.Color> = {
	0: new THREE.Color(0xb8c4d0), // None — bright slate (still visible, no hue)
	1: new THREE.Color(0xff8a3d), // Programmer — bright orange
	2: new THREE.Color(0x2dffd1), // Learner — neon teal
	3: new THREE.Color(0xffe338), // Readout — bright yellow
	4: new THREE.Color(0xc084fc), // Association — neon purple
	[VALUE_ROLE.Prompt]: new THREE.Color(0xfff03a), // Prompt — bright yellow
};

const ORPHAN_COLOR = new THREE.Color(0x4b5563); // dim slate so orphans recede

function getStatusColor(status: number): THREE.Color {
	return (STATUS_COLORS[status] ?? ORPHAN_COLOR).clone();
}

function getRoleColor(role: number): THREE.Color {
	return (ROLE_COLORS[role] ?? ORPHAN_COLOR).clone();
}

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

/*
HaloState describes a translucent bubble drawn around a community so
the operator can pick out clusters without having to read individual
swatches. The renderer treats halos as a separate InstancedMesh so any
number of communities is one draw call.
*/
export interface HaloState {
	id: string;
	position: THREE.Vector3;
	radius: number;
	color: THREE.Color;
}

/*
LabelState encodes everything the DOM label overlay needs to show next
to a salient Value without a second pass over the value map. Salient
means: the Value is the user's selection, its immediate neighbour, a
recruiter of a non-trivial community, a Prompt, in a transitional
status (BUSY/WAITING/SELECTED/ERROR), or carrying high surprisal.
Everything else stays as a bare instance — labels are an attention
budget, not a default.
*/
export interface LabelState {
	id: string;
	position: THREE.Vector3;
	badge: string;
	badgeColor: string;
	primary: string;
	secondary?: string;
	detail?: string;
	highlight?: boolean;
	isPrompt?: boolean;
}

export interface SceneSnapshot {
	instances: InstanceState[];
	chainEdges: EdgeState[];
	accentEdges: EdgeState[];
	selectedPosition: THREE.Vector3 | null;
	labels: LabelState[];
	halos: HaloState[];
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
		// sqrt scaling keeps the cluster footprint proportional to member
		// count without ballooning huge communities into the next cluster.
		const localRadius =
			COMMUNITY_LOCAL_BASE_RADIUS + Math.sqrt(bucket.length) * 1.4;

		const recruiterPresent = bucket.some((stored) => stored.id === communityHex);
		const memberCount = Math.max(1, bucket.length - (recruiterPresent ? 1 : 0));

		let memberIndex = 0;
		for (const stored of bucket) {
			if (stored.id === communityHex) {
				positions.set(stored.id, center.clone());
				continue;
			}

			const offset = fibonacciSphere(memberIndex, memberCount, localRadius);
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
layoutAroundReference (queries preset) is bipartite by construction:
recruiters on top, the SELECTED Values that point at them on the
bottom. We render that literally — recruiters spaced along an upper
horizontal arc, queries fanned out below their recruiter on a lower
arc — instead of the previous fibonacci sphere where queries hid
behind their recruiters when the camera rotated. Bystanders fall back
to their affinity position pushed to the back wall so the bipartite
foreground stays uncluttered.
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

	const recruiterCount = Math.max(1, recruiters.length);
	const recruiterStep =
		recruiterCount === 1 ? 0 : (QUERY_ARC_X_HALF * 2) / (recruiterCount - 1);
	const recruiterCenters = new Map<string, THREE.Vector3>();

	for (let i = 0; i < recruiters.length; i++) {
		const x =
			recruiterCount === 1 ? 0 : -QUERY_ARC_X_HALF + i * recruiterStep;
		const center = new THREE.Vector3(x, QUERY_RECRUITER_Y, 0);
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
		const fanWidth = Math.min(
			recruiterStep > 0 ? recruiterStep * 0.8 : 18,
			6 + bucket.length * 1.5,
		);
		const fanStep = bucket.length === 1 ? 0 : fanWidth / (bucket.length - 1);

		for (let j = 0; j < bucket.length; j++) {
			const x =
				bucket.length === 1
					? center.x
					: center.x - fanWidth / 2 + j * fanStep;
			positions.set(
				bucket[j].id,
				new THREE.Vector3(x, QUERY_LOCAL_Y, 0),
			);
		}
	}

	for (const stored of values.values()) {
		if (positions.has(stored.id)) {
			continue;
		}

		const aff = positionForValue(stored);
		aff.multiplyScalar(0.5);
		aff.z -= FIELD_RADIUS * 0.9;
		positions.set(stored.id, aff);
	}

	return positions;
}

/*
layoutPromptColumns is the prompts preset. Most ticks have 0–1 prompts
so any layout that scales width by prompt count collapses to nothing —
hence two fixed columns: prompts on the left, the recruiters they
target on the right, edges crossing the middle. Prompts without a
resolvable target still land in the left column; their accent edge
simply has nowhere to go. Members of a targeted community cluster
locally around their recruiter so the right column doubles as a
"who's home" probe. Bystanders fall back to affinity coordinates
pushed onto the back wall so the foreground stays clean.

The receivedAtMs ordering is intentional: newer prompts sink to the
bottom of the left column, so the eye reads top-to-bottom as oldest →
newest ingress.
*/
function layoutPromptColumns(
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

	prompts.sort((a, b) => a.receivedAtMs - b.receivedAtMs);

	const orderedRecruiterIds: string[] = [];
	const recruiterOrderIndex = new Map<string, number>();
	const targetHexFor = new Map<string, string>();

	for (const stored of prompts) {
		if (!stored.decoded) {
			continue;
		}

		const targetWord = stored.decoded.words[TARGET_WORD] ?? 0n;
		const communityWord = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
		const targetHex =
			targetWord !== 0n
				? targetWord.toString(16).padStart(16, "0")
				: communityWord !== 0n
					? communityWord.toString(16).padStart(16, "0")
					: "";

		if (
			targetHex &&
			recruiterById.has(targetHex) &&
			!recruiterOrderIndex.has(targetHex)
		) {
			recruiterOrderIndex.set(targetHex, orderedRecruiterIds.length);
			orderedRecruiterIds.push(targetHex);
		}

		if (targetHex) {
			targetHexFor.set(stored.id, targetHex);
		}
	}

	for (const stored of recruiterById.values()) {
		if (!recruiterOrderIndex.has(stored.id)) {
			recruiterOrderIndex.set(stored.id, orderedRecruiterIds.length);
			orderedRecruiterIds.push(stored.id);
		}
	}

	// Place prompts down the left column, distributing y across the
	// available height so even a single prompt sits where the user
	// expects (just above center) instead of at y=0 hidden by overlay
	// chrome.
	const promptCount = Math.max(1, prompts.length);
	const promptStep =
		promptCount === 1 ? 0 : (PROMPT_COLUMN_Y_HALF * 2) / (promptCount - 1);
	const promptYFor = new Map<string, number>();

	for (let i = 0; i < prompts.length; i++) {
		const y =
			promptCount === 1
				? PROMPT_COLUMN_Y_HALF * 0.4
				: PROMPT_COLUMN_Y_HALF - i * promptStep;
		promptYFor.set(prompts[i].id, y);
		positions.set(prompts[i].id, new THREE.Vector3(-PROMPT_COLUMN_X, y, 0));
	}

	// Recruiters: prefer the y of any prompt that targets them so the
	// edge crosses the page horizontally rather than diagonally; fall
	// back to evenly spaced positions for recruiters nobody targets.
	const recruiterCount = Math.max(1, orderedRecruiterIds.length);
	const recruiterStep =
		recruiterCount === 1
			? 0
			: (PROMPT_COLUMN_Y_HALF * 2) / (recruiterCount - 1);

	for (let i = 0; i < orderedRecruiterIds.length; i++) {
		const recHex = orderedRecruiterIds[i];
		let y =
			recruiterCount === 1
				? 0
				: PROMPT_COLUMN_Y_HALF - i * recruiterStep;

		for (const [promptId, hex] of targetHexFor) {
			if (hex === recHex) {
				const py = promptYFor.get(promptId);
				if (py !== undefined) {
					y = py;
					break;
				}
			}
		}

		positions.set(recHex, new THREE.Vector3(PROMPT_COLUMN_X, y, 0));
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
		aff.multiplyScalar(0.5);
		aff.z -= FIELD_RADIUS * 0.9;
		positions.set(stored.id, aff);
	}

	return positions;
}

/*
layoutForPreset is the dispatcher. The default "all" layout uses the
affinity-hash projection — random-looking but stable, with the camera
free to rotate around a 3D point cloud. Cramming community clusters
into a fibonacci-packed sphere collapsed to a single fog ball when
hundreds of communities existed, so we leave the spatial story to
the explicit recruitment / queries / prompts presets.
*/
function layoutForPreset(
	values: ReadonlyMap<string, StoredValue>,
	preset: ScenePreset,
): Map<string, THREE.Vector3> {
	if (preset === "queries") {
		return layoutAroundReference(values);
	}

	if (preset === "prompts") {
		return layoutPromptColumns(values);
	}

	if (preset === "recruitment") {
		return layoutByCommunity(values);
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
on the HSL ring. The hue is rendered at near-full saturation and a
generous lightness so each cluster reads as a distinct neon swatch
against #05050f, instead of the dishwater pastels that fall out of a
default 0.7 / 0.5 palette. Recruiters get a small lightness lift on
top of the base so the community header pops out of its members.
*/
export function colorForCommunity(communityId: number): THREE.Color {
	if (communityId <= 0) {
		return ORPHAN_COLOR.clone();
	}

	const hue = (Math.imul(communityId, 2654435761) >>> 0) / 0xffffffff;

	return new THREE.Color().setHSL(hue, 0.95, 0.62);
}

function communityIsRecruiter(stored: StoredValue): boolean {
	if (!stored.decoded) {
		return false;
	}

	const id = stored.decoded.regions.id.words[0] ?? 0n;
	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;

	return community !== 0n && community === id;
}

/*
colorForCategory returns the neon swatch the legend uses for a named
program category. The legend stores 0–255 RGB triples so the color
mode ("firmware") shows the same swatch the user sees in the side
panel — there is no second hash table for the renderer.
*/
function colorForCategory(category: ProgramCategory): THREE.Color {
	const [r, g, b] = PROGRAM_CATEGORIES[category].color;

	return new THREE.Color(r / 255, g / 255, b / 255);
}

/*
PROMPT_COLOR is the permanent bright yellow we paint on every Value
carrying the Prompt role, regardless of the active color mode.
Prompts are the substrate's ingress points — the operator asked for
them in a glaring yellow so they jump out of the field at any zoom.
The hue is unique among the role / community / status palettes so it
never clashes with another bucket.
*/
const PROMPT_COLOR = new THREE.Color(0xfff03a);

function colorForValue(
	stored: StoredValue,
	mode: ColorMode,
	isRecruiter: boolean,
): THREE.Color {
	if (!stored.decoded) {
		return ORPHAN_COLOR.clone();
	}

	const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
	if (role === VALUE_ROLE.Prompt) {
		return PROMPT_COLOR.clone();
	}

	if (mode === "status") {
		const code = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		return getStatusColor(code);
	}

	if (mode === "role") {
		return getRoleColor(role);
	}

	if (mode === "firmware") {
		return colorForCategory(stored.classification.category);
	}

	const color = colorForCommunity(stored.communityId);
	if (isRecruiter) {
		color.offsetHSL(0, 0, 0.12);
	}

	return color;
}

/*
GEOMETRY_FOR_CATEGORY assigns a unique 3D silhouette to each named
program family so the operator can identify what a Value is doing
purely from its shape. The mapping mirrors the 2D legend glyphs the
user already learned: diamond → tetrahedron (sharp), triangle_up →
cone, triangle_down → cone_down, square → cube, ring → torus, plus →
cylinder (a "post"), hourglass → icosahedron, concentric → torus_knot,
pentagon → dodecahedron. Where two categories share a family glyph
(query/gap_probe both diamonds) the renderer disambiguates by color —
the legend uses the same scheme.
*/
const GEOMETRY_FOR_CATEGORY: Record<ProgramCategory, GeometryKind> = {
	query: "tetrahedron",
	plumbing: "sphere",
	structural: "cone_down",
	beam: "cone",
	inference: "dodecahedron",
	classify: "cube",
	peer_gap: "icosahedron",
	consensus: "torus_knot",
	intervene: "octahedron",
	gap_probe: "tetrahedron",
	resident: "torus",
	recruiter: "cylinder",
	util: "cylinder",
	unknown: "sphere",
};

/*
geometryKindForValue picks the shape for a Value. Prompt and community
recruiter wins are absolute — those silhouettes (octahedron, torus)
are the substrate's most visually loaded events and must never be
overwritten. Otherwise, when a recognised program is installed, the
shape comes from that program's category so the operator can read
program identity from the silhouette alone. Values without an
installed program fall back to a role-driven shape — cubes for the
kernel-side actors, cone for Association residue, sphere for raw
data.
*/
function geometryKindForValue(
	stored: StoredValue,
	isRecruiter: boolean,
): GeometryKind {
	if (!stored.decoded) {
		return "sphere";
	}

	const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
	if (role === VALUE_ROLE.Prompt) {
		return "octahedron";
	}

	if (isRecruiter) {
		return "torus";
	}

	const category = stored.classification.category;
	if (category !== "unknown") {
		return GEOMETRY_FOR_CATEGORY[category];
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
trail when the user plays through history. The aged branch nudges the
color toward grey-blue but is floored at COLOR_LIGHTNESS_FLOOR so a
quiescent Value never disappears into the dark background.
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
		out.setHSL(
			hsl.h,
			Math.max(0.45, hsl.s * 0.85),
			Math.max(COLOR_LIGHTNESS_FLOOR, hsl.l * 0.95),
		);
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
		const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
		const isPrompt = role === VALUE_ROLE.Prompt;
		const baseColor = colorForValue(stored, mode, isRecruiter);
		const sinceTouch = ticksSinceTouch.get(stored.id) ?? GLOW_FADE_TICKS;
		// Prompts must read as "live ingress" at all times; force a
		// recent-activity glow so the magenta stays bright even when the
		// substrate hasn't touched the Value this tick.
		const glowTicks = isPrompt ? Math.min(sinceTouch, 0) : sinceTouch;
		const color = applyActivityGlow(baseColor, glowTicks);
		const position = positions.get(stored.id) ?? new THREE.Vector3();
		const dimmed = !matchesPreset(stored, preset);
		const statusCode = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		const hasContinuation =
			(stored.decoded.words[CONTINUATION_WORD] ?? 0n) !== 0n;

		// Scale compresses inert Values and lifts active ones so the eye
		// reads the field as a population of "doing things" against a
		// quiet backdrop. Anything running a recognised program, in a
		// transitional status, or carrying a role badge inflates by at
		// least 40%; idle PENDING / READY / DONE Values shrink to 0.7
		// so the foreground has room to breathe.
		const hasProgram = stored.classification.category !== "unknown";
		const hasRole = role !== 0;
		const isActiveStatus =
			statusCode === STATUS_BUSY ||
			statusCode === 3 || // WAITING
			statusCode === STATUS_SELECTED ||
			statusCode === 6 || // RESOLVED
			statusCode === 7; // ERROR

		let scale: number;
		if (stored.id === selectedId) {
			scale = 2.6;
			selectedPosition = position.clone();
		} else if (isPrompt) {
			// Prompts dwarf bystanders so the eye lands on them first.
			scale = 2.4;
		} else if (isRecruiter) {
			scale = 1.9;
		} else if (statusCode === STATUS_BUSY || statusCode === 7) {
			scale = 1.7;
		} else if (isActiveStatus) {
			scale = 1.5;
		} else if (hasProgram) {
			scale = 1.4;
		} else if (hasRole) {
			scale = 1.1;
		} else {
			scale = 0.7;
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

	const labels = collectLabels(values, positions, selectedId);

	return {
		instances,
		chainEdges,
		accentEdges,
		selectedPosition,
		labels,
		halos: [],
	};
}

function matchesPreset(stored: StoredValue, preset: ScenePreset): boolean {
	if (preset === "all" || !stored.decoded) {
		return true;
	}

	const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
	const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
	const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;

	// Prompts are the substrate's ingress points and the operator must
	// always be able to find them; they are never dimmed by a preset
	// filter regardless of which story the user is currently watching.
	if (role === VALUE_ROLE.Prompt) {
		return true;
	}

	if (preset === "queries") {
		return status === STATUS_SELECTED || communityIsRecruiter(stored);
	}

	if (preset === "recruitment") {
		return community !== 0n;
	}

	if (preset === "prompts") {
		return false;
	}

	return true;
}

/*
MAX_LABELS bounds the DOM cost. The renderer culls labels by camera
distance every frame, so this cap is the upper limit on how many
chips can be mounted at once — the visible subset is much smaller in
practice. 600 is well above what the eye can read on a single screen
but cheap enough that the layout stays smooth.
*/
const MAX_LABELS = 600;
const SURPRISAL_WORD = PROPERTY_WORD("SURPRISAL");

const STATUS_BADGE: Record<number, [string, string]> = {
	0: ["P", "#7dd3fc"], // PENDING
	1: ["R", "#34ffb1"], // READY
	2: ["B", "#ffb020"], // BUSY
	3: ["W", "#60a5fa"], // WAITING
	4: ["S", "#22ffe6"], // SELECTED
	5: ["D", "#c7d2fe"], // DONE
	6: ["+", "#c084fc"], // RESOLVED
	7: ["!", "#ff5566"], // ERROR
};

const STATUS_NAME: Record<number, string> = {
	0: "pending",
	1: "ready",
	2: "busy",
	3: "waiting",
	4: "selected",
	5: "done",
	6: "resolved",
	7: "error",
};

const ROLE_LABEL: Record<number, string> = {
	1: "programmer",
	2: "learner",
	3: "readout",
	4: "association",
	[VALUE_ROLE.Prompt]: "prompt",
};

const ROLE_BADGE: Record<number, [string, string]> = {
	1: ["P", "#ff8a3d"],
	2: ["L", "#2dffd1"],
	3: ["O", "#ffe338"],
	4: ["A", "#c084fc"],
	[VALUE_ROLE.Prompt]: ["!", "#fff03a"],
};

// Statuses considered "active" for label priority. PENDING and READY
// are common idle states and their labels would crowd out the
// foreground information the operator actually needs to read.
const ACTIVE_STATUSES = new Set([2, 3, 4, 6, 7]);

/*
collectLabels generates a label for every Value worth annotating.
Priority order is information-density: the selected Value first, then
anything running a recognised program (so the operator reads program
identity first), then anything in an active status (BUSY, WAITING,
SELECTED, RESOLVED, ERROR), then prompts, recruiters with members,
roles, Values that carry token text, and finally everything else if
there is room. Primary text follows the same priority — program name
beats status name beats role name beats token text beats id tail —
so each chip's first line is always the most useful word about that
Value.
*/
function collectLabels(
	values: ReadonlyMap<string, StoredValue>,
	positions: Map<string, THREE.Vector3>,
	selectedId: string | null,
): LabelState[] {
	const memberCounts = new Map<string, number>();
	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}
		const community = stored.decoded.words[COMMUNITY_WORD] ?? 0n;
		if (community === 0n) {
			continue;
		}
		const recHex = community.toString(16).padStart(16, "0");
		memberCounts.set(recHex, (memberCounts.get(recHex) ?? 0) + 1);
	}

	const candidates: Array<{ stored: StoredValue; rank: number }> = [];

	for (const stored of values.values()) {
		if (!stored.decoded) {
			continue;
		}

		const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
		const isRecruiter = communityIsRecruiter(stored);
		const memberCount = isRecruiter
			? (memberCounts.get(stored.id) ?? 0)
			: 0;
		const surprisal = Number(stored.decoded.words[SURPRISAL_WORD] ?? 0n);
		const hasProgram = stored.classification.category !== "unknown";
		const hasToken = stored.tokenText.length > 0;
		const hasRole = role !== 0;

		let rank: number;
		if (stored.id === selectedId) {
			rank = 0;
		} else if (hasProgram) {
			rank = 1;
		} else if (ACTIVE_STATUSES.has(status)) {
			rank = 2;
		} else if (role === VALUE_ROLE.Prompt) {
			rank = 3;
		} else if (isRecruiter && memberCount >= 1) {
			rank = 4 - Math.min(0.99, memberCount / 100);
		} else if (hasRole) {
			rank = 5;
		} else if (surprisal >= 320) {
			rank = 6;
		} else if (hasToken) {
			rank = 7;
		} else {
			rank = 8;
		}

		candidates.push({ stored, rank });
	}

	candidates.sort((a, b) => a.rank - b.rank);

	const labels: LabelState[] = [];

	for (const { stored } of candidates) {
		if (labels.length >= MAX_LABELS) {
			break;
		}
		if (!stored.decoded) {
			continue;
		}

		const position = positions.get(stored.id);
		if (!position) {
			continue;
		}

		const status = Number(stored.decoded.words[STATUS_WORD] ?? 0n);
		const role = Number(stored.decoded.words[ROLE_WORD] ?? 0n);
		const isRecruiter = communityIsRecruiter(stored);
		const memberCount = isRecruiter
			? (memberCounts.get(stored.id) ?? 0)
			: 0;
		const programName = stored.classification.program;
		const programCategory = stored.classification.category;
		const hasProgram = programCategory !== "unknown";
		const isPrompt = role === VALUE_ROLE.Prompt;
		const tokenText = stored.tokenText
			? truncateToken(stored.tokenText)
			: "";

		// primary: program name → status (if active) → role → token →
		// id tail. badge / badgeColor follows the same priority so the
		// chip's left swatch always reflects the level of information
		// shown on its first line.
		let primary: string;
		let badge: string;
		let badgeColor: string;

		if (hasProgram) {
			const [r, g, b] = PROGRAM_CATEGORIES[programCategory].color;
			primary = programName;
			badge = programCategory[0].toUpperCase();
			badgeColor = `rgb(${r}, ${g}, ${b})`;
		} else if (ACTIVE_STATUSES.has(status)) {
			const [b, c] = STATUS_BADGE[status] ?? ["?", "#9ca3af"];
			primary = STATUS_NAME[status] ?? `status ${status}`;
			badge = b;
			badgeColor = c;
		} else if (isPrompt) {
			primary = "prompt";
			badge = "!";
			badgeColor = "#fff03a";
		} else if (ROLE_LABEL[role]) {
			const [b, c] = ROLE_BADGE[role] ?? ["?", "#9ca3af"];
			primary = ROLE_LABEL[role];
			badge = b;
			badgeColor = c;
		} else if (tokenText) {
			primary = tokenText;
			const [b, c] = STATUS_BADGE[status] ?? ["?", "#9ca3af"];
			badge = b;
			badgeColor = c;
		} else {
			primary = stored.id.slice(-8);
			const [b, c] = STATUS_BADGE[status] ?? ["?", "#9ca3af"];
			badge = b;
			badgeColor = c;
		}

		// Secondary line carries the next-most-useful piece of info that
		// did not fit in primary, so the chip is dense without
		// repeating itself.
		let secondary: string | undefined;
		if (hasProgram && ACTIVE_STATUSES.has(status)) {
			secondary = STATUS_NAME[status];
		} else if (hasProgram && isRecruiter) {
			secondary = `recruiter · ${memberCount}`;
		} else if (isRecruiter) {
			secondary = `recruiter · ${memberCount}`;
		} else if (hasProgram && ROLE_LABEL[role]) {
			secondary = ROLE_LABEL[role];
		} else if (ACTIVE_STATUSES.has(status) && ROLE_LABEL[role]) {
			secondary = ROLE_LABEL[role];
		} else {
			secondary = stored.id.slice(-8);
		}

		// detail: token text on the third line if it didn't already
		// land in primary.
		const detail = tokenText && primary !== tokenText ? tokenText : undefined;

		labels.push({
			id: stored.id,
			position,
			badge,
			badgeColor,
			primary,
			secondary,
			detail,
			highlight: stored.id === selectedId,
			isPrompt,
		});
	}

	return labels;
}

function truncateToken(text: string): string {
	const cleaned = text.replace(/\s+/g, " ").trim();
	if (!cleaned) {
		return "";
	}
	if (cleaned.length <= 28) {
		return cleaned;
	}
	return `${cleaned.slice(0, 27)}…`;
}

function collectChainEdges(
	out: EdgeState[],
	values: ReadonlyMap<string, StoredValue>,
	positions: Map<string, THREE.Vector3>,
) {
	const seen = new Set<string>();

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

			out.push({
				from,
				to: target,
				color: new THREE.Color(0x4b5563),
			});
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
