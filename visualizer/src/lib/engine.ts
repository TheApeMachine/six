import { decodeVizFrames, EK, KIND_NAMES, type VizEvent } from "./wire";

/*
GF(257) arithmetic for inspector-side field lane visualization.
Mirrors pkg/core/numeric/geometry/field.go at the Mod257 tier.
*/
const GF_MOD = 257;

function gfAdd(a: number, b: number): number {
  return ((a + b) % GF_MOD + GF_MOD) % GF_MOD;
}

function gfMul(a: number, b: number): number {
  return ((a % GF_MOD) * (b % GF_MOD)) % GF_MOD;
}

function gfAffine(v: number, m: number, b: number): number {
  return gfAdd(gfMul(v, m), b);
}

function reduce257(value: number): number {
  let acc = 0;
  for (let i = 0; i < 4; i++) {
    const byte = (value >>> (8 * i)) & 0xff;
    acc += (i & 1) ? -byte : byte;
  }

  while (acc < 0) acc += 257;
  while (acc >= 257) acc -= 257;
  return acc;
}

class PhaseField {
  lanes: Uint32Array;

  constructor() {
    this.lanes = new Uint32Array(GF_MOD);
  }

  observeByte(tok: number, pos: number) {
    const w = this.lanes.length;
    const pm = ((pos % w) + w) % w;
    const mult = reduce257(pm + 1) || 1;
    const bias = gfAdd(tok, 1);
    const id = ((tok % w) + w) % w;
    const orb = ((id + pos + 1) % w + w) % w;
    const mir = ((id + pm + (w >> 1)) % w + w) % w;

    this.lanes[id] = gfAffine(this.lanes[id], mult, bias);
    this.lanes[orb] = gfAdd(this.lanes[orb], bias);
    this.lanes[mir] = gfAdd(this.lanes[mir], mult);
  }

  observeBytes(data: Uint8Array | number[]) {
    for (let i = 0; i < data.length; i++) this.observeByte(data[i], i);
  }

  dominant(): { index: number; amplitude: number; concentration: number } {
    let best = -1, bv = 0, tot = 0;

    for (let i = 0; i < this.lanes.length; i++) {
      tot += this.lanes[i];
      if (this.lanes[i] > bv) { bv = this.lanes[i]; best = i; }
    }

    return { index: best, amplitude: bv, concentration: tot > 0 ? bv / tot : 0 };
  }
}

const ACTIONS: { name: string; color: [number, number, number] }[] = [
  { name: "beam_swarm",       color: [0,   255, 150] },
  { name: "causal_explore",   color: [255, 200, 0  ] },
  { name: "active_inference", color: [255, 150, 50 ] },
  { name: "classification",   color: [100, 180, 255] },
];

const REACTIONS: { name: string; color: [number, number, number] }[] = [
  { name: "surprisal",     color: [0,   200, 255] },
  { name: "falsification", color: [255, 80,  80 ] },
];

const PROGRAM_COLORS: Record<string, [number, number, number]> = {};
for (const a of ACTIONS) PROGRAM_COLORS[a.name] = a.color;
PROGRAM_COLORS["beam_swarm_step"] = ACTIONS[0].color;
for (const r of REACTIONS) PROGRAM_COLORS[r.name] = r.color;
PROGRAM_COLORS["affinity"] = [186, 104, 200];

function programColor(name: string): [number, number, number] {
  return PROGRAM_COLORS[name] || [180, 180, 180];
}

/*
fnv1a32 yields deterministic [0,1) floats from string keys so layout matches
backend ids across runs — no Math.random placement for telemetry-driven nodes.
*/
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

function layoutPoint(key: string, width: number, height: number, inset: number): { x: number; y: number } {
  const ux = unitFromKey(key, "x");
  const uy = unitFromKey(key, "y");
  const w = Math.max(1, width - 2 * inset);
  const h = Math.max(1, height - 2 * inset);

  return { x: inset + ux * w, y: inset + uy * h };
}

const VIZ_LAYOUT_INSET = 80;

function layoutVelocity(key: string): { x: number; y: number } {
  const vx = unitFromKey(key, "vx") - 0.5;
  const vy = unitFromKey(key, "vy") - 0.5;

  return { x: vx * 0.8, y: vy * 0.8 };
}

/*
posFromWireLayout maps pkg/viz layout_meta.go normalized viz_lx/viz_ly into canvas
pixels. When the backend omits them, callers fall back to deterministic hashes.
*/
function posFromWireLayout(
  meta: Record<string, string> | undefined,
  width: number,
  height: number,
): { x: number; y: number } | null {
  if (!meta) return null;

  const xs = meta.viz_lx;
  const ys = meta.viz_ly;

  if (xs === undefined || ys === undefined) return null;

  const lx = Number.parseFloat(xs);
  const ly = Number.parseFloat(ys);

  if (!Number.isFinite(lx) || !Number.isFinite(ly)) return null;
  if (lx < 0 || lx > 1 || ly < 0 || ly > 1) return null;

  const w = Math.max(1, width - 2 * VIZ_LAYOUT_INSET);
  const h = Math.max(1, height - 2 * VIZ_LAYOUT_INSET);

  return { x: VIZ_LAYOUT_INSET + lx * w, y: VIZ_LAYOUT_INSET + ly * h };
}

function posOrFallback(
  meta: Record<string, string> | undefined,
  width: number,
  height: number,
  fallback: () => { x: number; y: number },
): { x: number; y: number } {
  const p = posFromWireLayout(meta, width, height);

  if (p) return p;

  return fallback();
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

interface VisValue {
  id: string;
  pos: { x: number; y: number };
  vel: { x: number; y: number };
  role: "data" | "action" | "reaction" | "prompt";
  program: string;
  communityId: number;
  label: string;
  content: string;
  tokens: Uint8Array | null;
  field: PhaseField | null;
  resonance: number;
  age: number;
  prevId: string;
  nextId: string;
  telemetry: ReturnType<typeof snapshotTelemetry> | null;
  actionResonance: number;
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
  actionResonance: number;
  prevId: string;
  nextId: string;
  telemetry: ReturnType<typeof snapshotTelemetry> | null;
}

export interface VizCallbacks {
  onEvent: (ev: VizEvent) => void;
  onStats: (stats: VizRuntimeStats) => void;
  onSelection?: (sel: VizInspectSnapshot | null) => void;
}

function tokensFromString(s: string): Uint8Array {
  const cap = Math.min(s.length, 256);
  const t = new Uint8Array(Math.max(16, cap));

  for (let i = 0; i < cap; i++) t[i] = s.charCodeAt(i) & 0xff;

  return t;
}

function buildField(tokens: Uint8Array): PhaseField {
  const f = new PhaseField();
  f.observeBytes(tokens);
  return f;
}

export function initEngine(container: HTMLDivElement, callbacks: VizCallbacks) {
  const canvas = document.createElement("canvas");
  canvas.style.display = "block";
  container.appendChild(canvas);

  const ctx = canvas.getContext("2d") as CanvasRenderingContext2D;
  let W = container.clientWidth;
  let H = container.clientHeight;
  canvas.width = W;
  canvas.height = H;

  let mouseX = W / 2;
  let mouseY = H / 2;
  let selectedId: string | null = null;

  canvas.addEventListener("mousemove", (e) => {
    mouseX = e.offsetX;
    mouseY = e.offsetY;
  });

  canvas.addEventListener("click", (e) => {
    const cx = e.offsetX, cy = e.offsetY;
    selectedId = null;

    for (const [vid, v] of valueMap) {
      if (Math.hypot(v.pos.x - cx, v.pos.y - cy) < 12) {
        selectedId = vid;
        break;
      }
    }

    emitSelection();
  });

  const ro = new ResizeObserver(() => {
    W = container.clientWidth;
    H = container.clientHeight;
    canvas.width = W;
    canvas.height = H;
  });
  ro.observe(container);

  const valueMap = new Map<string, VisValue>();
  const communityMap = new Map<number, VisCommunity>();
  const eventLog: { ts: number; kind: string; detail: string }[] = [];
  const stats: VizRuntimeStats = {
    values: 0,
    communities: 0,
    actions: 0,
    reactions: 0,
    dropped: 0,
    bootstrapNodes: 0,
    wireJsonBlobs: 0,
  };
  let frameCount = 0;
  let isDestroyed = false;
  let ws: WebSocket | null = null;
  let bootstrapNodeIds: string[] = [];

  const MAX_VALUES = 500;

  function addLog(kind: string, detail: string) {
    eventLog.unshift({ ts: Date.now(), kind, detail });
    if (eventLog.length > 40) eventLog.length = 40;
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

    callbacks.onSelection({
      id: v.id,
      role: v.role,
      program: v.program,
      communityId: v.communityId,
      label: v.label,
      content: v.content,
      pos: { ...v.pos },
      resonance: v.resonance,
      actionResonance: v.actionResonance,
      prevId: v.prevId,
      nextId: v.nextId,
      telemetry: v.telemetry,
    });
  }

  function spawnPos(layoutKey: string, communityId?: number): { x: number; y: number } {
    if (communityId !== undefined && communityId >= 0) {
      const c = communityMap.get(communityId);

      if (c) {
        const ox = (unitFromKey(`${layoutKey}|${communityId}`, "ox") - 0.5) * 72;
        const oy = (unitFromKey(`${layoutKey}|${communityId}`, "oy") - 0.5) * 72;

        return { x: c.center.x + ox, y: c.center.y + oy };
      }
    }

    return layoutPoint(`spawn|${layoutKey}`, W, H, 80);
  }

  function cullOldest() {
    if (valueMap.size <= MAX_VALUES) return;

    const sorted = [...valueMap.entries()]
      .filter(([, v]) => v.role !== "prompt")
      .sort((a, b) => a[1].age - b[1].age);

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

  function applyEvent(ev: VizEvent) {
    const kindName = KIND_NAMES[ev.kind] || `kind_${ev.kind}`;

    if (ev.kind === EK.Prompt) {
      const promptText = ev.meta?.prompt || "";

      for (const [pid, pv] of valueMap) {
        if (pv.role === "prompt") valueMap.delete(pid);
      }

      const vid = `prompt_${ev.ts}_${fnv1a32(promptText).toString(16)}`;
      const tokens = tokensFromString(promptText);
      const pos = posOrFallback(ev.meta, W, H, () => layoutPoint(`prompt|${vid}`, W, H, 120));
      const tel = snapshotTelemetry(ev);

      valueMap.set(vid, {
        id: vid, pos, vel: { x: 0, y: 0 },
        role: "prompt", program: "", communityId: -1,
        label: promptText.substring(0, 40), content: promptText,
        tokens, field: buildField(tokens),
        resonance: 1, age: 0, prevId: "", nextId: "",
        telemetry: tel,
        actionResonance: 0,
      });
      cullOldest();
      addLog("prompt", promptText.substring(0, 50));
    }

    else if (ev.kind === EK.PromptResult) {
      const gen = ev.meta?.generation || "";
      addLog("prompt_result", gen.substring(0, 56));
    }

    else if (ev.kind === EK.TokenizerEmit) {
      const vid = ev.meta?.value_id || `v_${ev.ts}`;
      const tokenContent = ev.meta?.content || "";
      const tokens = tokensFromString(tokenContent || vid);
      const pos = posOrFallback(ev.meta, W, H, () => spawnPos(`emit|${vid}`));
      const vel = layoutVelocity(`tok|${vid}`);
      const tel = snapshotTelemetry(ev);

      valueMap.set(vid, {
        id: vid, pos,
        vel,
        role: "data", program: "", communityId: -1,
        label: ev.lbl || "", content: tokenContent,
        tokens, field: buildField(tokens),
        resonance: 0.5, age: 0, prevId: "", nextId: "",
        telemetry: tel,
        actionResonance: 0,
      });
      cullOldest();
      addLog("tokenizer", `${(tokenContent || vid).substring(0, 30)}${ev.lbl ? " [" + ev.lbl + "]" : ""}`);
    }

    else if (ev.kind === EK.TokenizerChunk) {
      const bw = ev.vals?.bytes_written ?? 0;
      addLog("tokenizer_chunk", `+${bw} B`);
    }

    else if (ev.kind === EK.DatasetRead) {
      const ds = ev.meta?.dataset || "";
      const br = ev.vals?.bytes_read ?? 0;
      addLog("dataset", `${ds} +${br} B`);
    }

    else if (ev.kind === EK.TrieGraphSnapshot) {
      const tidx = ev.vals?.trie_idx ?? -1;
      const gj = ev.meta?.graph || "";
      addLog("trie_graph", `trie ${tidx} json ${gj.length} B`);
    }

    else if (ev.kind === EK.CommunityCreated) {
      const cid = ev.vals?.community_id ?? -1;
      const affinityHex = ev.meta?.initial_affinity || "";

      communityMap.set(cid, {
        id: cid,
        memberIds: new Set(),
        saturated: false,
        saturation: 0,
        lastAction: "",
        actionCount: 0,
        reactionCount: 0,
        center: posOrFallback(ev.meta, W, H, () => layoutPoint(`community|${cid}`, W, H, 100)),
        affinityHex,
      });
      addLog("community", `created #${cid}${affinityHex ? " aff=" + affinityHex.substring(0, 16) + ".." : ""}`);
    }

    else if (ev.kind === EK.ValueJoinedCommunity) {
      const vid = ev.meta?.value_id || "";
      const cid = ev.vals?.community_id ?? -1;
      const distance = ev.vals?.distance ?? -1;
      const c = communityMap.get(cid);

      if (c) {
        c.memberIds.add(vid);
        const v = valueMap.get(vid);

        if (v) {
          v.communityId = cid;
          v.resonance = 1;
          v.telemetry = snapshotTelemetry(ev);
        }
      }

      addLog("join", `${vid.substring(0, 8)} -> #${cid} (dist=${distance.toFixed(0)})`);
    }

    else if (ev.kind === EK.CommunitySaturated) {
      const cid = ev.vals?.community_id ?? -1;
      const sat = ev.vals?.saturation ?? 0;
      const c = communityMap.get(cid);
      if (c) { c.saturated = true; c.saturation = sat; }
      addLog("saturated", `#${cid} at ${(sat * 100).toFixed(1)}%`);
    }

    else if (ev.kind === EK.CommunityAction) {
      const prog = ev.lbl || "unknown";
      const cid = ev.vals?.community_id ?? -1;
      const c = communityMap.get(cid);
      if (c) { c.lastAction = prog; c.actionCount++; }
      stats.actions++;

      const vid = ev.meta?.value_id || ev.meta?.action_id || `a_${ev.ts}`;
      const pos = posOrFallback(ev.meta, W, H, () => spawnPos(`action|${vid}`, cid));
      const vel = layoutVelocity(`act|${vid}`);
      const tel = snapshotTelemetry(ev);
      const ar = ev.vals?.resonance ?? 0;

      valueMap.set(vid, {
        id: vid, pos,
        vel,
        role: "action", program: prog, communityId: cid,
        label: prog, content: "",
        tokens: null, field: null,
        resonance: 1, age: 0, prevId: "", nextId: "",
        telemetry: tel,
        actionResonance: ar,
      });
      cullOldest();
      addLog("action", `#${cid} -> ${prog}`);
    }

    else if (ev.kind === EK.CommunityReaction) {
      const prog = ev.lbl || "unknown";
      const cid = ev.vals?.community_id ?? -1;
      const c = communityMap.get(cid);
      if (c) c.reactionCount++;
      stats.reactions++;

      const vid = ev.meta?.value_id || ev.meta?.reaction_id || `r_${ev.ts}`;
      const pos = posOrFallback(ev.meta, W, H, () => spawnPos(`reaction|${vid}`, cid));
      const vel = layoutVelocity(`rx|${vid}`);
      const tel = snapshotTelemetry(ev);

      valueMap.set(vid, {
        id: vid, pos,
        vel,
        role: "reaction", program: prog, communityId: cid,
        label: prog, content: "",
        tokens: null, field: null,
        resonance: 0.8, age: 0, prevId: "", nextId: "",
        telemetry: tel,
        actionResonance: 0,
      });
      cullOldest();
      addLog("reaction", `#${cid} -> ${prog}`);
    }

    else if (ev.kind === EK.QueueSubmit) {
      const vid = ev.meta?.value_id || "";
      const v = valueMap.get(vid);

      if (v) {
        v.prevId = ev.meta?.prev_id || "";
        v.nextId = ev.meta?.next_id || "";
        v.telemetry = snapshotTelemetry(ev);
      }

      const inf = ev.vals?.inflight ?? -1;
      addLog("queue", `submit inf=${inf} ${(ev.lbl || "").substring(0, 40)}`);
    }

    else if (ev.kind === EK.CausalHubProbe) {
      const depth = ev.vals?.depth ?? 0;
      const status = ev.meta?.status || "unknown";
      addLog("causal_hub", `depth=${depth} ${status}`);
    }

    else if (ev.kind === EK.HolographicCrossover || ev.kind === EK.Sense) {
      addLog(kindName, ev.lbl || ev.src || "");
    }

    else {
      addLog(kindName, ev.lbl || ev.src || "");
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
      try {
        const buf = new Uint8Array(msg.data);

        for (const frame of decodeVizFrames(buf)) {
          if (frame.frameType === "event") {
            applyEvent(frame.event);
            callbacks.onEvent(frame.event);
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
            for (const ev of frame.events) {
              applyEvent(ev);
              callbacks.onEvent(ev);
            }

            continue;
          }

          if (frame.frameType === "json") {
            stats.wireJsonBlobs++;
            let summary = `len=${frame.text.length}`;

            try {
              const j = JSON.parse(frame.text) as Record<string, unknown>;

              if (typeof j.snapshot_kind === "string") summary = `snap:${j.snapshot_kind}`;
            } catch { /* non-JSON or partial */ }

            addLog("json", summary);
          }
        }
      } catch (_) { /* malformed frame */ }
    };

    ws.onclose = () => {
      if (!isDestroyed) setTimeout(connect, 2000);
    };
  }

  function updateLayout() {
    for (const [, v] of valueMap) {
      v.age++;
      v.resonance *= 0.96;

      if (v.communityId >= 0) {
        const c = communityMap.get(v.communityId);

        if (c && c.memberIds.size >= 2) {
          const dx = c.center.x - v.pos.x;
          const dy = c.center.y - v.pos.y;
          const d = Math.sqrt(dx * dx + dy * dy);

          if (d > 5) {
            v.vel.x += (dx / d) * 0.03;
            v.vel.y += (dy / d) * 0.03;
          }
        }
      }

      v.vel.x *= 0.985;
      v.vel.y *= 0.985;
      v.pos.x += v.vel.x;
      v.pos.y += v.vel.y;

      const speed = Math.sqrt(v.vel.x * v.vel.x + v.vel.y * v.vel.y);
      if (speed > 1.2) {
        v.vel.x = (v.vel.x / speed) * 1.2;
        v.vel.y = (v.vel.y / speed) * 1.2;
      }

      const margin = 30;
      if (v.pos.x < margin) v.vel.x += 0.05;
      if (v.pos.x > W - margin) v.vel.x -= 0.05;
      if (v.pos.y < margin) v.vel.y += 0.05;
      if (v.pos.y > H - margin) v.vel.y -= 0.05;
    }

    for (const [, c] of communityMap) {
      if (c.memberIds.size === 0) continue;
      let cx = 0, cy = 0, n = 0;

      for (const vid of c.memberIds) {
        const v = valueMap.get(vid);
        if (v) { cx += v.pos.x; cy += v.pos.y; n++; }
      }

      if (n > 0) {
        c.center.x += (cx / n - c.center.x) * 0.1;
        c.center.y += (cy / n - c.center.y) * 0.1;
      }
    }
  }

  function drawPressureField() {
    ctx.strokeStyle = "rgba(255,255,255,0.03)";
    ctx.lineWidth = 1;

    for (let x = 0; x < W; x += 80) {
      for (let y = 0; y < H; y += 80) {
        const angle = Math.atan2(mouseY - y, mouseX - x);
        const d = Math.hypot(mouseX - x, mouseY - y);
        const len = 3 + (1 - Math.min(d / Math.max(W, H), 1)) * 9;

        ctx.save();
        ctx.translate(x, y);
        ctx.rotate(angle);
        ctx.beginPath();
        ctx.moveTo(0, 0);
        ctx.lineTo(len, 0);
        ctx.stroke();
        ctx.restore();
      }
    }
  }

  function drawEdges() {
    ctx.strokeStyle = "rgba(255,255,255,0.04)";
    ctx.lineWidth = 0.5;
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

      const col = c.lastAction ? programColor(c.lastAction) : [186, 104, 200] as [number, number, number];
      const [cr, cg, cb] = col;
      const radius = 30 + c.memberIds.size * 8 + Math.sin(frameCount * 0.08) * 5;
      const baseAlpha = c.saturated ? 0.4 : 0.2;

      ctx.strokeStyle = `rgba(${cr},${cg},${cb},${baseAlpha})`;
      ctx.lineWidth = c.saturated ? 2 : 1;
      ctx.beginPath();
      ctx.arc(c.center.x, c.center.y, radius, 0, Math.PI * 2);
      ctx.stroke();

      ctx.strokeStyle = `rgba(${cr},${cg},${cb},${baseAlpha * 0.4})`;
      ctx.lineWidth = 0.5;
      ctx.beginPath();
      for (const vid of c.memberIds) {
        const v = valueMap.get(vid);
        if (v) {
          ctx.moveTo(c.center.x, c.center.y);
          ctx.lineTo(v.pos.x, v.pos.y);
        }
      }
      ctx.stroke();

      if (c.memberIds.size >= 3) {
        ctx.fillStyle = `rgba(${cr},${cg},${cb},0.04)`;
        ctx.beginPath();
        ctx.arc(c.center.x, c.center.y, radius * 1.3, 0, Math.PI * 2);
        ctx.fill();

        ctx.fillStyle = `rgba(${cr},${cg},${cb},0.45)`;
        ctx.font = "9px monospace";
        ctx.textAlign = "center";
        ctx.fillText(
          `#${c.id} [${c.memberIds.size}]${c.lastAction ? " " + c.lastAction : ""}`,
          c.center.x, c.center.y + radius + 12,
        );

        if (c.actionCount > 0 || c.reactionCount > 0) {
          ctx.fillStyle = `rgba(${cr},${cg},${cb},0.35)`;
          ctx.fillText(`a:${c.actionCount} r:${c.reactionCount}`, c.center.x, c.center.y + radius + 22);
        }

        if (c.saturated) {
          ctx.fillStyle = "rgba(255,80,80,0.45)";
          ctx.fillText(`SATURATED ${(c.saturation * 100).toFixed(0)}%`, c.center.x, c.center.y + radius + 32);
        }
      }
    }
  }

  function valueColor(v: VisValue): [number, number, number] {
    if (v.role === "prompt") return [255, 180, 0];
    if (v.program) return programColor(v.program);

    if (v.field) {
      const dom = v.field.dominant();
      const hue = (dom.index / GF_MOD) * 255;
      return [hue, 200, Math.max(0, 255 - hue * 0.3)];
    }

    return [100, 180, 255];
  }

  function drawValues() {
    for (const [vid, v] of valueMap) {
      if (v.role === "prompt") continue;

      const [r, g, b] = valueColor(v);
      const a = 0.47 + v.resonance * 0.53;

      ctx.save();
      ctx.translate(v.pos.x, v.pos.y);

      if (v.resonance > 0.2) {
        ctx.fillStyle = `rgba(${r},${g},${b},${v.resonance * 0.1})`;
        ctx.beginPath();
        ctx.arc(0, 0, 9 + v.resonance * 10, 0, Math.PI * 2);
        ctx.fill();
      }

      ctx.strokeStyle = `rgba(${r},${g},${b},${a})`;
      ctx.lineWidth = 1;

      if (v.role === "action") {
        ctx.beginPath();
        ctx.moveTo(0, -5);
        ctx.lineTo(5, 5);
        ctx.lineTo(-5, 5);
        ctx.closePath();
        ctx.stroke();
      } else if (v.role === "reaction") {
        ctx.beginPath();
        ctx.moveTo(0, 5);
        ctx.lineTo(5, -4);
        ctx.lineTo(-5, -4);
        ctx.closePath();
        ctx.stroke();
      } else {
        ctx.strokeRect(-4, -4, 8, 8);
      }

      if (v.program) {
        ctx.fillStyle = `rgba(${r},${g},${b},${a * 0.7})`;
        ctx.font = "8px monospace";
        ctx.textAlign = "left";
        ctx.fillText(v.program, 8, 3);
      } else if (v.content && v.role === "data") {
        ctx.fillStyle = `rgba(${r},${g},${b},${a * 0.4})`;
        ctx.font = "7px monospace";
        ctx.textAlign = "left";
        ctx.fillText(v.content.substring(0, 20), 8, 3);
      }

      if (selectedId === vid) {
        ctx.strokeStyle = "rgba(255,255,255,0.8)";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(0, 0, 14, 0, Math.PI * 2);
        ctx.stroke();
      }

      ctx.restore();
    }
  }

  /*
  Prompts drawn last so they always sit on top of everything else.
  Bright orange diamond with pulsing glow, impossible to miss.
  */
  function drawPrompts() {
    for (const [vid, v] of valueMap) {
      if (v.role !== "prompt") continue;

      const pulse = 0.6 + Math.sin(frameCount * 0.12) * 0.4;

      ctx.save();
      ctx.translate(v.pos.x, v.pos.y);

      ctx.fillStyle = `rgba(255,180,0,${0.08 * pulse})`;
      ctx.beginPath();
      ctx.arc(0, 0, 30 + pulse * 10, 0, Math.PI * 2);
      ctx.fill();

      ctx.fillStyle = `rgba(255,140,0,${0.12 * pulse})`;
      ctx.beginPath();
      ctx.arc(0, 0, 18 + pulse * 5, 0, Math.PI * 2);
      ctx.fill();

      ctx.strokeStyle = `rgba(255,200,0,${0.7 + pulse * 0.3})`;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(0, -10);
      ctx.lineTo(10, 0);
      ctx.lineTo(0, 10);
      ctx.lineTo(-10, 0);
      ctx.closePath();
      ctx.stroke();

      ctx.strokeStyle = `rgba(255,160,0,${0.4 + pulse * 0.2})`;
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(0, -15);
      ctx.lineTo(15, 0);
      ctx.lineTo(0, 15);
      ctx.lineTo(-15, 0);
      ctx.closePath();
      ctx.stroke();

      ctx.fillStyle = `rgba(255,200,0,${0.85})`;
      ctx.font = "bold 10px monospace";
      ctx.textAlign = "left";
      ctx.fillText(`PROMPT: ${v.label}`, 20, 4);

      if (selectedId === vid) {
        ctx.strokeStyle = "rgba(255,255,255,0.9)";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(0, 0, 20, 0, Math.PI * 2);
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
    const prompts = allVals.filter(v => v.role === "prompt").length;
    const data = allVals.filter(v => v.role === "data").length;
    const actionVals = allVals.filter(v => v.role === "action").length;
    const reactionVals = allVals.filter(v => v.role === "reaction").length;
    const liveCommunities = [...communityMap.values()].filter(c => c.memberIds.size >= 2).length;
    const totalCommunities = [...communityMap.values()].filter(c => c.memberIds.size > 0).length;
    const saturated = [...communityMap.values()].filter(c => c.saturated).length;

    ctx.fillText(`values: ${valueMap.size}`, x, y); y += 16;

    ctx.fillStyle = "rgba(255,180,0,0.9)";
    ctx.fillText(`  prompts: ${prompts}`, x, y); y += 14;

    ctx.fillStyle = "rgba(100,180,255,0.7)";
    ctx.fillText(`  data: ${data}`, x, y); y += 14;

    ctx.fillStyle = "rgba(0,255,150,0.7)";
    ctx.fillText(`  actions: ${actionVals}  (total: ${stats.actions})`, x, y); y += 14;

    ctx.fillStyle = "rgba(255,150,50,0.7)";
    ctx.fillText(`  reactions: ${reactionVals}  (total: ${stats.reactions})`, x, y); y += 18;

    ctx.fillStyle = "rgba(186,104,200,0.7)";
    ctx.fillText(`communities: ${liveCommunities} active  (${totalCommunities} total, ${saturated} sat)`, x, y); y += 18;

    ctx.fillStyle = stats.dropped > 0 ? "rgba(255,100,100,0.9)" : "rgba(255,255,255,0.35)";
    ctx.fillText(`bus dropped: ${stats.dropped}`, x, y); y += 14;

    ctx.fillStyle = "rgba(255,255,255,0.35)";
    ctx.fillText(`bootstrap: ${bootstrapNodeIds.length} peers   json: ${stats.wireJsonBlobs}`, x, y); y += 20;

    ctx.fillStyle = "rgba(255,255,255,0.3)";
    ctx.font = "9px monospace";
    ctx.fillText("click value to inspect (see JSON panel)", x, y); y += 16;

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
    ctx.fillText("ACTIONS", x, y); y += 14;

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
    ctx.fillText("REACTIONS", x, y); y += 14;

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

  function drawInspector() {
    if (!selectedId) return;
    const v = valueMap.get(selectedId);

    if (!v) {
      selectedId = null;
      emitSelection();
      return;
    }

    const px = W - 340;
    const py = 20;
    let y = py;
    let panelH = 168;

    if (v.field) panelH += 118;
    if (v.telemetry) panelH += 118;
    if (v.communityId >= 0) panelH += 52;
    if (v.prevId || v.nextId) panelH += 14;
    if (v.role === "action" && v.actionResonance > 0) panelH += 14;

    panelH = Math.min(460, panelH);

    ctx.fillStyle = "rgba(0,0,0,0.75)";
    ctx.fillRect(px - 10, py - 10, 330, panelH);
    ctx.strokeStyle = "rgba(255,255,255,0.08)";
    ctx.strokeRect(px - 10, py - 10, 330, panelH);

    ctx.fillStyle = "rgba(255,255,255,0.9)";
    ctx.font = "11px monospace";
    ctx.textAlign = "left";

    ctx.fillText(`VALUE ${v.id}`, px, y); y += 16;

    ctx.fillStyle = "rgba(255,255,255,0.5)";
    ctx.fillText(`role: ${v.role}${v.program ? "  program: " + v.program : ""}`, px, y); y += 14;
    ctx.fillText(`community: ${v.communityId >= 0 ? "#" + v.communityId : "none"}`, px, y); y += 14;
    ctx.fillText(`age: ${v.age}  resonance: ${v.resonance.toFixed(3)}`, px, y); y += 14;
    ctx.fillText(`pos: (${v.pos.x.toFixed(1)}, ${v.pos.y.toFixed(1)})`, px, y); y += 14;

    if (v.role === "action" && v.actionResonance > 0) {
      ctx.fillStyle = "rgba(0,255,180,0.55)";
      ctx.fillText(`wire resonance: ${v.actionResonance.toFixed(4)}`, px, y); y += 14;
    }

    if (v.prevId || v.nextId) {
      ctx.fillStyle = "rgba(200,200,255,0.55)";
      ctx.font = "9px monospace";
      ctx.fillText(`queue prev ${v.prevId || "—"}  next ${v.nextId || "—"}`, px, y); y += 14;
    }

    ctx.font = "11px monospace";

    if (v.label) {
      ctx.fillStyle = "rgba(255,200,0,0.7)";
      ctx.fillText(`label: ${v.label}`, px, y); y += 14;
    }

    if (v.content) {
      ctx.fillStyle = "rgba(0,255,150,0.8)";
      ctx.font = "10px monospace";
      ctx.fillText(`content: "${v.content.substring(0, 40)}"`, px, y); y += 14;

      if (v.content.length > 40) {
        ctx.fillText(`  "${v.content.substring(40, 80)}"`, px, y); y += 14;
      }
    }

    if (v.field) {
      const dom = v.field.dominant();
      ctx.fillStyle = "rgba(255,255,255,0.5)";
      ctx.font = "9px monospace";
      ctx.fillText(
        `dominant: idx=${dom.index}  amp=${dom.amplitude}  conc=${dom.concentration.toFixed(4)}`,
        px, y,
      ); y += 14;

      ctx.fillStyle = "rgba(255,255,255,0.3)";
      ctx.font = "8px monospace";
      ctx.fillText("field lanes (64 of 257):", px, y); y += 10;

      const maxLane = Math.max(1, ...Array.from(v.field.lanes.slice(0, 64)));
      const barY = y + 30;
      for (let i = 0; i < 64; i++) {
        const h = (v.field.lanes[i] / maxLane) * 30;
        const hue = (i / 64) * 255;
        ctx.fillStyle = `rgba(${hue},200,${Math.max(0, 255 - hue * 0.3)},0.4)`;
        ctx.fillRect(px + i * 4.5, barY - h, 3, h);
      }

      y = barY + 8;
    }

    if (v.telemetry) {
      const tel = v.telemetry;
      ctx.fillStyle = "rgba(140,220,255,0.65)";
      ctx.font = "8px monospace";
      ctx.fillText(`wire ts(µs) ${tel.ts}`, px, y); y += 10;
      ctx.fillText(`src ${tel.src}  tgt ${tel.tgt}`, px, y); y += 10;

      let nk = 0;
      ctx.fillStyle = "rgba(200,220,255,0.5)";
      for (const key of Object.keys(tel.vals).sort()) {
        if (nk++ >= 6) {
          ctx.fillText("vals …", px, y); y += 10;
          break;
        }

        ctx.fillText(`vals.${key}=${tel.vals[key]}`, px, y); y += 10;
      }

      nk = 0;
      ctx.fillStyle = "rgba(220,200,255,0.5)";
      for (const key of Object.keys(tel.meta).sort()) {
        if (nk++ >= 6) {
          ctx.fillText("meta …", px, y); y += 10;
          break;
        }

        const mv = tel.meta[key];
        const clip = mv.length > 48 ? `${mv.substring(0, 46)}..` : mv;
        ctx.fillText(`meta.${key}=${clip}`, px, y); y += 10;
      }
    }

    if (v.communityId >= 0) {
      const c = communityMap.get(v.communityId);

      if (c) {
        y += 6;
        ctx.fillStyle = "rgba(186,104,200,0.6)";
        ctx.font = "9px monospace";
        ctx.fillText(`community #${c.id}: ${c.memberIds.size} members`, px, y); y += 12;
        ctx.fillText(`actions: ${c.actionCount}  reactions: ${c.reactionCount}`, px, y); y += 12;

        if (c.affinityHex) {
          const aff = c.affinityHex.length > 36 ? `${c.affinityHex.substring(0, 34)}..` : c.affinityHex;
          ctx.fillText(`affinity ${aff}`, px, y); y += 12;
        }

        if (c.saturated) {
          ctx.fillStyle = "rgba(255,80,80,0.6)";
          ctx.fillText(`SATURATED (${(c.saturation * 100).toFixed(1)}%)`, px, y); y += 12;
        }
      }
    }
  }

  function drawEventLog() {
    if (eventLog.length === 0) return;

    const pw = 300;
    const px = W - pw - 10;
    const visibleCount = Math.min(eventLog.length, 25);
    const py = selectedId ? 420 : 20;

    ctx.fillStyle = "rgba(0,0,0,0.35)";
    ctx.fillRect(px - 4, py - 14, pw + 8, 14 + visibleCount * 12 + 4);

    ctx.fillStyle = "rgba(255,255,255,0.3)";
    ctx.font = "9px monospace";
    ctx.textAlign = "left";
    ctx.fillText("EVENT LOG", px, py);

    for (let i = 0; i < visibleCount; i++) {
      const entry = eventLog[i];
      const age = ((Date.now() - entry.ts) / 1000).toFixed(1);

      let color = "rgba(255,255,255,0.2)";
      if (entry.kind === "prompt") color = "rgba(255,180,0,0.7)";
      else if (entry.kind === "tokenizer") color = "rgba(100,180,255,0.5)";
      else if (entry.kind === "community") color = "rgba(186,104,200,0.5)";
      else if (entry.kind === "join") color = "rgba(186,104,200,0.4)";
      else if (entry.kind === "action") color = "rgba(0,255,150,0.5)";
      else if (entry.kind === "reaction") color = "rgba(255,106,64,0.5)";
      else if (entry.kind === "saturated") color = "rgba(255,80,80,0.5)";
      else if (entry.kind === "system") color = "rgba(255,255,255,0.4)";
      else if (entry.kind === "viz_bus") color = "rgba(255,120,120,0.65)";
      else if (entry.kind === "json") color = "rgba(180,255,200,0.5)";
      else if (entry.kind === "prompt_result") color = "rgba(100,255,180,0.55)";
      else if (entry.kind === "dataset") color = "rgba(120,200,255,0.45)";
      else if (entry.kind === "tokenizer_chunk") color = "rgba(130,160,255,0.45)";
      else if (entry.kind === "trie_graph") color = "rgba(255,200,120,0.5)";

      ctx.fillStyle = color;
      ctx.fillText(
        `${age.padStart(6)}s  ${entry.kind.padEnd(10).substring(0, 10)}  ${entry.detail.substring(0, 34)}`,
        px, py + 14 + i * 12,
      );
    }
  }

  function drawWaiting() {
    if (valueMap.size > 0) return;

    ctx.fillStyle = "rgba(255,255,255,0.15)";
    ctx.font = "14px monospace";
    ctx.textAlign = "center";
    ctx.fillText("waiting for events...", W / 2, H / 2);

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

    drawPressureField();
    updateLayout();
    drawEdges();
    drawCommunities();
    drawValues();
    drawPrompts();
    drawHUD();
    drawInspector();
    drawEventLog();
    drawWaiting();

    stats.values = valueMap.size;
    stats.communities = communityMap.size;
    stats.bootstrapNodes = bootstrapNodeIds.length;
    callbacks.onStats(stats);
  }

  connect();
  draw();

  return {
    destroy() {
      isDestroyed = true;
      if (ws) ws.close();
      ro.disconnect();
      container.removeChild(canvas);
    },

    sendPrompt(text: string) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "prompt", text }));
      }
    },
  };
}
