import { chainIdFromWord, readWordU64LE, WORD } from "./valueLayout";
import { decodeVizFrames, EK, KIND_NAMES, type VizEvent } from "./wire";

const ACTIONS: { name: string; color: [number, number, number] }[] = [
	{ name: "beam_swarm", color: [0, 255, 150] },
	{ name: "causal_explore", color: [255, 200, 0] },
	{ name: "active_inference", color: [255, 150, 50] },
	{ name: "classification", color: [100, 180, 255] },
];

const REACTIONS: { name: string; color: [number, number, number] }[] = [
	{ name: "surprisal", color: [0, 200, 255] },
	{ name: "falsification", color: [255, 80, 80] },
];

const PROGRAM_COLORS: Record<string, [number, number, number]> = {};
for (const a of ACTIONS) PROGRAM_COLORS[a.name] = a.color;
PROGRAM_COLORS["beam_swarm_step"] = ACTIONS[0].color;
for (const r of REACTIONS) PROGRAM_COLORS[r.name] = r.color;
PROGRAM_COLORS["affinity"] = [186, 104, 200];
PROGRAM_COLORS["aggregate"] = [186, 104, 200];

function programColor(name: string): [number, number, number] {
	return PROGRAM_COLORS[name] || [180, 180, 180];
}

/*
hslToRgb converts HSL (h∈[0,360), s/l∈[0,1]) to an RGB triple ∈ [0,255].
Used to derive stable community colors from their affinity phase vector.
*/
function hslToRgb(h: number, s: number, l: number): [number, number, number] {
	const c = (1 - Math.abs(2 * l - 1)) * s;
	const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
	const m = l - c / 2;
	let r = 0,
		g = 0,
		b = 0;
	if (h < 60) {
		r = c;
		g = x;
	} else if (h < 120) {
		r = x;
		g = c;
	} else if (h < 180) {
		g = c;
		b = x;
	} else if (h < 240) {
		g = x;
		b = c;
	} else if (h < 300) {
		r = x;
		b = c;
	} else {
		r = c;
		b = x;
	}
	return [
		Math.round((r + m) * 255),
		Math.round((g + m) * 255),
		Math.round((b + m) * 255),
	];
}

/*
affinityColor derives an RGB color from the community's affinity hex string.
The affinity is a phase vector in GF(8191); we map it to a stable hue so each
community has an identity color independent of its current action program.
*/
function affinityColor(hex: string): [number, number, number] {
	if (!hex || hex.length < 4) return [186, 104, 200];
	const val = parseInt(hex.substring(0, 8), 16);
	if (Number.isNaN(val)) return [186, 104, 200];
	const hue = ((val % 360) + 360) % 360;
	return hslToRgb(hue, 0.75, 0.62);
}

function fnv1a32(input: string): number {
	let hash = 0x811c9dc5;
	for (let i = 0; i < input.length; i++) {
		hash ^= input.charCodeAt(i);
		hash = Math.imul(hash, 0x01000193) >>> 0;
	}
	return hash >>> 0;
}

function unitFromKey(key: string, salt: string): number {
	return fnv1a32(`${key}\0${salt}`) / 0xffffffff;
}

const GOLDEN_ANGLE = 2.399963;

function worldLayoutPoint(
	key: string,
	extent: number,
	inset: number,
): { x: number; y: number } {
	const ux = unitFromKey(key, "x");
	const uy = unitFromKey(key, "y");
	const w = extent - 2 * inset;
	return { x: inset + ux * w, y: inset + uy * w };
}

let communitySeqCounter = 0;

function communitySpiralPos(
	seq: number,
	cx: number,
	cy: number,
): { x: number; y: number } {
	const baseRadius = Math.min(cx, cy) * 0.35;
	const ringRadius = baseRadius + Math.sqrt(seq) * baseRadius * 0.22;
	const angle = seq * GOLDEN_ANGLE;
	return {
		x: cx + Math.cos(angle) * ringRadius,
		y: cy + Math.sin(angle) * ringRadius,
	};
}

function layoutVelocity(key: string): { x: number; y: number } {
	const vx = unitFromKey(key, "vx") - 0.5;
	const vy = unitFromKey(key, "vy") - 0.5;
	return { x: vx * 0.8, y: vy * 0.8 };
}

function snapshotTelemetry(ev: VizEvent): {
	ts: number;
	src: string;
	tgt: string;
	lbl: string;
	vals: Record<string, number>;
	meta: Record<string, string>;
} {
	return {
		ts: ev.ts,
		src: ev.src,
		tgt: ev.tgt,
		lbl: ev.lbl,
		vals: { ...ev.vals },
		meta: { ...ev.meta },
	};
}

/*
mergeTelemetry overlays a new wire snapshot onto the previous one so later
events (belief gap, resolve) do not erase tokenizer content, queue chain ids,
or properties word hex from earlier QueueSubmit telemetry.
*/
function mergeTelemetry(
	prev: ReturnType<typeof snapshotTelemetry> | null,
	ev: VizEvent,
): ReturnType<typeof snapshotTelemetry> {
	const next = snapshotTelemetry(ev);

	if (!prev) return next;

	return {
		ts: next.ts,
		src: next.src || prev.src,
		tgt: next.tgt || prev.tgt,
		lbl: next.lbl || prev.lbl,
		vals: { ...prev.vals, ...next.vals },
		meta: { ...prev.meta, ...next.meta },
	};
}

function normalizedId(id: string | undefined): string {
	if (!id || id === "0") return "";

	const trimmed = id.trim().toLowerCase();

	if (/^[0-9a-f]+$/.test(trimmed) && trimmed.length <= 16) {
		return trimmed.padStart(16, "0");
	}

	return id;
}

function eventDurationMs(ev: VizEvent): number {
	const durationMs = ev.vals?.duration_ms;

	if (durationMs !== undefined) return durationMs;

	const durationNs = ev.vals?.duration_ns;

	if (durationNs !== undefined) return durationNs / 1e6;

	return 0;
}

function eventDurationNs(ev: VizEvent): number {
	const durationNs = ev.vals?.duration_ns;

	if (durationNs !== undefined) return durationNs;

	const durationMs = ev.vals?.duration_ms;

	if (durationMs !== undefined) return durationMs * 1e6;

	return 0;
}

interface VisValue {
	id: string;
	pos: { x: number; y: number };
	vel: { x: number; y: number };
	role: "data" | "action" | "reaction" | "prompt";
	/*
  wireFrame is the latest full Value.Bytes image from the viz binary path
	(WireFrameValue); word layout matches pkg/primitive + kernel layout.
  */
	wireFrame?: Uint8Array;
	program: string;
	communityId: number;
	label: string;
	content: string;
	resonance: number;
	gap: number;
	resolved: boolean;
	age: number;
	prevId: string;
	nextId: string;
	telemetry: ReturnType<typeof snapshotTelemetry> | null;
	actionResonance: number;
	memberIndex: number;
}

interface VisCommunity {
	id: number;
	memberIds: Set<string>;
	saturated: boolean;
	saturation: number;
	lastAction: string;
	actionCount: number;
	reactionCount: number;
	center: { x: number; y: number };
	affinityHex: string;
	concentration: number;
}

interface ALUSub {
	inflight: number;
	total: number;
	lastDurNs: number;
}

interface BeamEngineState {
	activeCount: number;
	bestScore: number;
	converged: boolean;
	lastSequence: string;
	collectCount: number;
	composeCount: number;
	breakCount: number;
	convergeCount: number;
}

interface TrieSig {
	surprisal: number;
	entropy: number;
	growth: number;
}

interface FieldDig {
	nodeId: string;
	surprisal: number;
	entropy: number;
	growth: number;
}

export interface VizRuntimeStats {
	values: number;
	communities: number;
	actions: number;
	reactions: number;
	dropped: number;
	bootstrapNodes: number;
	wireJsonBlobs: number;
}

export interface VizInspectSnapshot {
	id: string;
	role: VisValue["role"];
	program: string;
	communityId: number;
	label: string;
	content: string;
	pos: { x: number; y: number };
	resonance: number;
	gap: number;
	resolved: boolean;
	actionResonance: number;
	prevId: string;
	nextId: string;
	/*
  communityAffinityHex is the field’s initial affinity vector (five words) when
  the value belongs to a community; per-event telemetry often omits it.
  */
	communityAffinityHex: string;
	wireFrame: Uint8Array | null;
	telemetry: ReturnType<typeof snapshotTelemetry> | null;
}

/*
FieldValueSnapshot is a serializable snapshot of a single live value,
used to populate the graph viewer without holding live references.
*/
export interface FieldValueSnapshot {
	id: string;
	role: VisValue["role"];
	program: string;
	communityId: number;
	label: string;
	content: string;
	resonance: number;
	gap: number;
	resolved: boolean;
	actionResonance: number;
	prevId: string;
	nextId: string;
	communityAffinityHex: string;
	wireFrame: Uint8Array | null;
	telemetry: ReturnType<typeof snapshotTelemetry> | null;
}

/*
FieldSnapshot is a serializable snapshot of a single community (field),
including its members and aggregate metrics.
*/
export interface FieldSnapshot {
	id: number;
	memberCount: number;
	saturated: boolean;
	saturation: number;
	lastAction: string;
	actionCount: number;
	reactionCount: number;
	affinityHex: string;
	concentration: number;
	members: FieldValueSnapshot[];
}

/*
VizGraphSnapshot is a periodic snapshot of the full field/value state,
emitted every N frames so the React graph layer can re-render without
holding direct references into the engine's mutable maps.
*/
export interface VizGraphSnapshot {
	timestamp: number;
	fields: FieldSnapshot[];
	orphanValues: FieldValueSnapshot[];
	totalValues: number;
	totalCommunities: number;
}

export interface VizCallbacks {
	onEvent: (ev: VizEvent) => void;
	onStats: (stats: VizRuntimeStats) => void;
	onSelection?: (sel: VizInspectSnapshot | null) => void;
	onGraphSnapshot?: (snap: VizGraphSnapshot) => void;
}

export function initEngine(container: HTMLDivElement, callbacks: VizCallbacks) {
	const canvas = document.createElement("canvas");
	canvas.style.display = "block";
	canvas.style.cursor = "grab";
	container.appendChild(canvas);

	const ctx = canvas.getContext("2d") as CanvasRenderingContext2D;
	let W = container.clientWidth || 800;
	let H = container.clientHeight || 600;
	canvas.width = W;
	canvas.height = H;

	let camX = 0;
	let camY = 0;
	let camZoom = 1;
	let autoFitEnabled = true;

	let isPanning = false;
	let panStartX = 0;
	let panStartY = 0;
	let camStartX = 0;
	let camStartY = 0;

	let selectedId: string | null = null;

	function screenToWorld(sx: number, sy: number): { x: number; y: number } {
		return {
			x: (sx - W / 2) / camZoom + camX,
			y: (sy - H / 2) / camZoom + camY,
		};
	}

	canvas.addEventListener("mousedown", (e) => {
		isPanning = true;
		panStartX = e.clientX;
		panStartY = e.clientY;
		camStartX = camX;
		camStartY = camY;
		canvas.style.cursor = "grabbing";
		autoFitEnabled = false;
	});

	canvas.addEventListener("mousemove", (e) => {
		if (!isPanning) return;
		camX = camStartX - (e.clientX - panStartX) / camZoom;
		camY = camStartY - (e.clientY - panStartY) / camZoom;
	});

	canvas.addEventListener("mouseup", () => {
		isPanning = false;
		canvas.style.cursor = "grab";
	});

	canvas.addEventListener("mouseleave", () => {
		isPanning = false;
		canvas.style.cursor = "grab";
	});

	canvas.addEventListener("click", (e) => {
		if (
			Math.abs(e.clientX - panStartX) > 4 ||
			Math.abs(e.clientY - panStartY) > 4
		)
			return;

		const world = screenToWorld(e.offsetX, e.offsetY);
		selectedId = null;
		const hitRadius = Math.max(18, 12 / camZoom);
		// Prompts get a larger hit zone and are checked first so they are never
		// occluded by a data value that happens to share screen space.
		const promptHit = Math.max(40, 24 / camZoom);

		for (const [vid, v] of valueMap) {
			if (v.role !== "prompt") continue;
			if (Math.hypot(v.pos.x - world.x, v.pos.y - world.y) < promptHit) {
				selectedId = vid;
				break;
			}
		}

		if (!selectedId) {
			for (const [vid, v] of valueMap) {
				if (v.role === "prompt") continue;
				if (Math.hypot(v.pos.x - world.x, v.pos.y - world.y) < hitRadius) {
					selectedId = vid;
					break;
				}
			}
		}

		emitSelection();
	});

	canvas.addEventListener("dblclick", () => {
		autoFitEnabled = true;
	});

	canvas.addEventListener(
		"wheel",
		(e) => {
			e.preventDefault();
			autoFitEnabled = false;

			const world = screenToWorld(e.offsetX, e.offsetY);
			const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
			camZoom = Math.max(0.01, Math.min(10, camZoom * factor));

			camX = world.x - (e.offsetX - W / 2) / camZoom;
			camY = world.y - (e.offsetY - H / 2) / camZoom;
		},
		{ passive: false },
	);

	const ro = new ResizeObserver(() => {
		const newW = container.clientWidth;
		const newH = container.clientHeight;
		if (newW > 0 && newH > 0) {
			W = newW;
			H = newH;
			canvas.width = W;
			canvas.height = H;
		}
	});
	ro.observe(container);

	const valueMap = new Map<string, VisValue>();
	const communityMap = new Map<number, VisCommunity>();
	const eventLog: {
		ts: number;
		kind: string;
		detail: string;
		color: string;
	}[] = [];
	const stats: VizRuntimeStats = {
		values: 0,
		communities: 0,
		actions: 0,
		reactions: 0,
		dropped: 0,
		bootstrapNodes: 0,
		wireJsonBlobs: 0,
	};

	// Subsystem state — updated from specific event kinds, drawn as canvas overlays.
	const aluSubs = new Map<string, ALUSub>();
	let aluTotal = 0;
	const aluRecentOps: string[] = [];

	const beam: BeamEngineState = {
		activeCount: 0,
		bestScore: 0,
		converged: false,
		lastSequence: "",
		collectCount: 0,
		composeCount: 0,
		breakCount: 0,
		convergeCount: 0,
	};

	const trieSignals = new Map<string, TrieSig>();
	const fieldDigests = new Map<string, FieldDig>();
	const gossipSent = new Map<string, number>();
	const gossipRecv = new Map<string, number>();
	let poolScheduled = 0;
	let poolCompleted = 0;
	const adaptiveAlphas = new Map<string, number>();
	let compilerTotal = 0;
	let compilerLastProg = "";
	let finalizerTotal = 0;

	let frameCount = 0;
	let isDestroyed = false;
	let ws: WebSocket | null = null;
	let bootstrapNodeIds: string[] = [];

	const MAX_VALUES = 2000;
	const LOG_COLORS: Record<string, string> = {
		prompt: "rgba(255,180,0,0.7)",
		prompt_result: "rgba(100,255,180,0.55)",
		tokenizer: "rgba(100,180,255,0.5)",
		tokenizer_chunk: "rgba(130,160,255,0.45)",
		community: "rgba(186,104,200,0.5)",
		join: "rgba(186,104,200,0.4)",
		action: "rgba(0,255,150,0.5)",
		reaction: "rgba(255,106,64,0.5)",
		saturated: "rgba(255,80,80,0.5)",
		system: "rgba(255,255,255,0.4)",
		viz_bus: "rgba(255,120,120,0.65)",
		json: "rgba(180,255,200,0.5)",
		dataset: "rgba(120,200,255,0.45)",
		trie_graph: "rgba(255,200,120,0.5)",
		gap: "rgba(200,180,255,0.45)",
		resolved: "rgba(0,255,120,0.7)",
		emission: "rgba(255,220,0,0.6)",
		eigenmode: "rgba(140,200,255,0.5)",
		alu: "rgba(255,140,60,0.55)",
		beam: "rgba(80,200,255,0.55)",
		trie: "rgba(180,140,255,0.45)",
		pool: "rgba(160,200,160,0.45)",
		gossip: "rgba(200,220,120,0.45)",
		field: "rgba(140,180,255,0.5)",
		compiler: "rgba(200,160,255,0.5)",
		adaptive: "rgba(160,255,200,0.45)",
		causal_hub: "rgba(255,200,100,0.5)",
		queue: "rgba(160,180,255,0.45)",
		error: "rgba(255,60,60,0.7)",
	};

	function logColor(kind: string): string {
		return LOG_COLORS[kind] || "rgba(255,255,255,0.2)";
	}

	function addLog(kind: string, detail: string) {
		eventLog.unshift({ ts: Date.now(), kind, detail, color: logColor(kind) });
		if (eventLog.length > 60) eventLog.length = 60;
	}

	function emitSelection() {
		if (!callbacks.onSelection) return;

		if (!selectedId) {
			callbacks.onSelection(null);
			return;
		}

		const v = valueMap.get(selectedId);

		if (!v) {
			callbacks.onSelection(null);
			return;
		}

		const community =
			v.communityId >= 0 ? communityMap.get(v.communityId) : undefined;

		callbacks.onSelection({
			id: v.id,
			role: v.role,
			program: v.program,
			communityId: v.communityId,
			label: v.label,
			content: v.content,
			pos: { ...v.pos },
			resonance: v.resonance,
			gap: v.gap,
			resolved: v.resolved,
			actionResonance: v.actionResonance,
			prevId: v.prevId,
			nextId: v.nextId,
			communityAffinityHex: community?.affinityHex ?? "",
			wireFrame: v.wireFrame ?? null,
			telemetry: v.telemetry,
		});
	}

	function spiralOffset(index: number): { dx: number; dy: number } {
		const angle = index * GOLDEN_ANGLE;
		const radius = 14 + Math.sqrt(index) * 18;
		return { dx: Math.cos(angle) * radius, dy: Math.sin(angle) * radius };
	}

	function spawnPos(
		layoutKey: string,
		communityId?: number,
	): { x: number; y: number } {
		if (communityId !== undefined && communityId >= 0) {
			const c = communityMap.get(communityId);

			if (c) {
				const off = spiralOffset(c.memberIds.size);
				return { x: c.center.x + off.dx, y: c.center.y + off.dy };
			}
		}

		const extent = Math.max(W, H);
		return worldLayoutPoint(`spawn|${layoutKey}`, extent, 80);
	}

	function cullOldest() {
		if (valueMap.size <= MAX_VALUES) return;

		const sorted = [...valueMap.entries()]
			.filter(([, v]) => v.role !== "prompt")
			.sort((a, b) => b[1].age - a[1].age);

		const excess = valueMap.size - MAX_VALUES;

		for (let i = 0; i < Math.min(excess, sorted.length); i++) {
			const [vid, v] = sorted[i];
			valueMap.delete(vid);

			if (vid === selectedId) {
				selectedId = null;
				emitSelection();
			}

			if (v.communityId >= 0) {
				const c = communityMap.get(v.communityId);
				if (c) c.memberIds.delete(vid);
			}
		}
	}

	function ensureCommunity(cid: number): VisCommunity | null {
		if (cid < 0) return null;

		let community = communityMap.get(cid);

		if (community) return community;

		const seq = communitySeqCounter++;
		community = {
			id: cid,
			memberIds: new Set(),
			saturated: false,
			saturation: 0,
			lastAction: "",
			actionCount: 0,
			reactionCount: 0,
			center: communitySpiralPos(seq, W / 2, H / 2),
			affinityHex: "",
			concentration: 0,
		};
		communityMap.set(cid, community);

		return community;
	}

	function ensureValue(
		vid: string,
		ev: VizEvent,
		role: VisValue["role"],
		communityId: number,
	): VisValue | null {
		const id = normalizedId(vid);

		if (!id) return null;

		let value = valueMap.get(id);

		if (value) return value;

		const pos = spawnPos(`ref|${id}`, communityId);
		const vel = layoutVelocity(`ref|${id}`);
		value = {
			id,
			pos,
			vel,
			role,
			program: role === "data" ? ev.meta?.program || "" : ev.lbl || "",
			communityId,
			label: ev.lbl || id.substring(0, 12),
			content: ev.meta?.content || "",
			resonance: role === "data" ? 0.55 : 0.8,
			gap: 1,
			resolved: false,
			age: 0,
			prevId: normalizedId(ev.meta?.prev_id),
			nextId: normalizedId(ev.meta?.next_id),
			telemetry: snapshotTelemetry(ev),
			actionResonance: ev.vals?.resonance ?? 0,
			memberIndex: 0,
		};
		valueMap.set(id, value);
		cullOldest();

		return value;
	}

  function applyValueWireFrame(valueId: bigint, bytes: Uint8Array) {
    const vid = valueId.toString(16).padStart(16, "0").toLowerCase();
    const v = valueMap.get(vid);

    if (!v) return;

    v.wireFrame = bytes;

    const needBytes = (WORD.ID + 1) * 8;

    if (bytes.length < needBytes) return;

    const prevW = chainIdFromWord(readWordU64LE(bytes, WORD.PREV));
    const nextW = chainIdFromWord(readWordU64LE(bytes, WORD.NEXT));

    /*
    Do not clear chain ids when the frame shows zero: later queue snapshots can
    arrive after execute/reuse with w121=0 while QueueSubmit already carried
    next_id in meta.
    */
    if (prevW) v.prevId = prevW;

    if (nextW) v.nextId = nextW;
  }

	function applyEvent(ev: VizEvent) {
		const kindName = KIND_NAMES[ev.kind] || `kind_${ev.kind}`;
		callbacks.onEvent(ev);

		// ── Node lifecycle ──────────────────────────────────────────────────────
		if (ev.kind === EK.NodeCreated) {
			addLog("system", `node+ ${ev.src} "${ev.lbl}"`);
		} else if (ev.kind === EK.NodeUpdated) {
			addLog("system", `node~ ${ev.src}`);
		} else if (ev.kind === EK.NodeRemoved) {
			addLog("system", `node- ${ev.src}`);
		}

		// ── Peer events ─────────────────────────────────────────────────────────
		else if (ev.kind === EK.PeerAdded) {
			addLog(
				"system",
				`peer+ ${ev.src}→${ev.tgt} bkt=${ev.vals?.bucket ?? "?"}`,
			);
		} else if (ev.kind === EK.PeerRemoved) {
			addLog("system", `peer- ${ev.src}→${ev.tgt}`);
		} else if (ev.kind === EK.PeerLatency) {
			addLog(
				"system",
				`lat ${ev.src}→${ev.tgt} ${(ev.vals?.latency_ms ?? 0).toFixed(1)}ms`,
			);
		}

		// ── Data flow ───────────────────────────────────────────────────────────
		else if (ev.kind === EK.ValuePublished) {
			addLog(
				"system",
				`pub ${ev.src} key=${ev.meta?.key?.substring(0, 12) ?? "?"}`,
			);
		} else if (ev.kind === EK.ValueReplicated) {
			addLog("system", `rep ${ev.src}→${ev.tgt}`);
		}

		// ── Gossip ──────────────────────────────────────────────────────────────
		else if (ev.kind === EK.GossipSent) {
			const epoch = ev.vals?.epoch ?? 0;
			gossipSent.set(ev.src, (gossipSent.get(ev.src) ?? 0) + 1);
			addLog("gossip", `sent ${ev.src} epoch=${epoch}`);
		} else if (ev.kind === EK.GossipReceived) {
			const epoch = ev.vals?.epoch ?? 0;
			gossipRecv.set(ev.src, (gossipRecv.get(ev.src) ?? 0) + 1);
			addLog("gossip", `recv ${ev.src}←${ev.tgt} epoch=${epoch}`);
		}

		// ── Field dynamics ──────────────────────────────────────────────────────
		else if (ev.kind === EK.FieldDigest) {
			const surprisal = ev.vals?.surprisal ?? 0;
			const entropy = ev.vals?.entropy ?? 0;
			const growth = ev.vals?.growth ?? 0;
			fieldDigests.set(ev.src, { nodeId: ev.src, surprisal, entropy, growth });
			addLog(
				"field",
				`digest ${ev.src} S=${surprisal.toFixed(3)} H=${entropy.toFixed(3)} g=${growth.toFixed(3)}`,
			);
		} else if (ev.kind === EK.FieldPressure) {
			const decay = ev.vals?.decay ?? 0;
			const learn = ev.vals?.learning ?? 0;
			const prune = ev.vals?.prune ?? 0;
			addLog(
				"field",
				`pressure ${ev.src} d=${decay.toFixed(3)} l=${learn.toFixed(3)} p=${prune.toFixed(3)}`,
			);
		} else if (ev.kind === EK.EigenmodeDetected) {
			const modeCount = ev.vals?.mode_count ?? 0;
			const dominantEnergy = ev.vals?.dominant_energy ?? 0;
			addLog(
				"eigenmode",
				`${ev.src} modes=${modeCount} energy=${dominantEnergy.toFixed(3)}`,
			);
		}

		// ── Trie / value-graph instrumentation ─────────────────────────────────
		else if (ev.kind === EK.TrieInsert) {
			addLog("trie", `insert ${ev.src} depth=${ev.vals?.depth ?? "?"}`);
		} else if (ev.kind === EK.TrieDecay) {
			addLog(
				"trie",
				`decay ${ev.src} factor=${(ev.vals?.factor ?? 0).toFixed(4)}`,
			);
		} else if (ev.kind === EK.TriePrune) {
			addLog("trie", `prune ${ev.src} removed=${ev.vals?.removed ?? "?"}`);
		} else if (ev.kind === EK.TriePredict) {
			addLog(
				"trie",
				`predict ${ev.src} score=${(ev.vals?.score ?? 0).toFixed(4)}`,
			);
		} else if (ev.kind === EK.TrieClassify) {
			addLog("trie", `classify ${ev.src} lbl=${ev.lbl}`);
		} else if (ev.kind === EK.TrieExperience) {
			addLog("trie", `experience ${ev.src}`);
		} else if (ev.kind === EK.TrieSignal) {
			const key = ev.src;
			const surprisal = ev.vals?.surprisal ?? 0;
			const entropy = ev.vals?.entropy ?? 0;
			const growth = ev.vals?.growth ?? 0;
			trieSignals.set(key, { surprisal, entropy, growth });
			addLog(
				"trie",
				`signal ${ev.src} S=${surprisal.toFixed(3)} H=${entropy.toFixed(3)}`,
			);
		} else if (ev.kind === EK.TrieCoupling) {
			addLog(
				"trie",
				`coupling ${ev.src}↔${ev.tgt} str=${(ev.vals?.strength ?? 0).toFixed(3)}`,
			);
		} else if (ev.kind === EK.TrieMode) {
			addLog("trie", `mode ${ev.src} m=${ev.vals?.mode ?? "?"}`);
		} else if (ev.kind === EK.TriePressure) {
			addLog(
				"trie",
				`pressure ${ev.src} d=${(ev.vals?.decay ?? 0).toFixed(3)} l=${(ev.vals?.learn ?? 0).toFixed(3)}`,
			);
		}

		// ── Pool ────────────────────────────────────────────────────────────────
		else if (ev.kind === EK.PoolSchedule) {
			poolScheduled++;
			const inflight = ev.vals?.inflight ?? ev.vals?.queue_size ?? 0;
			const workers = ev.vals?.workers ?? 0;
			addLog(
				"pool",
				`schedule ${ev.lbl || ev.src} inf=${inflight} workers=${workers}`,
			);
		} else if (ev.kind === EK.PoolComplete) {
			poolCompleted++;
			const durMs = eventDurationMs(ev);
			const dur = durMs > 0 ? `${durMs.toFixed(3)}ms` : "";
			addLog("pool", `complete ${ev.lbl || ev.src} ${dur}`);
		}

		// ── Adaptive ────────────────────────────────────────────────────────────
		else if (ev.kind === EK.AdaptiveUpdate) {
			const alpha = ev.vals?.alpha ?? ev.vals?.ema_alpha ?? 0;
			adaptiveAlphas.set(ev.src, alpha);
			addLog("adaptive", `α=${alpha.toFixed(5)} ${ev.src}`);
		}

		// ── Beam search ─────────────────────────────────────────────────────────
		else if (ev.kind === EK.BeamCollect) {
			beam.collectCount++;
			const active = ev.vals?.active ?? ev.vals?.continuation_count ?? 0;
			const trieCount = ev.vals?.trie_count ?? 0;
			beam.activeCount = Math.max(beam.activeCount, active);
			addLog(
				"beam",
				`collect ${ev.src} tries=${trieCount} candidates=${active}`,
			);
		} else if (ev.kind === EK.BeamCompose) {
			beam.composeCount++;
			const score = ev.vals?.best_score ?? ev.vals?.score ?? 0;
			beam.bestScore = score;
			beam.converged = false;
			const seq = ev.meta?.sequence || ev.meta?.tokens || "";
			if (seq) beam.lastSequence = seq.substring(0, 40);
			addLog(
				"beam",
				`compose ${ev.src} score=${score.toFixed(4)} ${seq.substring(0, 20)}`,
			);
		} else if (ev.kind === EK.BeamBreak) {
			beam.breakCount++;
			addLog(
				"beam",
				`BREAK ${ev.src} reason=${ev.meta?.reason || ev.lbl || "?"}`,
			);
		} else if (ev.kind === EK.BeamConverge) {
			beam.convergeCount++;
			beam.converged = true;
			const score = ev.vals?.score ?? ev.vals?.best_score ?? 0;
			beam.bestScore = score;
			const seq = ev.meta?.sequence || ev.meta?.tokens || "";
			if (seq) beam.lastSequence = seq.substring(0, 40);
			addLog(
				"beam",
				`CONVERGE ${ev.src} score=${score.toFixed(4)} ${seq.substring(0, 24)}`,
			);
		}

		// ── Compiler → ALU → Finalizer ──────────────────────────────────────────
		else if (ev.kind === EK.CompilerCompile) {
			compilerTotal++;
			compilerLastProg = ev.meta?.program || ev.lbl || "";
			const compileUs = (ev.vals?.compile_ns ?? 0) / 1000;
			addLog(
				"compiler",
				`compile ${ev.lbl || ev.src} op=${ev.vals?.operation ?? "?"} ${compileUs.toFixed(1)}µs`,
			);
		} else if (ev.kind === EK.ALUDispatch) {
			aluTotal++;
			const sub = ev.meta?.substrate || ev.lbl || ev.src || "cpu";
			const durNs = eventDurationNs(ev);
			const existing = aluSubs.get(sub) ?? {
				inflight: 0,
				total: 0,
				lastDurNs: 0,
			};
			existing.total++;
			existing.inflight = Math.max(0, ev.vals?.inflight ?? existing.inflight);
			existing.lastDurNs = durNs;
			aluSubs.set(sub, existing);
			aluRecentOps.unshift(`${sub}:op${ev.vals?.opcode ?? "?"}`);
			if (aluRecentOps.length > 8) aluRecentOps.length = 8;
			const ms = durNs > 0 ? `${(durNs / 1e6).toFixed(3)}ms` : "";
			addLog("alu", `dispatch ${sub} op=${ev.vals?.opcode ?? "?"} ${ms}`);
		} else if (ev.kind === EK.FinalizerRun) {
			finalizerTotal++;
			const durNs = ev.vals?.duration_ns ?? 0;
			const ms = durNs > 0 ? `${(durNs / 1e6).toFixed(3)}ms` : "";
			addLog("compiler", `finalizer #${finalizerTotal} ${ms} ${ev.src}`);
		}

		// ── Prompt ──────────────────────────────────────────────────────────────
		else if (ev.kind === EK.Prompt) {
			const promptText = ev.meta?.prompt || "";

			for (const [pid, pv] of valueMap) {
				if (pv.role === "prompt") valueMap.delete(pid);
			}

			const vid = `prompt_${ev.ts}_${fnv1a32(promptText).toString(16)}`;
			// Fixed world position: centre-top so the prompt is always reachable and
			// does not drift with auto-fit. Communities cluster near y=0 so −250 keeps
			// the diamond clearly above them.
			const pos = { x: 0, y: -250 };
			const tel = snapshotTelemetry(ev);

			valueMap.set(vid, {
				id: vid,
				pos,
				vel: { x: 0, y: 0 },
				role: "prompt",
				program: "",
				communityId: -1,
				label: promptText.substring(0, 40),
				content: promptText,
				resonance: 1,
				gap: 1,
				resolved: false,
				age: 0,
				prevId: "",
				nextId: "",
				telemetry: tel,
				actionResonance: 0,
				memberIndex: 0,
			});
			cullOldest();
			addLog("prompt", promptText.substring(0, 50));
		} else if (ev.kind === EK.PromptResult) {
			const gen = ev.meta?.generation || "";
			addLog("prompt_result", gen.substring(0, 56));
		}

		// ── Ingest pipeline ─────────────────────────────────────────────────────
		else if (ev.kind === EK.TokenizerEmit) {
			const vid = ev.meta?.value_id || `v_${ev.ts}`;
			const tokenContent = ev.meta?.content || "";
			const pos = spawnPos(`emit|${vid}`);
			const vel = layoutVelocity(`tok|${vid}`);
			const tel = snapshotTelemetry(ev);

			valueMap.set(vid, {
				id: vid,
				pos,
				vel,
				role: "data",
				program: ev.meta?.program || "affinity",
				communityId: -1,
				label: ev.lbl || "",
				content: tokenContent,
				resonance: 0.5,
				gap: 1,
				resolved: false,
				age: 0,
				prevId: "",
				nextId: "",
				telemetry: tel,
				actionResonance: 0,
				memberIndex: 0,
			});
			cullOldest();
			addLog(
				"tokenizer",
				`${(tokenContent || vid).substring(0, 30)}${ev.lbl ? " [" + ev.lbl + "]" : ""}`,
			);
		} else if (ev.kind === EK.TokenizerChunk) {
			const bw = ev.vals?.bytes_written ?? 0;
			addLog("tokenizer_chunk", `+${bw} B`);
		} else if (ev.kind === EK.DatasetRead) {
			const ds = ev.meta?.dataset || "";
			const br = ev.vals?.bytes_read ?? 0;
			addLog("dataset", `${ds} +${br} B`);
		} else if (ev.kind === EK.QueueSubmit) {
			const vid = normalizedId(ev.meta?.value_id);
			const v = ensureValue(vid, ev, "data", -1);

			if (v) {
				v.prevId = normalizedId(ev.meta?.prev_id);
				v.nextId = normalizedId(ev.meta?.next_id);
				v.telemetry = mergeTelemetry(v.telemetry, ev);

				if (ev.meta?.program) {
					v.program = ev.meta.program;
				} else if (v.role === "data" && !v.program) {
					v.program = "affinity";
				}
			}

			const inf = ev.vals?.inflight ?? -1;
			addLog("queue", `submit inf=${inf} ${(ev.lbl || "").substring(0, 40)}`);
		} else if (ev.kind === EK.HolographicCrossover) {
			addLog("field", `holographic crossover ${ev.lbl || ev.src}`);
		} else if (ev.kind === EK.Sense) {
			addLog("field", `sense ${ev.src} ${ev.lbl}`);
		}

		// ── Orchestrator / Community ────────────────────────────────────────────
		else if (ev.kind === EK.CommunityCreated) {
			const cid = ev.vals?.community_id ?? -1;
			const affinityHex = ev.meta?.initial_affinity || "";
			const community = ensureCommunity(cid);
			if (community)
				community.affinityHex = affinityHex || community.affinityHex;
			addLog(
				"community",
				`created #${cid}${affinityHex ? " aff=" + affinityHex.substring(0, 16) + ".." : ""}`,
			);
		} else if (ev.kind === EK.ValueJoinedCommunity) {
			const vid = normalizedId(ev.meta?.value_id);
			const cid = ev.vals?.community_id ?? -1;
			const distance = ev.vals?.distance ?? -1;
			const c = ensureCommunity(cid);

			if (vid && c) {
				const v = ensureValue(vid, ev, "data", cid);
				const isNewMember = !c.memberIds.has(vid);
				c.memberIds.add(vid);

				if (v) {
					if (v.communityId >= 0 && v.communityId !== cid) {
						communityMap.get(v.communityId)?.memberIds.delete(vid);
					}

					v.communityId = cid;
					v.resonance = 1;
					if (isNewMember) v.memberIndex = c.memberIds.size - 1;
					v.telemetry = mergeTelemetry(v.telemetry, ev);

					const off = spiralOffset(v.memberIndex);
					v.pos.x = c.center.x + off.dx;
					v.pos.y = c.center.y + off.dy;
					v.vel.x = 0;
					v.vel.y = 0;
				}
			}

			addLog(
				"join",
				`${vid.substring(0, 8)} → #${cid} dist=${distance.toFixed(0)}`,
			);
		} else if (ev.kind === EK.CommunitySaturated) {
			const cid = ev.vals?.community_id ?? -1;
			const sat = ev.vals?.saturation ?? 0;
			const c = communityMap.get(cid);
			if (c) {
				c.saturated = true;
				c.saturation = sat;
			}
			addLog("saturated", `#${cid} at ${(sat * 100).toFixed(1)}%`);
		} else if (ev.kind === EK.CommunityAction) {
			const prog = ev.lbl || "unknown";
			const cid = ev.vals?.community_id ?? -1;
			const c = ensureCommunity(cid);
			if (c) {
				c.lastAction = prog;
				c.actionCount++;
			}
			stats.actions++;

			const vid =
				normalizedId(ev.meta?.value_id) ||
				normalizedId(ev.meta?.action_id) ||
				`a_${ev.ts}`;
			const pos = spawnPos(`action|${vid}`, cid);
			const vel = layoutVelocity(`act|${vid}`);
			const tel = snapshotTelemetry(ev);
			const ar = ev.vals?.resonance ?? 0;

			valueMap.set(vid, {
				id: vid,
				pos,
				vel,
				role: "action",
				program: prog,
				communityId: cid,
				label: prog,
				content: "",
				resonance: 1,
				gap: 0,
				resolved: false,
				age: 0,
				prevId: "",
				nextId: "",
				telemetry: tel,
				actionResonance: ar,
				memberIndex: 0,
			});
			cullOldest();
			addLog("action", `#${cid} → ${prog}`);
		} else if (ev.kind === EK.CommunityReaction) {
			const prog = ev.lbl || "unknown";
			const cid = ev.vals?.community_id ?? -1;
			const c = ensureCommunity(cid);
			if (c) c.reactionCount++;
			stats.reactions++;

			const vid =
				normalizedId(ev.meta?.value_id) ||
				normalizedId(ev.meta?.reaction_id) ||
				`r_${ev.ts}`;
			const pos = spawnPos(`reaction|${vid}`, cid);
			const vel = layoutVelocity(`rx|${vid}`);
			const tel = snapshotTelemetry(ev);

			valueMap.set(vid, {
				id: vid,
				pos,
				vel,
				role: "reaction",
				program: prog,
				communityId: cid,
				label: prog,
				content: "",
				resonance: 0.8,
				gap: 0,
				resolved: false,
				age: 0,
				prevId: "",
				nextId: "",
				telemetry: tel,
				actionResonance: 0,
				memberIndex: 0,
			});
			cullOldest();
			addLog("reaction", `#${cid} → ${prog}`);
		} else if (ev.kind === EK.CausalHubProbe) {
			const depth = ev.vals?.depth ?? 0;
			const status = ev.meta?.status || "unknown";
			addLog("causal_hub", `depth=${depth} ${status}`);
		}

		// ── Belief gap ──────────────────────────────────────────────────────────
		else if (ev.kind === EK.BeliefGapEvaluated) {
			const vid = normalizedId(ev.meta?.value_id);
			const gap = ev.vals?.gap ?? 1;
			const cid = ev.vals?.community_id ?? -1;
			const v = ensureValue(vid, ev, "data", cid);

			if (v) {
				v.gap = gap;
				v.resonance = 1 - gap;
				v.telemetry = mergeTelemetry(v.telemetry, ev);
			}

			addLog("gap", `${vid.substring(0, 8)} gap=${gap.toFixed(4)}`);
		} else if (ev.kind === EK.ValueResolved) {
			const vid = normalizedId(ev.meta?.value_id);
			const gap = ev.vals?.gap ?? 0;
			const cid = ev.vals?.community_id ?? -1;
			const v = ensureValue(vid, ev, "data", cid);

			if (v) {
				v.resolved = true;
				v.gap = gap;
				v.resonance = 1;
				v.telemetry = mergeTelemetry(v.telemetry, ev);
			}

			addLog("resolved", `${vid.substring(0, 8)} gap=${gap.toFixed(4)}`);
		} else if (ev.kind === EK.CommunityEmission) {
			const cid = ev.vals?.community_id ?? -1;
			const memberCount = ev.vals?.member_count ?? 0;
			const concentration = ev.vals?.concentration ?? 0;
			const c = communityMap.get(cid);
			if (c) {
				c.concentration = concentration;

				for (const memberId of c.memberIds) {
					const member = valueMap.get(memberId);
					if (member && member.role === "data") member.communityId = -1;
				}

				c.memberIds.clear();
			}
			addLog(
				"emission",
				`#${cid} members=${memberCount} conc=${concentration.toFixed(3)}`,
			);
		} else if (ev.kind === EK.TrieGraphSnapshot) {
			const tidx = ev.vals?.trie_idx ?? -1;
			const gj = ev.meta?.graph || "";
			addLog("trie_graph", `trie ${tidx} json ${gj.length} B`);
		}

		// ── Catch-all for any future kinds ──────────────────────────────────────
		else {
			addLog(kindName.toLowerCase(), ev.lbl || ev.src || "");
		}
	}

	function connect() {
		const meta = import.meta as ImportMeta & { env?: Record<string, string> };
		const host = meta.env?.VITE_VIZ_HOST || "localhost";
		const port = meta.env?.VITE_VIZ_PORT || "6600";
		ws = new WebSocket(`ws://${host}:${port}/ws`);
		ws.binaryType = "arraybuffer";

		ws.onopen = () => addLog("system", "connected");

		ws.onmessage = (msg) => {
			if (isDestroyed) return;
			if (!(msg.data instanceof ArrayBuffer)) return;

			let buf: Uint8Array | undefined;

			try {
				buf = new Uint8Array(msg.data);

				for (const frame of decodeVizFrames(buf)) {
					if (frame.frameType === "event") {
						applyEvent(frame.event);
						continue;
					}

					if (frame.frameType === "bootstrap") {
						bootstrapNodeIds = frame.nodes;
						stats.bootstrapNodes = frame.nodes.length;
						addLog("system", `bootstrap ${frame.nodes.length} nodes`);
						continue;
					}

					if (frame.frameType === "stats") {
						stats.dropped = frame.dropped;

						if (frame.dropped > 0) {
							addLog("viz_bus", `dropped ${frame.dropped} (bus)`);
						}

						continue;
					}

					if (frame.frameType === "scrub") {
						for (const ev of frame.events) applyEvent(ev);
						continue;
					}

					if (frame.frameType === "value") {
						applyValueWireFrame(frame.valueId, frame.bytes);
						continue;
					}

					if (frame.frameType === "json") {
						stats.wireJsonBlobs++;
						let summary = `len=${frame.text.length}`;

						try {
							const j = JSON.parse(frame.text) as Record<string, unknown>;
							if (typeof j.snapshot_kind === "string")
								summary = `snap:${j.snapshot_kind}`;
						} catch {
							/* partial JSON */
						}

						addLog("json", summary);
					}
				}
			} catch (err) {
				const msgText = err instanceof Error ? err.message : String(err);
				const preview =
					buf && buf.length > 0
						? Array.from(buf.subarray(0, Math.min(24, buf.length)))
								.map((b) => b.toString(16).padStart(2, "0"))
								.join(" ")
						: "";

				addLog(
					"error",
					`malformed frame: ${msgText}${preview ? ` bytes=${preview}` : ""}`,
				);
			}
		};

		ws.onclose = () => {
			if (!isDestroyed) setTimeout(connect, 2000);
		};
	}

	function updateLayout() {
		for (const [, v] of valueMap) {
			v.age++;

			if (v.communityId >= 0) {
				const c = communityMap.get(v.communityId);

				if (c) {
					const off = spiralOffset(v.memberIndex);
					const targetX = c.center.x + off.dx;
					const targetY = c.center.y + off.dy;

					v.vel.x += (targetX - v.pos.x) * 0.12;
					v.vel.y += (targetY - v.pos.y) * 0.12;
				}
			}

			v.vel.x *= 0.88;
			v.vel.y *= 0.88;
			v.pos.x += v.vel.x;
			v.pos.y += v.vel.y;

			const speed = Math.sqrt(v.vel.x * v.vel.x + v.vel.y * v.vel.y);
			if (speed > 3) {
				v.vel.x = (v.vel.x / speed) * 3;
				v.vel.y = (v.vel.y / speed) * 3;
			}
		}

		const activeCommunities: VisCommunity[] = [];

		for (const [, c] of communityMap) {
			if (c.memberIds.size === 0) continue;
			activeCommunities.push(c);
		}

		const minSep = 220;

		for (let i = 0; i < activeCommunities.length; i++) {
			const a = activeCommunities[i];

			for (let j = i + 1; j < activeCommunities.length; j++) {
				const b = activeCommunities[j];
				const dx = b.center.x - a.center.x;
				const dy = b.center.y - a.center.y;
				const dist = Math.sqrt(dx * dx + dy * dy) || 1;

				if (dist >= minSep) continue;

				const push = (minSep - dist) * 0.04;
				const nx = (dx / dist) * push;
				const ny = (dy / dist) * push;

				a.center.x -= nx;
				a.center.y -= ny;
				b.center.x += nx;
				b.center.y += ny;
			}
		}
	}

	function autoFitCamera() {
		if (!autoFitEnabled || valueMap.size === 0) return;

		let minX = Infinity,
			minY = Infinity,
			maxX = -Infinity,
			maxY = -Infinity;

		for (const [, v] of valueMap) {
			if (v.pos.x < minX) minX = v.pos.x;
			if (v.pos.y < minY) minY = v.pos.y;
			if (v.pos.x > maxX) maxX = v.pos.x;
			if (v.pos.y > maxY) maxY = v.pos.y;
		}

		const bw = maxX - minX;
		const bh = maxY - minY;
		const cx = minX + bw / 2;
		const cy = minY + bh / 2;
		const pad = 1.15;
		const targetZoom = Math.min(
			W / (Math.max(bw, 100) * pad),
			H / (Math.max(bh, 100) * pad),
		);

		camX += (cx - camX) * 0.08;
		camY += (cy - camY) * 0.08;
		camZoom += (targetZoom - camZoom) * 0.08;
	}

	function beginWorldDraw() {
		ctx.save();
		ctx.translate(W / 2, H / 2);
		ctx.scale(camZoom, camZoom);
		ctx.translate(-camX, -camY);
	}

	function endWorldDraw() {
		ctx.restore();
	}

	function drawEdges() {
		ctx.strokeStyle = "rgba(255,255,255,0.06)";
		ctx.lineWidth = 0.5 / camZoom;
		ctx.beginPath();

		for (const [, v] of valueMap) {
			if (!v.nextId) continue;
			const other = valueMap.get(v.nextId);
			if (!other) continue;
			ctx.moveTo(v.pos.x, v.pos.y);
			ctx.lineTo(other.pos.x, other.pos.y);
		}

		ctx.stroke();
	}

	function drawCommunities() {
		for (const [, c] of communityMap) {
			if (c.memberIds.size < 2) continue;

			// Use affinity hex for stable identity color, fall back to last action color.
			const col = c.affinityHex
				? affinityColor(c.affinityHex)
				: c.lastAction
					? programColor(c.lastAction)
					: ([186, 104, 200] as [number, number, number]);
			const [cr, cg, cb] = col;

			let maxDist = 0;
			for (const vid of c.memberIds) {
				const v = valueMap.get(vid);
				if (v) {
					const d = Math.hypot(v.pos.x - c.center.x, v.pos.y - c.center.y);
					if (d > maxDist) maxDist = d;
				}
			}
			const radius =
				Math.max(60, maxDist + 30) + Math.sin(frameCount * 0.06) * 4;
			const conc = c.concentration;

			ctx.fillStyle = `rgba(${cr},${cg},${cb},${0.02 + conc * 0.06})`;
			ctx.beginPath();
			ctx.arc(c.center.x, c.center.y, radius * 1.4, 0, Math.PI * 2);
			ctx.fill();

			ctx.fillStyle = `rgba(${cr},${cg},${cb},${0.03 + conc * 0.08})`;
			ctx.beginPath();
			ctx.arc(c.center.x, c.center.y, radius, 0, Math.PI * 2);
			ctx.fill();

			ctx.strokeStyle = `rgba(${cr},${cg},${cb},${c.saturated ? 0.5 : 0.25})`;
			ctx.lineWidth = (c.saturated ? 2 : 1) / camZoom;
			ctx.beginPath();
			ctx.arc(c.center.x, c.center.y, radius, 0, Math.PI * 2);
			ctx.stroke();

			ctx.strokeStyle = `rgba(${cr},${cg},${cb},0.08)`;
			ctx.lineWidth = 0.5 / camZoom;
			ctx.beginPath();
			for (const vid of c.memberIds) {
				const v = valueMap.get(vid);
				if (v) {
					ctx.moveTo(c.center.x, c.center.y);
					ctx.lineTo(v.pos.x, v.pos.y);
				}
			}
			ctx.stroke();

			if (c.memberIds.size >= 2) {
				const fontSize = Math.max(8, 9 / camZoom);
				ctx.fillStyle = `rgba(${cr},${cg},${cb},0.55)`;
				ctx.font = `${fontSize}px monospace`;
				ctx.textAlign = "center";
				ctx.fillText(
					`#${c.id} [${c.memberIds.size}]${c.lastAction ? " " + c.lastAction : ""}`,
					c.center.x,
					c.center.y + radius + fontSize + 4,
				);

				if (c.actionCount > 0 || c.reactionCount > 0) {
					ctx.fillStyle = `rgba(${cr},${cg},${cb},0.4)`;
					ctx.fillText(
						`a:${c.actionCount} r:${c.reactionCount} conc:${conc.toFixed(2)}`,
						c.center.x,
						c.center.y + radius + fontSize * 2 + 8,
					);
				}

				if (c.saturated) {
					ctx.fillStyle = "rgba(255,80,80,0.5)";
					ctx.fillText(
						`SATURATED ${(c.saturation * 100).toFixed(0)}%`,
						c.center.x,
						c.center.y + radius + fontSize * 3 + 12,
					);
				}
			}
		}
	}

	function valueColor(v: VisValue): [number, number, number] {
		if (v.role === "prompt") return [255, 180, 0];
		if (v.resolved) return [0, 255, 120];
		if (v.program) return programColor(v.program);

		if (v.gap < 1) {
			const closeness = 1 - v.gap;
			const r = Math.round(100 + closeness * 155);
			const g = Math.round(180 - closeness * 80);
			return [r, g, 255];
		}

		return [100, 180, 255];
	}

	function drawValues() {
		const invZoom = 1 / camZoom;

		for (const [vid, v] of valueMap) {
			if (v.role === "prompt") continue;

			const [r, g, b] = valueColor(v);
			const a = 0.47 + v.resonance * 0.53;

			ctx.save();
			ctx.translate(v.pos.x, v.pos.y);

			if (v.resonance > 0.15) {
				ctx.fillStyle = `rgba(${r},${g},${b},${v.resonance * 0.1})`;
				ctx.beginPath();
				ctx.arc(0, 0, 12 + v.resonance * 14, 0, Math.PI * 2);
				ctx.fill();
			}

			ctx.strokeStyle = `rgba(${r},${g},${b},${a})`;
			ctx.lineWidth = invZoom;

			if (v.role === "action") {
				ctx.beginPath();
				ctx.moveTo(0, -6);
				ctx.lineTo(6, 6);
				ctx.lineTo(-6, 6);
				ctx.closePath();
				ctx.stroke();
			} else if (v.role === "reaction") {
				ctx.beginPath();
				ctx.moveTo(0, 6);
				ctx.lineTo(6, -5);
				ctx.lineTo(-6, -5);
				ctx.closePath();
				ctx.stroke();
			} else {
				ctx.strokeRect(-5, -5, 10, 10);
			}

			if (v.program) {
				const labelSize = Math.max(6, 8 / camZoom);
				ctx.fillStyle = `rgba(${r},${g},${b},${a * 0.7})`;
				ctx.font = `${labelSize}px monospace`;
				ctx.textAlign = "left";
				ctx.fillText(v.program, 10, 3);
			} else if (v.content && v.role === "data" && camZoom > 0.3) {
				const labelSize = Math.max(5, 7 / camZoom);
				ctx.fillStyle = `rgba(${r},${g},${b},${a * 0.4})`;
				ctx.font = `${labelSize}px monospace`;
				ctx.textAlign = "left";
				ctx.fillText(v.content.substring(0, 20), 10, 3);
			}

			if (v.resolved) {
				ctx.strokeStyle = "rgba(0,255,120,0.6)";
				ctx.lineWidth = 2 * invZoom;
				ctx.beginPath();
				ctx.arc(0, 0, 12, 0, Math.PI * 2);
				ctx.stroke();
			}

			if (selectedId === vid) {
				ctx.strokeStyle = "rgba(255,255,255,0.8)";
				ctx.lineWidth = 2 * invZoom;
				ctx.beginPath();
				ctx.arc(0, 0, 16, 0, Math.PI * 2);
				ctx.stroke();
			}

			ctx.restore();
		}
	}

	function drawPrompts() {
		const invZoom = 1 / camZoom;

		for (const [vid, v] of valueMap) {
			if (v.role !== "prompt") continue;

			const pulse = 0.6 + Math.sin(frameCount * 0.12) * 0.4;

			ctx.save();
			ctx.translate(v.pos.x, v.pos.y);

			ctx.fillStyle = `rgba(255,180,0,${0.06 * pulse})`;
			ctx.beginPath();
			ctx.arc(0, 0, 40 + pulse * 15, 0, Math.PI * 2);
			ctx.fill();

			ctx.fillStyle = `rgba(255,140,0,${0.1 * pulse})`;
			ctx.beginPath();
			ctx.arc(0, 0, 24 + pulse * 8, 0, Math.PI * 2);
			ctx.fill();

			ctx.strokeStyle = `rgba(255,200,0,${0.7 + pulse * 0.3})`;
			ctx.lineWidth = 2 * invZoom;
			ctx.beginPath();
			ctx.moveTo(0, -12);
			ctx.lineTo(12, 0);
			ctx.lineTo(0, 12);
			ctx.lineTo(-12, 0);
			ctx.closePath();
			ctx.stroke();

			ctx.strokeStyle = `rgba(255,160,0,${0.4 + pulse * 0.2})`;
			ctx.lineWidth = invZoom;
			ctx.beginPath();
			ctx.moveTo(0, -18);
			ctx.lineTo(18, 0);
			ctx.lineTo(0, 18);
			ctx.lineTo(-18, 0);
			ctx.closePath();
			ctx.stroke();

			const labelSize = Math.max(8, 10 / camZoom);
			ctx.fillStyle = "rgba(255,200,0,0.85)";
			ctx.font = `bold ${labelSize}px monospace`;
			ctx.textAlign = "left";
			ctx.fillText(`PROMPT: ${v.label}`, 24, 4);

			if (selectedId === vid) {
				ctx.strokeStyle = "rgba(255,255,255,0.9)";
				ctx.lineWidth = 2 * invZoom;
				ctx.beginPath();
				ctx.arc(0, 0, 24, 0, Math.PI * 2);
				ctx.stroke();
			}

			ctx.restore();
		}
	}

	function drawHUD() {
		ctx.fillStyle = "rgba(255,255,255,0.7)";
		ctx.font = "11px monospace";
		ctx.textAlign = "left";
		let y = 20;
		const x = 16;

		const allVals = [...valueMap.values()];
		const prompts = allVals.filter((v) => v.role === "prompt").length;
		const data = allVals.filter((v) => v.role === "data").length;
		const resolvedVals = allVals.filter((v) => v.resolved).length;
		const actionVals = allVals.filter((v) => v.role === "action").length;
		const reactionVals = allVals.filter((v) => v.role === "reaction").length;
		const liveCommunities = [...communityMap.values()].filter(
			(c) => c.memberIds.size >= 2,
		).length;
		const totalCommunities = [...communityMap.values()].filter(
			(c) => c.memberIds.size > 0,
		).length;
		const saturated = [...communityMap.values()].filter(
			(c) => c.saturated,
		).length;

		ctx.fillText(`values: ${valueMap.size}`, x, y);
		y += 16;

		ctx.fillStyle = "rgba(255,180,0,0.9)";
		ctx.fillText(`  prompts: ${prompts}`, x, y);
		y += 14;

		ctx.fillStyle = "rgba(100,180,255,0.7)";
		ctx.fillText(`  data: ${data}  resolved: ${resolvedVals}`, x, y);
		y += 14;

		ctx.fillStyle = "rgba(0,255,150,0.7)";
		ctx.fillText(`  actions: ${actionVals}  (total: ${stats.actions})`, x, y);
		y += 14;

		ctx.fillStyle = "rgba(255,150,50,0.7)";
		ctx.fillText(
			`  reactions: ${reactionVals}  (total: ${stats.reactions})`,
			x,
			y,
		);
		y += 18;

		ctx.fillStyle = "rgba(186,104,200,0.7)";
		ctx.fillText(
			`communities: ${liveCommunities} active  (${totalCommunities} total, ${saturated} sat)`,
			x,
			y,
		);
		y += 18;

		ctx.fillStyle =
			stats.dropped > 0 ? "rgba(255,100,100,0.9)" : "rgba(255,255,255,0.35)";
		ctx.fillText(`bus dropped: ${stats.dropped}`, x, y);
		y += 14;

		ctx.fillStyle = "rgba(255,255,255,0.35)";
		ctx.fillText(
			`bootstrap: ${bootstrapNodeIds.length} peers   json: ${stats.wireJsonBlobs}`,
			x,
			y,
		);
		y += 14;

		ctx.fillStyle = "rgba(255,255,255,0.25)";
		ctx.fillText(
			`zoom: ${camZoom.toFixed(2)}x${autoFitEnabled ? "  (auto)" : ""}`,
			x,
			y,
		);
		y += 20;

		ctx.fillStyle = "rgba(255,255,255,0.3)";
		ctx.font = "9px monospace";
		ctx.fillText(
			"drag: pan  |  scroll: zoom  |  dblclick: re-fit  |  click: inspect",
			x,
			y,
		);
		y += 16;

		ctx.fillStyle = "rgba(255,180,0,0.7)";
		ctx.beginPath();
		ctx.moveTo(x, y - 2);
		ctx.lineTo(x + 5, y + 4);
		ctx.lineTo(x, y + 10);
		ctx.lineTo(x - 5, y + 4);
		ctx.closePath();
		ctx.stroke();
		ctx.fillStyle = "rgba(255,255,255,0.48)";
		ctx.fillText("prompt", x + 12, y + 6);
		y += 16;

		ctx.fillStyle = "rgba(100,180,255,0.6)";
		ctx.fillRect(x - 4, y, 8, 8);
		ctx.fillStyle = "rgba(255,255,255,0.48)";
		ctx.fillText("data", x + 12, y + 7);
		y += 14;

		ctx.fillStyle = "rgba(255,255,255,0.48)";
		ctx.fillText("ACTIONS", x, y);
		y += 14;

		for (const action of ACTIONS) {
			const [ar, ag, ab] = action.color;
			ctx.fillStyle = `rgba(${ar},${ag},${ab},0.63)`;
			ctx.beginPath();
			ctx.moveTo(x, y - 4);
			ctx.lineTo(x + 5, y + 4);
			ctx.lineTo(x - 5, y + 4);
			ctx.closePath();
			ctx.fill();
			ctx.fillStyle = "rgba(255,255,255,0.48)";
			ctx.fillText(action.name, x + 12, y + 3);
			y += 14;
		}

		y += 4;
		ctx.fillStyle = "rgba(255,255,255,0.48)";
		ctx.fillText("REACTIONS", x, y);
		y += 14;

		for (const reaction of REACTIONS) {
			const [rr, rg, rb] = reaction.color;
			ctx.fillStyle = `rgba(${rr},${rg},${rb},0.63)`;
			ctx.beginPath();
			ctx.moveTo(x, y + 4);
			ctx.lineTo(x + 5, y - 4);
			ctx.lineTo(x - 5, y - 4);
			ctx.closePath();
			ctx.fill();
			ctx.fillStyle = "rgba(255,255,255,0.48)";
			ctx.fillText(reaction.name, x + 12, y + 3);
			y += 14;
		}
	}

	/*
  drawSubsystemPanel renders a compact status panel on the bottom-left of the
  canvas showing ALU substrate load, beam search state, pool throughput, and
  field digest metrics. These are derived directly from the backend event kinds
  that do not correspond to visible value particles.
  */
	function drawSubsystemPanel() {
		const px = 16;
		const pw = 240;
		let py = H - 16;

		ctx.font = "9px monospace";
		ctx.textAlign = "left";

		// ── Adaptive alphas ─────────────────────────────────────────────────────
		if (adaptiveAlphas.size > 0) {
			const entries = [...adaptiveAlphas.entries()].slice(0, 3);
			for (let i = entries.length - 1; i >= 0; i--) {
				const [src, alpha] = entries[i];
				ctx.fillStyle = "rgba(160,255,200,0.45)";
				ctx.fillText(`α ${src.substring(0, 16)}: ${alpha.toFixed(5)}`, px, py);
				py -= 12;
			}
			ctx.fillStyle = "rgba(160,255,200,0.25)";
			ctx.fillText("ADAPTIVE", px, py);
			py -= 16;
		}

		// ── Field digest ────────────────────────────────────────────────────────
		if (fieldDigests.size > 0) {
			const dig = [...fieldDigests.values()][fieldDigests.size - 1];
			ctx.fillStyle = "rgba(140,180,255,0.45)";
			ctx.fillText(
				`S=${dig.surprisal.toFixed(3)}  H=${dig.entropy.toFixed(3)}  g=${dig.growth.toFixed(3)}`,
				px,
				py,
			);
			py -= 12;
			ctx.fillStyle = "rgba(140,180,255,0.25)";
			ctx.fillText(`FIELD DIGEST (${dig.nodeId.substring(0, 12)})`, px, py);
			py -= 16;
		}

		// ── Pool ────────────────────────────────────────────────────────────────
		if (poolScheduled > 0) {
			ctx.fillStyle = "rgba(160,200,160,0.45)";
			ctx.fillText(`sched:${poolScheduled}  done:${poolCompleted}`, px, py);
			py -= 12;
			ctx.fillStyle = "rgba(160,200,160,0.25)";
			ctx.fillText("POOL", px, py);
			py -= 16;
		}

		// ── Beam ────────────────────────────────────────────────────────────────
		if (beam.collectCount > 0 || beam.composeCount > 0) {
			ctx.fillStyle = beam.converged
				? "rgba(0,255,150,0.6)"
				: "rgba(80,200,255,0.5)";
			ctx.fillText(
				`score:${beam.bestScore.toFixed(3)}  break:${beam.breakCount}  converge:${beam.convergeCount}`,
				px,
				py,
			);
			py -= 12;
			if (beam.lastSequence) {
				ctx.fillStyle = "rgba(80,200,255,0.35)";
				ctx.fillText(`"${beam.lastSequence.substring(0, 30)}"`, px, py);
				py -= 12;
			}
			ctx.fillStyle = "rgba(80,200,255,0.25)";
			ctx.fillText(
				`BEAM  collect:${beam.collectCount}  compose:${beam.composeCount}`,
				px,
				py,
			);
			py -= 16;
		}

		// ── ALU substrates ──────────────────────────────────────────────────────
		if (aluSubs.size > 0) {
			for (const [sub, state] of aluSubs) {
				const barW = Math.min(pw - 60, state.total);
				const barFill = Math.min(
					pw - 60,
					Math.round((state.inflight / Math.max(1, state.total)) * (pw - 60)),
				);
				ctx.fillStyle = "rgba(255,140,60,0.15)";
				ctx.fillRect(
					px + 60,
					py - 8,
					barW > 0 ? Math.min(barW, pw - 60) : pw - 60,
					8,
				);
				ctx.fillStyle = "rgba(255,140,60,0.5)";
				ctx.fillRect(px + 60, py - 8, barFill, 8);
				ctx.fillStyle = "rgba(255,140,60,0.55)";
				const ms =
					state.lastDurNs > 0 ? `${(state.lastDurNs / 1e6).toFixed(2)}ms` : "";
				ctx.fillText(`${sub.substring(0, 6)} ×${state.total} ${ms}`, px, py);
				py -= 14;
			}
			ctx.fillStyle = "rgba(255,140,60,0.25)";
			ctx.fillText(`ALU  total:${aluTotal}  compiler:${compilerTotal}`, px, py);
			py -= 16;
		}

		// ── Gossip ──────────────────────────────────────────────────────────────
		const totalSent = [...gossipSent.values()].reduce((a, b) => a + b, 0);
		const totalRecv = [...gossipRecv.values()].reduce((a, b) => a + b, 0);
		if (totalSent > 0 || totalRecv > 0) {
			ctx.fillStyle = "rgba(200,220,120,0.45)";
			ctx.fillText(
				`sent:${totalSent}  recv:${totalRecv}  nodes:${gossipSent.size}`,
				px,
				py,
			);
			py -= 12;
			ctx.fillStyle = "rgba(200,220,120,0.25)";
			ctx.fillText("GOSSIP", px, py);
		}
	}

	function drawEventLog() {
		if (eventLog.length === 0) return;

		const pw = 320;
		const px = W - pw - 10;
		const visibleCount = Math.min(eventLog.length, 35);
		const py = 20;

		ctx.fillStyle = "rgba(0,0,0,0.4)";
		ctx.fillRect(px - 6, py - 16, pw + 12, 18 + visibleCount * 12 + 4);

		ctx.fillStyle = "rgba(255,255,255,0.3)";
		ctx.font = "9px monospace";
		ctx.textAlign = "left";
		ctx.fillText("EVENT LOG", px, py);

		for (let i = 0; i < visibleCount; i++) {
			const entry = eventLog[i];
			const age = ((Date.now() - entry.ts) / 1000).toFixed(1);
			ctx.fillStyle = entry.color;
			ctx.fillText(
				`${age.padStart(5)}s  ${entry.kind.padEnd(12).substring(0, 12)}  ${entry.detail.substring(0, 36)}`,
				px,
				py + 16 + i * 12,
			);
		}
	}

	function drawWaiting() {
		if (valueMap.size > 0) return;

		ctx.fillStyle = "rgba(255,255,255,0.15)";
		ctx.font = "14px monospace";
		ctx.textAlign = "center";
		ctx.fillText("waiting for events…", W / 2, H / 2);

		ctx.fillStyle = "rgba(255,255,255,0.08)";
		ctx.font = "11px monospace";

		const meta = import.meta as ImportMeta & { env?: Record<string, string> };
		const host = meta.env?.VITE_VIZ_HOST || "localhost";
		const port = meta.env?.VITE_VIZ_PORT || "6600";
		ctx.fillText(`ws://${host}:${port}/ws`, W / 2, H / 2 + 20);
	}

	function draw() {
		if (isDestroyed) return;
		requestAnimationFrame(draw);
		frameCount++;

		ctx.fillStyle = "rgba(5,5,16,0.25)";
		ctx.fillRect(0, 0, W, H);

		updateLayout();
		autoFitCamera();

		beginWorldDraw();
		drawEdges();
		drawCommunities();
		drawValues();
		drawPrompts();
		endWorldDraw();

		drawHUD();
		drawSubsystemPanel();
		drawEventLog();
		drawWaiting();

		if (selectedId && frameCount % 15 === 0) emitSelection();

		if (callbacks.onGraphSnapshot && frameCount % 60 === 0) {
			emitGraphSnapshot();
		}

		stats.values = valueMap.size;
		stats.communities = communityMap.size;
		stats.bootstrapNodes = bootstrapNodeIds.length;
		callbacks.onStats(stats);
	}

	function emitGraphSnapshot() {
		const fields: FieldSnapshot[] = [];
		const orphanValues: FieldValueSnapshot[] = [];
		const emittedValueIds = new Set<string>();

		for (const [, community] of communityMap) {
			if (
				community.memberIds.size === 0 &&
				community.actionCount === 0 &&
				community.reactionCount === 0 &&
				community.concentration === 0
			)
				continue;

			const members: FieldValueSnapshot[] = [];
			for (const vid of community.memberIds) {
				const v = valueMap.get(vid);
				if (!v) continue;
				emittedValueIds.add(v.id);
				members.push({
					id: v.id,
					role: v.role,
					program: v.program,
					communityId: v.communityId,
					label: v.label,
					content: v.content,
					resonance: v.resonance,
					gap: v.gap,
					resolved: v.resolved,
					actionResonance: v.actionResonance,
					prevId: v.prevId,
					nextId: v.nextId,
					communityAffinityHex: community.affinityHex,
					wireFrame: v.wireFrame ?? null,
					telemetry: v.telemetry,
				});
			}

			fields.push({
				id: community.id,
				memberCount: community.memberIds.size,
				saturated: community.saturated,
				saturation: community.saturation,
				lastAction: community.lastAction,
				actionCount: community.actionCount,
				reactionCount: community.reactionCount,
				affinityHex: community.affinityHex,
				concentration: community.concentration,
				members,
			});
		}

		for (const [, v] of valueMap) {
			if (emittedValueIds.has(v.id)) continue;

			orphanValues.push({
				id: v.id,
				role: v.role,
				program: v.program,
				communityId: v.communityId,
				label: v.label,
				content: v.content,
				resonance: v.resonance,
				gap: v.gap,
				resolved: v.resolved,
				actionResonance: v.actionResonance,
				prevId: v.prevId,
				nextId: v.nextId,
				communityAffinityHex:
					v.communityId >= 0
						? (communityMap.get(v.communityId)?.affinityHex ?? "")
						: "",
				wireFrame: v.wireFrame ?? null,
				telemetry: v.telemetry,
			});
		}

		callbacks.onGraphSnapshot?.({
			timestamp: Date.now(),
			fields,
			orphanValues,
			totalValues: valueMap.size,
			totalCommunities: communityMap.size,
		});
	}

	connect();
	draw();

	return {
		destroy() {
			isDestroyed = true;
			if (ws) ws.close();
			ro.disconnect();
			if (canvas.parentNode) canvas.parentNode.removeChild(canvas);
		},

		sendPrompt(text: string) {
			const meta = import.meta as ImportMeta & { env?: Record<string, string> };
			const host = meta.env?.VITE_VIZ_HOST || "localhost";
			const port = meta.env?.VITE_VIZ_PORT || "6600";
			fetch(`http://${host}:${port}/api/prompt`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ prompt: text }),
			}).catch(() => {
				addLog(
					"error",
					`prompt POST failed — is viz server running at ${host}:${port}?`,
				);
			});
		},
	};
}
