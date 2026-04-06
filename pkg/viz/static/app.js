import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

// ─── Event Kind enum (mirrors Go) ─────────────────────────────────────────
const EK = {
  NodeCreated: 0, NodeUpdated: 1, NodeRemoved: 2,
  PeerAdded: 3, PeerRemoved: 4, PeerLatency: 5,
  ValuePublished: 6, ValueReplicated: 7,
  GossipSent: 8, GossipReceived: 9,
  FieldDigest: 10, EigenmodeDetected: 11, FieldPressure: 12,
  TrieInsert: 13, TrieDecay: 14, TriePrune: 15,
  TriePredict: 16, TrieClassify: 17, TrieExperience: 18,
  PoolSchedule: 19, PoolComplete: 20,
  AdaptiveUpdate: 21,
  Prompt: 22, PromptResult: 23,
};

const KIND_NAMES = Object.fromEntries(Object.entries(EK).map(([k,v])=>[v,k]));

// ─── State ─────────────────────────────────────────────────────────────────
const state = {
  nodes: new Map(),       // id → { mesh, data, edges: Set<edgeId> }
  edges: new Map(),       // "from→to" → { line, data }
  particles: [],          // animated data flow particles
  events: [],             // full event log
  paused: false,
  scrubPos: -1,           // -1 = live
  selected: null,         // selected node id
  eventCount: 0,
  droppedCount: 0,
};

// ─── Three.js setup ────────────────────────────────────────────────────────
const canvas = document.getElementById('canvas');
const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: false });
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
renderer.setSize(window.innerWidth, window.innerHeight);
renderer.setClearColor(0x0a0a0f);

const scene = new THREE.Scene();
scene.fog = new THREE.FogExp2(0x0a0a0f, 0.008);

const camera = new THREE.PerspectiveCamera(60, window.innerWidth / window.innerHeight, 0.1, 1000);
camera.position.set(0, 30, 60);

const controls = new OrbitControls(camera, canvas);
controls.enableDamping = true;
controls.dampingFactor = 0.05;
controls.minDistance = 5;
controls.maxDistance = 200;

// Lighting — subtle, functional.
const ambientLight = new THREE.AmbientLight(0x1a1a2e, 0.6);
scene.add(ambientLight);

const dirLight = new THREE.DirectionalLight(0x4a5a8e, 0.8);
dirLight.position.set(20, 40, 30);
scene.add(dirLight);

const pointLight = new THREE.PointLight(0x7c8aff, 0.4, 100);
pointLight.position.set(0, 20, 0);
scene.add(pointLight);

// Grid floor.
const grid = new THREE.GridHelper(200, 80, 0x111128, 0x0d0d1a);
scene.add(grid);

// ─── Materials ─────────────────────────────────────────────────────────────
const nodeMat = new THREE.MeshPhongMaterial({ color: 0x4a7aff, emissive: 0x1a2a5e, shininess: 60 });
const nodeHighMat = new THREE.MeshPhongMaterial({ color: 0xff6a4a, emissive: 0x5a1a0a, shininess: 60 });
const nodeLowMat = new THREE.MeshPhongMaterial({ color: 0x4aff8a, emissive: 0x0a3a1a, shininess: 60 });
const nodeDominantMat = new THREE.MeshPhongMaterial({ color: 0xffa04a, emissive: 0x3a2a0a, shininess: 80 });
const selectedMat = new THREE.MeshPhongMaterial({ color: 0xffffff, emissive: 0x3a3a5e, shininess: 100 });

const edgeMat = new THREE.LineBasicMaterial({ color: 0x1a2a4e, transparent: true, opacity: 0.3 });
const edgeActiveMat = new THREE.LineBasicMaterial({ color: 0x4a5a8e, transparent: true, opacity: 0.6 });
const gossipMat = new THREE.LineBasicMaterial({ color: 0x8a4aff, transparent: true, opacity: 0.5 });

const particleMat = new THREE.MeshBasicMaterial({ color: 0xff4a8a });
const particleGeo = new THREE.SphereGeometry(0.15, 6, 6);

const nodeGeo = new THREE.SphereGeometry(1, 16, 16);
const nodeSmallGeo = new THREE.SphereGeometry(0.6, 12, 12);

// ─── Node layout (force-directed, simple) ──────────────────────────────────
const LAYOUT = {
  repulsion: 800,
  attraction: 0.005,
  damping: 0.85,
  center: 0.001,
};

function layoutStep() {
  const nodes = [...state.nodes.values()];
  if (nodes.length < 2) return;

  // Repulsion between all pairs.
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i], b = nodes[j];
      const dx = a.mesh.position.x - b.mesh.position.x;
      const dz = a.mesh.position.z - b.mesh.position.z;
      const dist2 = dx * dx + dz * dz + 1;
      const f = LAYOUT.repulsion / dist2;
      const fx = (dx / Math.sqrt(dist2)) * f;
      const fz = (dz / Math.sqrt(dist2)) * f;
      a.vx = (a.vx || 0) + fx;
      a.vz = (a.vz || 0) + fz;
      b.vx = (b.vx || 0) - fx;
      b.vz = (b.vz || 0) - fz;
    }
  }

  // Attraction along edges.
  for (const [, edge] of state.edges) {
    const a = state.nodes.get(edge.data.from);
    const b = state.nodes.get(edge.data.to);
    if (!a || !b) continue;
    const dx = b.mesh.position.x - a.mesh.position.x;
    const dz = b.mesh.position.z - a.mesh.position.z;
    a.vx = (a.vx || 0) + dx * LAYOUT.attraction;
    a.vz = (a.vz || 0) + dz * LAYOUT.attraction;
    b.vx = (b.vx || 0) - dx * LAYOUT.attraction;
    b.vz = (b.vz || 0) - dz * LAYOUT.attraction;
  }

  // Center gravity + apply.
  for (const n of nodes) {
    n.vx = (n.vx || 0) - n.mesh.position.x * LAYOUT.center;
    n.vz = (n.vz || 0) - n.mesh.position.z * LAYOUT.center;
    n.vx *= LAYOUT.damping;
    n.vz *= LAYOUT.damping;
    n.mesh.position.x += n.vx;
    n.mesh.position.z += n.vz;
  }

  // Update edge geometries.
  for (const [, edge] of state.edges) {
    const a = state.nodes.get(edge.data.from);
    const b = state.nodes.get(edge.data.to);
    if (!a || !b) continue;
    const points = [a.mesh.position.clone(), b.mesh.position.clone()];
    edge.line.geometry.setFromPoints(points);
    edge.line.geometry.verticesNeedUpdate = true;
  }
}

// ─── Event handlers ────────────────────────────────────────────────────────
function handleEvent(ev) {
  state.events.push(ev);
  state.eventCount++;

  if (state.paused && state.scrubPos >= 0) return; // buffered but not applied

  applyEvent(ev);
  addLogEntry(ev);
}

function applyEvent(ev) {
  switch (ev.kind) {
    case EK.NodeCreated:
      createNode(ev.src, ev.lbl, ev.vals);
      break;

    case EK.NodeUpdated:
      updateNode(ev.src, ev.vals);
      break;

    case EK.NodeRemoved:
      removeNode(ev.src);
      break;

    case EK.PeerAdded:
      ensureNode(ev.src);
      ensureNode(ev.tgt);
      addEdge(ev.src, ev.tgt);
      break;

    case EK.ValuePublished:
      pulseNode(ev.src, 0xff4a8a);
      break;

    case EK.ValueReplicated:
      ensureNode(ev.src);
      ensureNode(ev.tgt);
      addEdge(ev.src, ev.tgt);
      spawnParticle(ev.src, ev.tgt, 0xff4a8a);
      break;

    case EK.GossipSent:
      pulseNode(ev.src, 0x8a4aff);
      break;

    case EK.GossipReceived:
      ensureNode(ev.src);
      ensureNode(ev.tgt);
      spawnParticle(ev.tgt, ev.src, 0x8a4aff);
      break;

    case EK.FieldDigest:
      updateNodeFromDigest(ev.src, ev.vals);
      break;

    case EK.EigenmodeDetected:
      markDominantMode(ev.src, ev.vals);
      break;

    case EK.FieldPressure:
      updateNodePressure(ev.src, ev.vals);
      break;

    case EK.TrieInsert:
    case EK.TrieExperience:
      pulseNode(ev.src, 0x4aff8a);
      break;

    case EK.TriePredict:
    case EK.TrieClassify:
      pulseNode(ev.src, 0xffa04a);
      break;

    case EK.AdaptiveUpdate:
      updateNode(ev.src, ev.vals);
      break;

    case EK.Prompt:
      ensureNode('prompt');
      pulseNode('prompt', 0x7c8aff);
      break;

    case EK.PromptResult:
      ensureNode('prompt');
      pulseNode('prompt', 0x4aff8a);
      break;

    case EK.PoolSchedule:
      ensureNode('pool');
      pulseNode('pool', 0xffa04a);
      break;

    case EK.PoolComplete:
      ensureNode('pool');
      pulseNode('pool', 0x4aff8a);
      break;
  }

  // Update inspector if this event concerns the selected node.
  if (state.selected && (ev.src === state.selected || ev.tgt === state.selected)) {
    refreshInspector();
  }
}

// ─── Node management ───────────────────────────────────────────────────────
function ensureNode(id) {
  if (!id || state.nodes.has(id)) return;
  createNode(id, id, {});
}

function createNode(id, label, vals) {
  if (state.nodes.has(id)) return;

  const mesh = new THREE.Mesh(nodeGeo, nodeMat.clone());
  // Random initial position in a disk.
  const angle = Math.random() * Math.PI * 2;
  const r = 5 + Math.random() * 25;
  mesh.position.set(Math.cos(angle) * r, 1, Math.sin(angle) * r);
  mesh.userData.id = id;
  scene.add(mesh);

  // Label sprite.
  const sprite = makeLabel(label || id);
  sprite.position.set(0, 1.8, 0);
  mesh.add(sprite);

  state.nodes.set(id, {
    mesh, sprite,
    data: { id, label, vals: vals || {}, pressure: {}, digest: {} },
    edges: new Set(),
    vx: 0, vz: 0,
    pulseTime: 0,
  });

  updateStats();
}

function updateNode(id, vals) {
  const n = state.nodes.get(id);
  if (!n) return;
  Object.assign(n.data.vals, vals);
}

function removeNode(id) {
  const n = state.nodes.get(id);
  if (!n) return;
  scene.remove(n.mesh);
  // Remove associated edges.
  for (const eid of n.edges) {
    const e = state.edges.get(eid);
    if (e) {
      scene.remove(e.line);
      state.edges.delete(eid);
    }
  }
  state.nodes.delete(id);
  if (state.selected === id) closeInspector();
  updateStats();
}

function updateNodeFromDigest(id, vals) {
  const n = state.nodes.get(id);
  if (!n) { ensureNode(id); return; }
  n.data.digest = vals;

  // Color by surprisal: green (low) → blue (normal) → red (high).
  const s = vals.surprisal || 0;
  if (s > 5) {
    n.mesh.material.color.setHex(0xff6a4a);
    n.mesh.material.emissive.setHex(0x5a1a0a);
  } else if (s < 1) {
    n.mesh.material.color.setHex(0x4aff8a);
    n.mesh.material.emissive.setHex(0x0a3a1a);
  } else {
    const t = (s - 1) / 4; // 0-1
    n.mesh.material.color.setHex(lerpColor(0x4aff8a, 0xff6a4a, t));
    n.mesh.material.emissive.setHex(lerpColor(0x0a3a1a, 0x5a1a0a, t));
  }

  // Scale by entropy.
  const e = vals.entropy || 0;
  const scale = 0.5 + Math.min(e, 3) * 0.5;
  n.mesh.scale.setScalar(scale);
}

function markDominantMode(id, vals) {
  const n = state.nodes.get(id);
  if (!n) return;
  // Briefly highlight dominant mode nodes.
  if ((vals.dominant_energy || 0) > 0) {
    n.mesh.material.color.setHex(0xffa04a);
    n.mesh.material.emissive.setHex(0x3a2a0a);
  }
}

function updateNodePressure(id, vals) {
  const n = state.nodes.get(id);
  if (!n) return;
  n.data.pressure = vals;

  // Y-position encodes field pressure: high decay pressure → lower, high learning → higher.
  const dp = vals.decay || 0;
  const lp = vals.learning || 0;
  n.mesh.position.y = 1 + (lp - dp) * 2;
}

function pulseNode(id, color) {
  const n = state.nodes.get(id);
  if (!n) { ensureNode(id); return; }
  n.pulseTime = performance.now();
  n.pulseColor = color;
}

// ─── Edge management ───────────────────────────────────────────────────────
function edgeId(from, to) {
  return from < to ? `${from}→${to}` : `${to}→${from}`;
}

function addEdge(from, to) {
  const eid = edgeId(from, to);
  if (state.edges.has(eid)) return;

  const nA = state.nodes.get(from);
  const nB = state.nodes.get(to);
  if (!nA || !nB) return;

  const geo = new THREE.BufferGeometry().setFromPoints([
    nA.mesh.position.clone(), nB.mesh.position.clone()
  ]);
  const line = new THREE.Line(geo, edgeMat.clone());
  scene.add(line);

  state.edges.set(eid, { line, data: { from, to } });
  nA.edges.add(eid);
  nB.edges.add(eid);
  updateStats();
}

// ─── Particles (data flow animation) ───────────────────────────────────────
function spawnParticle(from, to, color) {
  const nA = state.nodes.get(from);
  const nB = state.nodes.get(to);
  if (!nA || !nB) return;

  const mesh = new THREE.Mesh(particleGeo, new THREE.MeshBasicMaterial({ color }));
  mesh.position.copy(nA.mesh.position);
  scene.add(mesh);

  state.particles.push({
    mesh,
    from: nA.mesh.position,
    to: nB.mesh.position,
    t: 0,
    speed: 0.015 + Math.random() * 0.01,
  });
}

function updateParticles() {
  for (let i = state.particles.length - 1; i >= 0; i--) {
    const p = state.particles[i];
    p.t += p.speed;
    if (p.t >= 1) {
      scene.remove(p.mesh);
      state.particles.splice(i, 1);
      continue;
    }
    p.mesh.position.lerpVectors(p.from, p.to, p.t);
    // Slight arc.
    p.mesh.position.y += Math.sin(p.t * Math.PI) * 3;
    // Fade out.
    p.mesh.material.opacity = 1 - p.t * 0.5;
    p.mesh.material.transparent = true;
  }
}

// ─── Label sprites ─────────────────────────────────────────────────────────
function makeLabel(text) {
  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d');
  canvas.width = 256;
  canvas.height = 64;
  ctx.font = '24px monospace';
  ctx.fillStyle = '#606878';
  ctx.textAlign = 'center';
  ctx.fillText(text.substring(0, 16), 128, 40);

  const tex = new THREE.CanvasTexture(canvas);
  const mat = new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false });
  const sprite = new THREE.Sprite(mat);
  sprite.scale.set(4, 1, 1);
  return sprite;
}

// ─── Raycasting (click to inspect) ─────────────────────────────────────────
const raycaster = new THREE.Raycaster();
const mouse = new THREE.Vector2();

canvas.addEventListener('click', (e) => {
  mouse.x = (e.clientX / window.innerWidth) * 2 - 1;
  mouse.y = -(e.clientY / window.innerHeight) * 2 + 1;
  raycaster.setFromCamera(mouse, camera);

  const meshes = [...state.nodes.values()].map(n => n.mesh);
  const hits = raycaster.intersectObjects(meshes);

  if (hits.length > 0) {
    selectNode(hits[0].object.userData.id);
  } else {
    closeInspector();
  }
});

function selectNode(id) {
  // Deselect previous.
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.mesh.material = prev._savedMat || nodeMat.clone();
  }

  state.selected = id;
  const n = state.nodes.get(id);
  if (!n) return;
  n._savedMat = n.mesh.material.clone();
  n.mesh.material = selectedMat.clone();

  refreshInspector();
  document.getElementById('inspector').classList.add('open');
}

function closeInspector() {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev && prev._savedMat) prev.mesh.material = prev._savedMat;
  }
  state.selected = null;
  document.getElementById('inspector').classList.remove('open');
}

function refreshInspector() {
  const n = state.nodes.get(state.selected);
  if (!n) return;

  document.getElementById('inspector-title').textContent = n.data.label || n.data.id;
  const body = document.getElementById('inspector-body');

  let html = '';

  // Digest values.
  if (Object.keys(n.data.digest).length) {
    html += '<div class="insp-section"><h4>Field Digest</h4>';
    for (const [k, v] of Object.entries(n.data.digest)) {
      html += `<div class="insp-row"><span class="insp-key">${k}</span><span class="insp-val">${typeof v === 'number' ? v.toFixed(4) : v}</span></div>`;
    }
    html += '</div>';
  }

  // Pressure.
  if (Object.keys(n.data.pressure).length) {
    html += '<div class="insp-section"><h4>Field Pressure</h4>';
    for (const [k, v] of Object.entries(n.data.pressure)) {
      html += `<div class="insp-row"><span class="insp-key">${k}</span><span class="insp-val">${typeof v === 'number' ? v.toFixed(4) : v}</span></div>`;
    }
    html += '</div>';
  }

  // Generic values.
  if (Object.keys(n.data.vals).length) {
    html += '<div class="insp-section"><h4>Values</h4>';
    for (const [k, v] of Object.entries(n.data.vals)) {
      html += `<div class="insp-row"><span class="insp-key">${k}</span><span class="insp-val">${typeof v === 'number' ? v.toFixed(4) : v}</span></div>`;
    }
    html += '</div>';
  }

  // Connections.
  html += `<div class="insp-section"><h4>Connections (${n.edges.size})</h4>`;
  for (const eid of n.edges) {
    const e = state.edges.get(eid);
    if (e) {
      const peer = e.data.from === state.selected ? e.data.to : e.data.from;
      html += `<div class="insp-row"><span class="insp-key">${peer}</span></div>`;
    }
  }
  html += '</div>';

  // Recent events for this node.
  const recent = state.events.filter(e => e.src === state.selected || e.tgt === state.selected).slice(-20);
  if (recent.length) {
    html += '<div class="insp-section"><h4>Recent Events</h4>';
    for (const e of recent.reverse()) {
      html += `<div class="insp-row"><span class="insp-key">${KIND_NAMES[e.kind] || e.kind}</span><span class="insp-val">${e.lbl || ''}</span></div>`;
    }
    html += '</div>';
  }

  body.innerHTML = html;
}

// ─── Event log panel ───────────────────────────────────────────────────────
const logEl = document.getElementById('eventlog');
const MAX_LOG = 500;

function addLogEntry(ev) {
  const div = document.createElement('div');
  div.className = 'log-entry';
  const ts = new Date(ev.ts / 1000).toISOString().substr(11, 12);
  div.innerHTML = `<span class="log-time">${ts}</span><span class="log-kind">${KIND_NAMES[ev.kind] || ev.kind}</span><span class="log-src">${ev.src}${ev.tgt ? ' → ' + ev.tgt : ''}</span> ${ev.lbl || ''}`;
  logEl.appendChild(div);

  // Trim.
  while (logEl.children.length > MAX_LOG) {
    logEl.removeChild(logEl.firstChild);
  }

  logEl.scrollTop = logEl.scrollHeight;
}

// ─── Timeline / scrubbing ──────────────────────────────────────────────────
const timelineBar = document.getElementById('timeline-bar');
const timelineFill = document.getElementById('timeline-fill');
const timelineCursor = document.getElementById('timeline-cursor');
const timelineLabel = document.getElementById('timeline-label');

function updateTimeline() {
  const total = state.events.length;
  const pos = state.scrubPos >= 0 ? state.scrubPos : total;
  const pct = total > 0 ? (pos / total) * 100 : 0;
  timelineFill.style.width = pct + '%';
  timelineCursor.style.left = pct + '%';
  timelineLabel.textContent = `${pos} / ${total}`;
}

timelineBar.addEventListener('click', (e) => {
  if (!state.paused) return;
  const rect = timelineBar.getBoundingClientRect();
  const pct = (e.clientX - rect.left) / rect.width;
  scrubTo(Math.floor(pct * state.events.length));
});

function scrubTo(pos) {
  pos = Math.max(0, Math.min(pos, state.events.length));
  state.scrubPos = pos;

  // Rebuild scene from scratch up to pos.
  clearScene();
  for (let i = 0; i < pos; i++) {
    applyEvent(state.events[i]);
  }
  updateTimeline();
}

function clearScene() {
  for (const [, n] of state.nodes) scene.remove(n.mesh);
  for (const [, e] of state.edges) scene.remove(e.line);
  for (const p of state.particles) scene.remove(p.mesh);
  state.nodes.clear();
  state.edges.clear();
  state.particles.length = 0;
}

// ─── Button handlers ───────────────────────────────────────────────────────
document.getElementById('btn-pause').addEventListener('click', function() {
  state.paused = !state.paused;
  this.textContent = state.paused ? '▶ Resume' : '⏸ Pause';
  this.classList.toggle('active', state.paused);
  if (!state.paused) {
    state.scrubPos = -1;
    // Replay any events that arrived while paused.
    clearScene();
    for (const ev of state.events) applyEvent(ev);
  }
});

document.getElementById('btn-log').addEventListener('click', function() {
  logEl.classList.toggle('open');
  this.classList.toggle('active');
});

document.getElementById('btn-prompt').addEventListener('click', function() {
  const panel = document.getElementById('prompt-panel');
  panel.classList.toggle('open');
  this.classList.toggle('active');
  if (panel.classList.contains('open')) {
    document.getElementById('prompt-input').focus();
  }
});

document.getElementById('btn-close-inspector').addEventListener('click', closeInspector);

document.getElementById('btn-tl-start').addEventListener('click', () => {
  if (state.paused) scrubTo(0);
});
document.getElementById('btn-tl-end').addEventListener('click', () => {
  if (state.paused) scrubTo(state.events.length);
});

// Save/Load.
document.getElementById('btn-save').addEventListener('click', () => {
  const blob = new Blob([JSON.stringify(state.events)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `six-viz-${Date.now()}.json`;
  a.click();
  URL.revokeObjectURL(url);
});

document.getElementById('btn-load').addEventListener('click', () => {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.json';
  input.onchange = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const text = await file.text();
    const events = JSON.parse(text);
    state.events = events;
    state.eventCount = events.length;
    state.paused = true;
    document.getElementById('btn-pause').textContent = '▶ Resume';
    document.getElementById('btn-pause').classList.add('active');
    scrubTo(events.length);
  };
  input.click();
});

// Prompt submission.
document.getElementById('prompt-input').addEventListener('keydown', async (e) => {
  if (e.key !== 'Enter') return;
  const input = e.target;
  const prompt = input.value.trim();
  if (!prompt) return;

  input.value = '';
  const resultEl = document.getElementById('prompt-result');
  resultEl.textContent = 'Sending...';

  try {
    const resp = await fetch('/api/prompt', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ prompt }),
    });
    const data = await resp.json();
    let text = `Generation: ${data.generation || '(empty)'}\n\nClassification:\n`;
    if (data.classification) {
      for (const [k, v] of Object.entries(data.classification)) {
        text += `  ${k}: ${v.toFixed(2)}%\n`;
      }
    }
    resultEl.textContent = text;
  } catch (err) {
    resultEl.textContent = `Error: ${err.message}`;
  }
});

// ─── WebSocket connection ──────────────────────────────────────────────────
let ws = null;
let reconnectTimer = null;

function connect() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/ws`;
  console.log('[viz] connecting to', url);
  ws = new WebSocket(url);

  ws.onopen = () => {
    console.log('[viz] websocket connected');
    document.title = 'Six — Connected';
  };

  ws.onmessage = (msg) => {
    try {
      const ev = JSON.parse(msg.data);
      // Handle server responses.
      if (ev.action) {
        handleServerResponse(ev);
        return;
      }
      handleEvent(ev);
    } catch (e) {
      console.warn('[viz] parse error:', e, msg.data);
    }
  };

  ws.onclose = () => {
    console.log('[viz] websocket closed, reconnecting in 2s');
    document.title = 'Six — Disconnected';
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 2000);
  };

  ws.onerror = (e) => {
    console.error('[viz] websocket error:', e);
    ws.close();
  };
}

function handleServerResponse(resp) {
  if (resp.action === 'scrub_result' && resp.events) {
    clearScene();
    for (const ev of resp.events) applyEvent(ev);
  }
}

connect();

// ─── Utility ───────────────────────────────────────────────────────────────
function lerpColor(a, b, t) {
  const ar = (a >> 16) & 0xff, ag = (a >> 8) & 0xff, ab = a & 0xff;
  const br = (b >> 16) & 0xff, bg = (b >> 8) & 0xff, bb = b & 0xff;
  const r = Math.round(ar + (br - ar) * t);
  const g = Math.round(ag + (bg - ag) * t);
  const bl = Math.round(ab + (bb - ab) * t);
  return (r << 16) | (g << 8) | bl;
}

function updateStats() {
  document.getElementById('stat-nodes').textContent = state.nodes.size;
  document.getElementById('stat-edges').textContent = state.edges.size;
  document.getElementById('stat-events').textContent = state.eventCount;
}

// ─── Render loop ───────────────────────────────────────────────────────────
let lastTime = performance.now();
let frameCount = 0;
let fpsTime = 0;

function animate(now) {
  requestAnimationFrame(animate);

  const dt = now - lastTime;
  lastTime = now;

  // FPS counter.
  frameCount++;
  fpsTime += dt;
  if (fpsTime > 1000) {
    document.getElementById('stat-fps').textContent = frameCount;
    frameCount = 0;
    fpsTime = 0;
  }

  controls.update();

  // Layout.
  if (!state.paused) {
    layoutStep();
  }

  // Node pulse animation.
  for (const [, n] of state.nodes) {
    if (n.pulseTime > 0) {
      const elapsed = now - n.pulseTime;
      if (elapsed < 600) {
        const t = elapsed / 600;
        const s = 1 + Math.sin(t * Math.PI) * 0.5;
        n.mesh.scale.setScalar(s * (n.mesh.scale.x > 0.1 ? 1 : 1));
      } else {
        n.pulseTime = 0;
      }
    }
  }

  updateParticles();
  updateTimeline();

  renderer.render(scene, camera);
}

requestAnimationFrame(animate);

// ─── Resize ────────────────────────────────────────────────────────────────
window.addEventListener('resize', () => {
  camera.aspect = window.innerWidth / window.innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(window.innerWidth, window.innerHeight);
});

// ─── Keyboard shortcuts ────────────────────────────────────────────────────
document.addEventListener('keydown', (e) => {
  if (e.target.tagName === 'INPUT') return;
  switch (e.key) {
    case ' ':
      e.preventDefault();
      document.getElementById('btn-pause').click();
      break;
    case 'l':
      document.getElementById('btn-log').click();
      break;
    case 'p':
      document.getElementById('btn-prompt').click();
      break;
    case 'Escape':
      closeInspector();
      document.getElementById('prompt-panel').classList.remove('open');
      break;
    case 'ArrowLeft':
      if (state.paused && state.scrubPos > 0) scrubTo(state.scrubPos - 1);
      break;
    case 'ArrowRight':
      if (state.paused) scrubTo((state.scrubPos >= 0 ? state.scrubPos : state.events.length) + 1);
      break;
  }
});
