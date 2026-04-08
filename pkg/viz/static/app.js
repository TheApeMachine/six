import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

const EK = {
  NodeCreated:0, NodeUpdated:1, NodeRemoved:2,
  PeerAdded:3, PeerRemoved:4, PeerLatency:5,
  ValuePublished:6, ValueReplicated:7,
  GossipSent:8, GossipReceived:9,
  FieldDigest:10, EigenmodeDetected:11, FieldPressure:12,
  TrieInsert:13, TrieDecay:14, TriePrune:15,
  TriePredict:16, TrieClassify:17, TrieExperience:18,
  PoolSchedule:19, PoolComplete:20,
  AdaptiveUpdate:21,
  TrieCoupling:22, TrieMode:23, TriePressure:24, TrieSignal:25,
  BeamCollect:26, BeamCompose:27, BeamBreak:28, BeamConverge:29,
  Prompt:30, PromptResult:31,
};
const KIND_NAMES = Object.fromEntries(Object.entries(EK).map(([k,v])=>[v,k]));

function kindClass(kind) {
  if (kind <= 2) return 'c-node';
  if (kind <= 5) return 'c-peer';
  if (kind <= 7) return 'c-data';
  if (kind <= 12) return 'c-field';
  if (kind <= 18) return 'c-trie';
  if (kind <= 20) return 'c-pool';
  if (kind <= 21) return 'c-node';
  if (kind <= 25) return 'c-field';
  if (kind <= 29) return 'c-beam';
  return 'c-user';
}

const EDGE_COLORS = {
  peer: 0x4868a8,
  gossip: 0xa080e0,
  replication: 0xe06888,
  latency: 0x6ea8fe,
};

/*
Cap trie column geometry: TrieSignal/TrieMode/TriePressure indices are real
Markov trie slots. TrieCoupling must never grow this list — Go publishes
pairwise participants over digest origins there, not node.tries indices.
*/
const MAX_VIZ_TRIE_VISUALS = 64;

const state = {
  nodes: new Map(),
  edges: new Map(),
  fieldArcs: new Map(),
  eigenmodeRing: null,
  floaters: [],
  particles: [],
  edgeParticles: [],
  events: [],
  paused: false,
  scrubPos: -1,
  selected: null,
  eventCount: 0,
  droppedCount: 0,
  compute: {
    substrates: {},
    totalDispatches: 0,
    recentActions: [],
  },
  throughput: {
    buckets: new Float32Array(120),
    idx: 0,
    countThisSec: 0,
    lastSec: 0,
  },
  statsDirty: false,
};

const canvas = document.getElementById('canvas');
const renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
renderer.setSize(window.innerWidth, window.innerHeight);
renderer.setClearColor(0x101218);

const scene = new THREE.Scene();

const camera = new THREE.PerspectiveCamera(50, window.innerWidth/window.innerHeight, 0.1, 400);
camera.position.set(0, 25, 45);

const controls = new OrbitControls(camera, canvas);
controls.enableDamping = true;
controls.dampingFactor = 0.07;
controls.target.set(0, 0, 0);
controls.minDistance = 12;
controls.maxDistance = 120;
controls.maxPolarAngle = Math.PI * 0.8;

scene.add(new THREE.AmbientLight(0x606880, 1.5));
const sun = new THREE.DirectionalLight(0xd0e0ff, 0.8);
sun.position.set(20, 40, 20);
scene.add(sun);
const rim = new THREE.DirectionalLight(0x4060a0, 0.3);
rim.position.set(-20, 5, -20);
scene.add(rim);

for (let r = 10; r <= 40; r += 10) {
  const ringGeo = new THREE.RingGeometry(r - 0.03, r + 0.03, 80);
  const ringMat = new THREE.MeshBasicMaterial({ color: 0x303848, transparent: true, opacity: 0.15, side: THREE.DoubleSide });
  const ring = new THREE.Mesh(ringGeo, ringMat);
  ring.rotation.x = -Math.PI / 2;
  ring.position.y = -0.5;
  scene.add(ring);
}

const NODE_RADIUS = 16;

function nodeAngle(idx, total) {
  return (idx / Math.max(total, 1)) * Math.PI * 2;
}

function repositionNodes() {
  const total = state.nodes.size;
  let idx = 0;
  for (const [, node] of state.nodes) {
    const angle = nodeAngle(idx, total);
    node.targetPos.set(
      Math.cos(angle) * NODE_RADIUS,
      0,
      Math.sin(angle) * NODE_RADIUS
    );
    idx++;
  }
}

function createNode(id, label) {
  if (state.nodes.has(id)) return;

  const group = new THREE.Group();
  scene.add(group);

  const coreGeo = new THREE.DodecahedronGeometry(1.4, 0);
  const coreMat = new THREE.MeshPhongMaterial({
    color: 0x6ea8fe, emissive: 0x182840, shininess: 90,
    wireframe: false, transparent: true, opacity: 0.7,
  });
  const core = new THREE.Mesh(coreGeo, coreMat);
  core.userData.id = id;
  group.add(core);

  const wireGeo = new THREE.DodecahedronGeometry(1.5, 0);
  const wireMat = new THREE.MeshBasicMaterial({ color: 0x6ea8fe, wireframe: true, transparent: true, opacity: 0.25 });
  const wire = new THREE.Mesh(wireGeo, wireMat);
  group.add(wire);

  // Health pulse ring — expands/contracts with activity.
  const pulseGeo = new THREE.RingGeometry(1.8, 1.9, 32);
  const pulseMat = new THREE.MeshBasicMaterial({ color: 0x6ea8fe, transparent: true, opacity: 0.0, side: THREE.DoubleSide });
  const pulseRing = new THREE.Mesh(pulseGeo, pulseMat);
  pulseRing.rotation.x = -Math.PI / 2;
  group.add(pulseRing);

  const nameSprite = textSprite(label || id, '#6ea8fe', 22, true);
  nameSprite.position.y = 2.6;
  nameSprite.scale.set(4.5, 1.1, 1);
  group.add(nameSprite);

  // Stats panel.
  const statsCanvas = document.createElement('canvas');
  statsCanvas.width = 800;
  statsCanvas.height = 640;
  const statsTex = new THREE.CanvasTexture(statsCanvas);
  statsTex.minFilter = THREE.LinearFilter;
  statsTex.magFilter = THREE.LinearFilter;
  const statsMat = new THREE.SpriteMaterial({ map: statsTex, transparent: true, depthWrite: false });
  const statsSprite = new THREE.Sprite(statsMat);
  statsSprite.position.y = -3.4;
  statsSprite.scale.set(8, 6.4, 1);
  group.add(statsSprite);

  // Trie cluster area.
  const trieGroup = new THREE.Group();
  trieGroup.position.y = -5.5;
  group.add(trieGroup);

  const stemGeo = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(0, -1.5, 0),
    new THREE.Vector3(0, -5, 0),
  ]);
  const stem = new THREE.Line(stemGeo, new THREE.LineDashedMaterial({
    color: 0x405060, transparent: true, opacity: 0.3,
    dashSize: 0.3, gapSize: 0.2,
  }));
  stem.computeLineDistances();
  group.add(stem);

  const nodeData = {
    group, core, wire, pulseRing, trieGroup,
    statsCanvas, statsTex, statsSprite,
    targetPos: new THREE.Vector3(),
    pulseScale: 1.0,
    pulseAlpha: 0.0,
    data: {
      id, label: label || id,
      vals: {}, pressure: {}, digest: {},
      trieCount: 0,
      recentSequences: [],
      labelCounts: {},
      insertCount: 0,
      predictCount: 0,
      gossipCount: 0,
      latencies: {},
      activitySpark: new Float32Array(60),
      sparkIdx: 0,
    },
    edges: new Set(),
    tries: [],
    trieCouplings: new Map(),  // "a|b" → { mesh, coupling, glow }
    trieSignals: [],           // per-trie { surprisal, entropy, growth }
    trieModes: [],             // per-trie { aligned, modeIdx, energy }
    triePressures: [],         // per-trie { decay, learn, decayMul, learnMul }
    beam: {
      collecting: false,
      rays: [],            // { mesh, t, from:Vector3, to:Vector3 }
      hypotheses: [],      // { mesh, score, origin, angle, fade }
      breakParticles: [],  // { mesh, velocity:Vector3, life }
      convergeRing: null,  // { mesh, scale, fade }
      lastCollect: 0,
      lastCompose: 0,
      activeCount: 0,
      rejectedCount: 0,
      bestScore: 0,
    },
  };

  state.nodes.set(id, nodeData);
  repositionNodes();
  renderNodeStats(nodeData);
  updateStats();
}

function tickNodeActivity(nodeId, amount) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  node.data.activitySpark[node.data.sparkIdx % 60] += amount;
}

function advanceSparklines() {
  for (const [, node] of state.nodes) {
    node.data.sparkIdx++;
    node.data.activitySpark[node.data.sparkIdx % 60] = 0;
  }
}

const sparklineIntervalId = setInterval(advanceSparklines, 500);

window.addEventListener('beforeunload', () => {
  clearInterval(sparklineIntervalId);
});

function renderNodeStats(node) {
  const ctx = node.statsCanvas.getContext('2d');
  const w = node.statsCanvas.width;
  const h = node.statsCanvas.height;
  const d = node.data;

  ctx.clearRect(0, 0, w, h);

  ctx.fillStyle = 'rgba(14,16,22,0.82)';
  ctx.beginPath(); ctx.roundRect(0, 0, w, h, 12); ctx.fill();
  ctx.strokeStyle = 'rgba(80,110,160,0.25)';
  ctx.lineWidth = 1.5;
  ctx.beginPath(); ctx.roundRect(0, 0, w, h, 12); ctx.stroke();

  ctx.fillStyle = 'rgba(60,80,120,0.15)';
  ctx.fillRect(0, 0, w, 36);

  ctx.font = 'bold 22px monospace';
  ctx.fillStyle = '#6ea8fe';
  ctx.fillText(d.label, 14, 26);

  ctx.font = '16px monospace';
  ctx.fillStyle = '#3a4868';
  ctx.textAlign = 'right';
  ctx.fillText(d.id.substring(0, 20), w - 14, 26);
  ctx.textAlign = 'left';

  let y = 58;
  const ROW = 26;
  const COL2 = 200;
  const COL3 = 500;

  const label = (text, x) => { ctx.font = '18px monospace'; ctx.fillStyle = '#506080'; ctx.fillText(text, x || 14, y); };
  const value = (text, color, x) => { ctx.font = 'bold 18px monospace'; ctx.fillStyle = color || '#b0c8f0'; ctx.fillText(text, x || COL2, y); };

  const bar = (val, max, color, x, barW) => {
    const bx = x || COL2;
    const bw = barW || 200;
    ctx.fillStyle = 'rgba(30,38,55,0.8)';
    ctx.fillRect(bx, y - 12, bw, 10);
    const pct = Math.min(Math.abs(val) / max, 1);
    ctx.fillStyle = color;
    ctx.fillRect(bx, y - 12, bw * pct, 10);
  };

  const s = d.digest.surprisal;
  const sColor = s > 5 ? '#f06060' : s > 2 ? '#e8a840' : '#60d890';
  label('surprisal');
  if (s !== undefined) {
    value(s.toFixed(4), sColor);
    bar(s, 10, sColor, COL2 + 120, 160);
  } else { value('—', '#3a4868'); }
  y += ROW;

  const ent = d.digest.entropy;
  label('entropy');
  if (ent !== undefined) {
    value(ent.toFixed(4), '#6ea8fe');
    bar(ent, 5, '#6ea8fe', COL2 + 120, 160);
  } else { value('—', '#3a4868'); }
  y += ROW;

  const gr = d.digest.growth;
  label('growth');
  if (gr !== undefined) {
    const grColor = gr > 0 ? '#60d890' : '#e06888';
    value((gr >= 0 ? '+' : '') + gr.toFixed(4), grColor);
  } else { value('—', '#3a4868'); }
  y += ROW;

  ctx.strokeStyle = 'rgba(60,80,120,0.15)';
  ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();

  const pr = d.pressure;
  if (pr.decay !== undefined || pr.learning !== undefined) {
    label('decay');
    value(pr.decay !== undefined ? pr.decay.toFixed(6) : '—', pr.decay > 0 ? '#e06888' : '#60d890');
    label('learn', COL3);
    value(pr.learning !== undefined ? pr.learning.toFixed(6) : '—', pr.learning > 0 ? '#60d890' : '#e06888', COL3 + 100);
    y += ROW;
  }

  ctx.strokeStyle = 'rgba(60,80,120,0.15)';
  ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();

  label('tries');   value(String(d.trieCount), '#60d890');
  label('inserts', COL3); value(String(d.insertCount), '#e06888', COL3 + 100);
  y += ROW;

  label('predict'); value(String(d.predictCount), '#f0a848');
  label('gossip', COL3);  value(String(d.gossipCount), '#a080e0', COL3 + 100);
  y += ROW;

  const bm = node.beam;
  if (bm && (bm.lastCompose > 0 || bm.lastCollect > 0 || bm.activeCount > 0)) {
    ctx.strokeStyle = 'rgba(60,80,120,0.15)';
    ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();
    label('beam');
    const beamTxt = `${bm.activeCount} hyps · rej ${bm.rejectedCount} · best ${bm.bestScore.toFixed(3)}`;
    value(beamTxt, '#f0a848', COL2);
    y += ROW;
  }

  // Activity sparkline.
  ctx.strokeStyle = 'rgba(60,80,120,0.15)';
  ctx.beginPath(); ctx.moveTo(14, y - 6); ctx.lineTo(w - 14, y - 6); ctx.stroke();

  const sparkW = w - 28;
  const sparkH = 24;
  const sparkY = y;
  ctx.fillStyle = 'rgba(20,25,38,0.6)';
  ctx.fillRect(14, sparkY, sparkW, sparkH);

  let sparkMax = 1;
  for (let i = 0; i < 60; i++) {
    if (d.activitySpark[i] > sparkMax) sparkMax = d.activitySpark[i];
  }

  ctx.beginPath();
  ctx.strokeStyle = '#6ea8fe';
  ctx.lineWidth = 1.5;
  for (let i = 0; i < 60; i++) {
    const si = (d.sparkIdx + 1 + i) % 60;
    const sx = 14 + (i / 59) * sparkW;
    const sy = sparkY + sparkH - (d.activitySpark[si] / sparkMax) * sparkH;
    if (i === 0) ctx.moveTo(sx, sy); else ctx.lineTo(sx, sy);
  }
  ctx.stroke();
  ctx.lineWidth = 1;

  ctx.font = '12px monospace';
  ctx.fillStyle = '#3a4868';
  ctx.fillText('activity', 16, sparkY + 11);
  y += sparkH + 8;

  // Label distribution.
  const labels = Object.entries(d.labelCounts).sort((a,b) => b[1]-a[1]).slice(0, 4);
  if (labels.length) {
    ctx.strokeStyle = 'rgba(60,80,120,0.15)';
    ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();

    const total = labels.reduce((s, [,v]) => s + v, 0) || 1;
    const barStart = 14;
    const barTotal = w - 28;
    const colors = ['#a080e0', '#6ea8fe', '#60d890', '#e8a840'];

    for (let i = 0; i < labels.length; i++) {
      const [lbl, cnt] = labels[i];
      const pct = cnt / total;
      const bw = barTotal * pct;
      ctx.fillStyle = colors[i % colors.length];
      ctx.globalAlpha = 0.35;
      ctx.fillRect(barStart + barTotal * (labels.slice(0, i).reduce((s,[,v]) => s + v/total, 0)), y - 10, bw, 16);
      ctx.globalAlpha = 1;
      ctx.font = '14px monospace';
      ctx.fillStyle = colors[i % colors.length];
      const labelX = barStart + barTotal * (labels.slice(0, i).reduce((s,[,v]) => s + v/total, 0)) + 4;
      ctx.fillText(`${lbl} ${cnt}`, labelX, y + 2);
    }
    y += ROW;
  }

  // Recent sequences (longer lines — matches expanded TrieInsert telemetry).
  const seqs = d.recentSequences.slice(-5).reverse();
  if (seqs.length) {
    ctx.strokeStyle = 'rgba(60,80,120,0.15)';
    ctx.beginPath(); ctx.moveTo(14, y - 6); ctx.lineTo(w - 14, y - 6); ctx.stroke();

    ctx.font = '14px monospace';
    const seqMaxPx = w - 28;
    for (const seq of seqs) {
      ctx.fillStyle = '#407858';
      let display = seq;
      ctx.font = '14px monospace';
      while (display.length > 8 && ctx.measureText(display).width > seqMaxPx) {
        display = `${display.slice(0, display.length - 4)}…`;
      }
      ctx.fillText(display, 14, y + 8);
      y += 18;
    }
  }

  node.statsTex.needsUpdate = true;
}

function addTrieVisual(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  if (node.tries.length >= MAX_VIZ_TRIE_VISUALS) return;

  const idx = node.tries.length;
  const spread = 1.8;
  const offset = (idx - (node.data.trieCount - 1) / 2) * spread;

  const trieGroup = new THREE.Group();
  trieGroup.position.x = offset;

  // Trie represented as a small icosahedron cluster.
  const trunkGeo = new THREE.IcosahedronGeometry(0.3, 0);
  const trunkMat = new THREE.MeshPhongMaterial({
    color: 0x60d890, emissive: 0x102818, transparent: true, opacity: 0.7,
  });
  const trunk = new THREE.Mesh(trunkGeo, trunkMat);
  trieGroup.add(trunk);

  const branches = new THREE.Group();
  branches.position.y = 0.4;
  for (let b = 0; b < 4; b++) {
    const angle = (b / 4) * Math.PI * 2;
    const r = 0.35;
    const leafGeo = new THREE.OctahedronGeometry(0.08, 0);
    const leaf = new THREE.Mesh(leafGeo, new THREE.MeshBasicMaterial({ color: 0x80f0b0, transparent: true, opacity: 0.6 }));
    leaf.position.set(Math.cos(angle) * r, 0.15 + Math.random() * 0.2, Math.sin(angle) * r);
    branches.add(leaf);

    const lineGeo = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(0, -0.4, 0),
      leaf.position.clone(),
    ]);
    const line = new THREE.Line(lineGeo, new THREE.LineBasicMaterial({ color: 0x408060, transparent: true, opacity: 0.3 }));
    branches.add(line);
  }
  trieGroup.add(branches);

  const label = textSprite(`T${idx}`, '#60d890', 12);
  label.position.y = -0.6;
  label.scale.set(1.5, 0.4, 1);
  trieGroup.add(label);

  node.trieGroup.add(trieGroup);
  node.tries.push({ group: trieGroup, branches, trunk, insertFlash: 0 });
  updateStats();
}

/*
Intra-node trie coupling arcs: thin colored lines between tries under
the same node, showing how strongly each pair's affinity vectors correlate.
*/
function updateTrieCouplingArc(nodeId, trieA, trieB, coupling) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  if (trieA >= node.tries.length || trieB >= node.tries.length) return;

  const aid = `${Math.min(trieA,trieB)}|${Math.max(trieA,trieB)}`;
  const existing = node.trieCouplings.get(aid);

  if (existing) {
    existing.coupling = coupling;
    existing.glow = 0.8;
    return;
  }

  const tA = node.tries[trieA];
  const tB = node.tries[trieB];
  if (!tA || !tB) return;

  const pA = tA.group.position.clone();
  const pB = tB.group.position.clone();
  const mid = pA.clone().add(pB).multiplyScalar(0.5);
  mid.y -= 0.5;

  const curve = new THREE.QuadraticBezierCurve3(pA, mid, pB);
  const geo = new THREE.TubeGeometry(curve, 8, 0.015 + coupling * 0.02, 3, false);
  const intensity = Math.min(coupling * 0.8, 0.6);
  const mat = new THREE.MeshBasicMaterial({
    color: 0xe8a840, transparent: true, opacity: intensity,
  });
  const mesh = new THREE.Mesh(geo, mat);
  node.trieGroup.add(mesh);

  node.trieCouplings.set(aid, { mesh, coupling, glow: 0.8 });
}

/*
Color a trie based on its eigenmode alignment and pressure state.
Aligned tries glow warm (amber); misaligned fade cool (blue-grey).
Pressure intensity modulates brightness.
*/
function updateTrieAppearance(nodeId, trieIdx) {
  const node = state.nodes.get(nodeId);
  if (!node || trieIdx >= node.tries.length) return;

  const trie = node.tries[trieIdx];
  const mode = node.trieModes[trieIdx];
  const pressure = node.triePressures[trieIdx];
  const signal = node.trieSignals[trieIdx];

  if (!trie || !mode) return;

  // Aligned = warm amber, misaligned = cool steel.
  const baseColor = mode.aligned ? 0xf0a848 : 0x607090;
  const emissiveColor = mode.aligned ? 0x402810 : 0x101820;
  trie.trunk.material.color.setHex(baseColor);
  trie.trunk.material.emissive.setHex(emissiveColor);

  // Scale trunk by pressure magnitude.
  if (pressure) {
    const pressureMag = Math.abs(pressure.decay) + Math.abs(pressure.learn);
    const scale = 1.0 + Math.min(pressureMag * 2, 0.8);
    trie.trunk.scale.setScalar(scale);
  }

  // Surprisal drives opacity — high surprisal = more opaque/visible.
  if (signal) {
    const opacity = 0.4 + Math.min(signal.surprisal / 8, 0.6);
    trie.trunk.material.opacity = opacity;
  }
}

function addEdge(fromId, toId) {
  const eid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  if (state.edges.has(eid)) return;

  const nA = state.nodes.get(fromId);
  const nB = state.nodes.get(toId);
  if (!nA || !nB) return;

  const curve = new THREE.QuadraticBezierCurve3(
    nA.group.position.clone(),
    new THREE.Vector3(0, 3, 0),
    nB.group.position.clone(),
  );
  const geo = new THREE.TubeGeometry(curve, 20, 0.04, 4, false);
  const mat = new THREE.MeshBasicMaterial({ color: EDGE_COLORS.peer, transparent: true, opacity: 0.2 });
  const mesh = new THREE.Mesh(geo, mat);
  scene.add(mesh);

  // Edge label sprite (shows latency, flow count).
  const labelSprite = textSprite('', '#6878a0', 11);
  labelSprite.scale.set(3, 0.6, 1);
  labelSprite.visible = false;
  scene.add(labelSprite);

  state.edges.set(eid, {
    mesh, from: fromId, to: toId, activity: 0,
    labelSprite,
    latencyMs: 0,
    gossipCount: 0,
    replicationCount: 0,
    lastFlowType: 'peer',
  });
  nA.edges.add(eid);
  nB.edges.add(eid);
  updateStats();
}

/*
Field arcs — the height now scales proportionally to the distance between nodes
so they never form spikes when nodes are close together.
*/
function updateFieldArc(fromId, toId, coupling) {
  const aid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const nA = state.nodes.get(fromId);
  const nB = state.nodes.get(toId);
  if (!nA || !nB) return;

  const arc = state.fieldArcs.get(aid);
  if (arc) {
    arc.coupling = coupling;
    return;
  }

  const dist = nA.group.position.distanceTo(nB.group.position);
  const arcHeight = Math.max(1.5, dist * 0.25 + coupling * 1.5);

  const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
  mid.y += arcHeight;
  const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
  const geo = new THREE.TubeGeometry(curve, 24, 0.02 + coupling * 0.03, 4, false);
  const mat = new THREE.MeshBasicMaterial({
    color: 0xf0a848, transparent: true, opacity: Math.min(coupling * 0.5, 0.4),
  });
  const mesh = new THREE.Mesh(geo, mat);
  scene.add(mesh);

  state.fieldArcs.set(aid, { mesh, from: fromId, to: toId, coupling, glow: 0 });
}

function pulseFieldArc(fromId, toId) {
  const aid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const arc = state.fieldArcs.get(aid);
  if (arc) arc.glow = 1.0;
}

function showEigenmodeCluster(nodeId, modeCount, dominantEnergy) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  node.data.eigenmode = { modeCount, dominantEnergy, flash: 1.0 };
  rebuildEigenmodeRing();
}

function rebuildEigenmodeRing() {
  if (state.eigenmodeRing) {
    scene.remove(state.eigenmodeRing);
    state.eigenmodeRing.traverse(c => { if (c.geometry) c.geometry.dispose(); if (c.material) c.material.dispose(); });
    state.eigenmodeRing = null;
  }

  const eigenNodes = [...state.nodes.values()].filter(n => n.data.eigenmode && n.data.eigenmode.dominantEnergy > 0);
  if (eigenNodes.length < 2) return;

  const group = new THREE.Group();

  for (let i = 0; i < eigenNodes.length; i++) {
    for (let j = i + 1; j < eigenNodes.length; j++) {
      const pA = eigenNodes[i].group.position;
      const pB = eigenNodes[j].group.position;
      const dist = pA.distanceTo(pB);
      const arcHeight = Math.max(2, dist * 0.3);

      const mid = pA.clone().add(pB).multiplyScalar(0.5);
      mid.y += arcHeight;
      const curve = new THREE.QuadraticBezierCurve3(pA.clone(), mid, pB.clone());
      const geo = new THREE.TubeGeometry(curve, 16, 0.03, 3, false);
      const mat = new THREE.MeshBasicMaterial({
        color: 0xf0a848, transparent: true, opacity: 0.2,
      });
      group.add(new THREE.Mesh(geo, mat));
    }
  }

  scene.add(group);
  state.eigenmodeRing = group;
}

// --- Edge particle system: shows directional data flow along edges ---

function spawnEdgeParticle(fromId, toId, color) {
  const nA = state.nodes.get(fromId);
  const nB = state.nodes.get(toId);
  if (!nA || !nB) return;

  const geo = new THREE.SphereGeometry(0.08, 6, 6);
  const mat = new THREE.MeshBasicMaterial({ color: color || 0xa080e0, transparent: true });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.position.copy(nA.group.position);
  scene.add(mesh);

  // Trail effect — small fading spheres behind the main particle.
  const trail = [];
  for (let i = 0; i < 4; i++) {
    const tGeo = new THREE.SphereGeometry(0.04 - i * 0.008, 4, 4);
    const tMat = new THREE.MeshBasicMaterial({ color: color || 0xa080e0, transparent: true, opacity: 0.4 - i * 0.1 });
    const tMesh = new THREE.Mesh(tGeo, tMat);
    tMesh.position.copy(nA.group.position);
    scene.add(tMesh);
    trail.push(tMesh);
  }

  state.edgeParticles.push({
    mesh, trail,
    from: fromId, to: toId,
    t: 0, speed: 0.012 + Math.random() * 0.008,
  });
}

// --- Compute resource panel (bottom-left HUD overlay) ---

const computePanel = document.createElement('div');
computePanel.id = 'compute-panel';
computePanel.style.cssText = 'position:fixed;bottom:36px;left:0;width:320px;background:rgba(16,18,24,0.92);border-right:1px solid rgba(80,100,140,0.15);border-top:1px solid rgba(80,100,140,0.15);backdrop-filter:blur(12px);padding:10px 14px;font-size:11px;z-index:10;pointer-events:auto;';
document.getElementById('hud').appendChild(computePanel);

function renderComputePanel() {
  const c = state.compute;
  let html = '<div style="font-size:13px;font-weight:700;color:#d0a060;margin-bottom:6px;">Compute Backend</div>';

  const substrates = Object.entries(c.substrates);
  if (substrates.length === 0) {
    html += '<div style="color:#4a5878;">no dispatches yet</div>';
  } else {
    for (const [name, s] of substrates) {
      const barPct = Math.min(s.inflight / 8, 1) * 100;
      const color = name === 'cuda' ? '#76b900' : name === 'metal' ? '#a080e0' : '#6ea8fe';
      html += `<div style="margin-bottom:6px;">`;
      html += `<div style="display:flex;justify-content:space-between;"><span style="color:${color};font-weight:600;text-transform:uppercase;">${name}</span><span style="color:#6878a0;">dispatches: <b style="color:#a0b0d0;">${s.totalDispatches}</b></span></div>`;
      html += `<div style="display:flex;gap:12px;color:#6878a0;">`;
      html += `<span>inflight: <b style="color:#a0c0ff;">${s.inflight}</b></span>`;
      html += `<span>last: <b style="color:#a0c0ff;">${s.lastDurationMs}ms</b></span>`;
      html += `<span>ema: <b style="color:#a0c0ff;">${s.emaDurationMs.toFixed(1)}ms</b></span>`;
      html += `</div>`;
      html += `<div style="height:3px;background:rgba(30,35,50,0.8);border-radius:2px;margin-top:2px;"><div style="height:100%;width:${barPct}%;background:${color};border-radius:2px;"></div></div>`;
      html += `</div>`;
    }
  }

  html += `<div style="color:#4a5878;margin-top:4px;">total: ${c.totalDispatches}</div>`;

  if (c.recentActions.length) {
    html += '<div style="margin-top:6px;border-top:1px solid rgba(60,80,120,0.15);padding-top:4px;color:#4a5878;font-size:10px;">';
    for (const a of c.recentActions.slice(-5)) {
      html += `<div>${a}</div>`;
    }
    html += '</div>';
  }

  computePanel.innerHTML = html;
}

renderComputePanel();

// --- Throughput chart (top-right mini sparkline) ---

const throughputCanvas = document.createElement('canvas');
throughputCanvas.width = 240;
throughputCanvas.height = 40;
throughputCanvas.style.cssText = 'position:fixed;top:40px;right:390px;z-index:10;pointer-events:none;';
document.getElementById('hud').appendChild(throughputCanvas);

function renderThroughputChart() {
  const ctx = throughputCanvas.getContext('2d');
  const w = throughputCanvas.width;
  const h = throughputCanvas.height;
  const tp = state.throughput;

  ctx.clearRect(0, 0, w, h);
  ctx.fillStyle = 'rgba(14,16,22,0.7)';
  ctx.fillRect(0, 0, w, h);

  let max = 1;
  for (let i = 0; i < 120; i++) { if (tp.buckets[i] > max) max = tp.buckets[i]; }

  ctx.beginPath();
  ctx.strokeStyle = '#4868a8';
  ctx.lineWidth = 1;
  for (let i = 0; i < 120; i++) {
    const bi = (tp.idx + 1 + i) % 120;
    const x = (i / 119) * w;
    const y = h - (tp.buckets[bi] / max) * (h - 4);
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  }
  ctx.stroke();

  ctx.lineTo(w, h);
  ctx.lineTo(0, h);
  ctx.closePath();
  ctx.fillStyle = 'rgba(72,104,168,0.1)';
  ctx.fill();

  ctx.font = '9px monospace';
  ctx.fillStyle = '#4a5878';
  ctx.fillText(`${tp.buckets[tp.idx]|0} evt/s`, 4, 10);
}

function tickThroughput() {
  const now = Math.floor(Date.now() / 1000);
  const tp = state.throughput;

  if (now !== tp.lastSec) {
    tp.idx = (tp.idx + 1) % 120;
    tp.buckets[tp.idx] = tp.countThisSec;
    tp.countThisSec = 0;
    tp.lastSec = now;
    renderThroughputChart();
  }

  tp.countThisSec++;
}

function pulseEdge(fromId, toId, color) {
  const eid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const edge = state.edges.get(eid);
  if (!edge) return;
  edge.activity = 1.0;
  edge.mesh.material.color.setHex(color || 0xa080e0);
}

function spawnFloater(position, text, color, direction) {
  const sprite = textSprite(text, color || '#c0d0e0', 14);
  sprite.position.copy(position);
  sprite.scale.set(4, 1, 1);
  scene.add(sprite);

  const dir = direction || new THREE.Vector3(
    (Math.random() - 0.5) * 0.02,
    0.015 + Math.random() * 0.01,
    (Math.random() - 0.5) * 0.02,
  );

  state.floaters.push({ sprite, velocity: dir, life: 1.0, decay: 0.008 + Math.random() * 0.004 });
}

function textMaterialFromText(text, color, fontSize, bold) {
  const scale = 2;
  const fs = (fontSize || 16) * scale;
  const c = document.createElement('canvas');
  const ctx = c.getContext('2d');
  c.width = 1024; c.height = 64 * scale;
  ctx.font = `${bold ? 'bold ' : ''}${fs}px monospace`;
  ctx.fillStyle = color || '#a0b0d0';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(text.substring(0, 56), c.width / 2, c.height / 2);
  const tex = new THREE.CanvasTexture(c);
  tex.minFilter = THREE.LinearFilter;
  tex.magFilter = THREE.LinearFilter;
  return new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false });
}

function textSprite(text, color, fontSize, bold) {
  return new THREE.Sprite(textMaterialFromText(text, color, fontSize, bold));
}

function updateEdgeLabel(edge) {
  const nA = state.nodes.get(edge.from);
  const nB = state.nodes.get(edge.to);
  if (!nA || !nB) return;

  const parts = [];
  if (edge.latencyMs > 0) parts.push(`${edge.latencyMs.toFixed(1)}ms`);
  if (edge.gossipCount > 0) parts.push(`g:${edge.gossipCount}`);
  if (edge.replicationCount > 0) parts.push(`r:${edge.replicationCount}`);

  if (parts.length === 0) {
    edge.labelSprite.visible = false;
    return;
  }

  const text = parts.join(' ');
  const spr = edge.labelSprite;
  if (spr.material.map) spr.material.map.dispose();
  spr.material.dispose();

  spr.material = textMaterialFromText(text, '#7888a8', 10);

  const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
  mid.y += 2.5;
  spr.position.copy(mid);
  spr.visible = true;
}

function pulseNode(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  node.pulseAlpha = 0.6;
  node.pulseScale = 1.0;
}

/*
The timeline ring on the server only keeps the last N events. After heavy
TrieCoupling/field traffic, replay for a new browser tab may no longer
contain NodeCreated / PeerAdded, so every handler that does
state.nodes.get(ev.src) would no-op. Lazily materialize kadabra nodes from
their stable id prefix so the scene can render.
*/
function normalizeVizEvent(ev) {
  if (!ev || typeof ev !== 'object' || ev.action !== undefined) return;
  ev.kind = Number(ev.kind);
}

function ensureTopologyForEvent(ev) {
  if (ev == null || ev.kind === undefined) return;
  if (ev.kind === EK.NodeCreated || ev.kind === EK.NodeRemoved) return;

  const stub = (id) => {
    if (typeof id !== 'string' || !id.startsWith('node_')) return;
    if (!state.nodes.has(id)) createNode(id, id);
  };

  stub(ev.src);
  stub(ev.tgt);
}

/*
Trie field events reference trie_idx for real columns. TrieCoupling uses
different indices server-side; never allocate tries from it (see top comment).
*/
function ensureTrieCapacityForEvent(ev) {
  if (ev == null || ev.kind === undefined) return;

  const nodeId = ev.src;
  if (typeof nodeId !== 'string' || !nodeId.startsWith('node_')) return;

  let maxIdx = -1;
  switch (ev.kind) {
    case EK.TrieMode:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    case EK.TriePressure:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    case EK.TrieSignal:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    default:
      return;
  }

  if (maxIdx < 0) return;

  maxIdx = Math.min(maxIdx, MAX_VIZ_TRIE_VISUALS - 1);

  const node = state.nodes.get(nodeId);
  if (!node) return;

  while (node.tries.length <= maxIdx && node.tries.length < MAX_VIZ_TRIE_VISUALS) {
    node.data.trieCount = node.tries.length + 1;
    addTrieVisual(nodeId);
  }
}

function replayEvent(ev) {
  normalizeVizEvent(ev);
  try {
    ensureTopologyForEvent(ev);
    ensureTrieCapacityForEvent(ev);
    applyEvent(ev);
  } catch (err) {
    console.warn('viz: replayEvent', err);
  }
}

function handleEvent(ev) {
  state.events.push(ev);
  state.eventCount++;
  tickThroughput();
  state.statsDirty = true;
  if (state.paused && state.scrubPos >= 0) return;
  replayEvent(ev);
  addLogEntry(ev);
}

function applyEvent(ev) {
  switch (ev.kind) {
    case EK.NodeCreated:
      createNode(ev.src, ev.lbl);
      break;

    case EK.NodeUpdated: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      if (ev.vals?.trie_count !== undefined) {
        const newCount = Math.min(Math.floor(ev.vals.trie_count), MAX_VIZ_TRIE_VISUALS);
        for (let i = node.data.trieCount; i < newCount; i++) addTrieVisual(ev.src);
        node.data.trieCount = newCount;
        renderNodeStats(node);
      }
      Object.assign(node.data.vals, ev.vals || {});
      break;
    }

    case EK.NodeRemoved: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      scene.remove(node.group);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (e) {
          scene.remove(e.mesh);
          scene.remove(e.labelSprite);
          state.edges.delete(eid);
        }
      }
      state.nodes.delete(ev.src);
      repositionNodes();
      updateStats();
      break;
    }

    case EK.PeerAdded:
      if (!state.nodes.has(ev.src)) createNode(ev.src, ev.src);
      if (!state.nodes.has(ev.tgt)) createNode(ev.tgt, ev.tgt);
      addEdge(ev.src, ev.tgt);
      break;

    case EK.PeerLatency: {
      pulseEdge(ev.src, ev.tgt, EDGE_COLORS.latency);
      const eid = ev.src < ev.tgt ? `${ev.src}|${ev.tgt}` : `${ev.tgt}|${ev.src}`;
      const edge = state.edges.get(eid);
      if (edge) {
        edge.latencyMs = ev.vals?.latency_ms || 0;
        updateEdgeLabel(edge);
      }
      const nA = state.nodes.get(ev.src);
      const nB = state.nodes.get(ev.tgt);
      if (nA) nA.data.latencies[ev.tgt] = edge?.latencyMs || 0;
      if (nB) nB.data.latencies[ev.src] = edge?.latencyMs || 0;
      break;
    }

    case EK.ValuePublished: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.insertCount++;
      tickNodeActivity(ev.src, 1);
      pulseNode(ev.src);
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      const pos = node.group.position.clone();
      pos.y += 1;
      spawnFloater(pos, ev.lbl || 'publish', '#e06888');
      renderNodeStats(node);
      break;
    }

    case EK.ValueReplicated: {
      const nA = state.nodes.get(ev.src);
      const nB = state.nodes.get(ev.tgt);
      if (nA && nB) {
        spawnEdgeParticle(ev.src, ev.tgt, EDGE_COLORS.replication);
        const eid = ev.src < ev.tgt ? `${ev.src}|${ev.tgt}` : `${ev.tgt}|${ev.src}`;
        const edge = state.edges.get(eid);
        if (edge) {
          edge.replicationCount++;
          edge.lastFlowType = 'replication';
          updateEdgeLabel(edge);
        }
      }
      break;
    }

    case EK.GossipSent: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.gossipCount++;
      tickNodeActivity(ev.src, 0.5);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (!e) continue;
        const peerId = e.from === ev.src ? e.to : e.from;
        spawnEdgeParticle(ev.src, peerId, EDGE_COLORS.gossip);
        e.gossipCount++;
        e.lastFlowType = 'gossip';
        updateEdgeLabel(e);
        pulseEdge(ev.src, peerId, EDGE_COLORS.gossip);
      }
      renderNodeStats(node);
      break;
    }

    case EK.GossipReceived: {
      const nA = state.nodes.get(ev.tgt);
      const nB = state.nodes.get(ev.src);
      if (nA && nB) {
        pulseEdge(ev.src, ev.tgt, EDGE_COLORS.gossip);
        spawnEdgeParticle(ev.src, ev.tgt, EDGE_COLORS.gossip);
        tickNodeActivity(ev.tgt, 0.5);
        pulseNode(ev.tgt);
      }
      break;
    }

    case EK.FieldDigest: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.digest = ev.vals || {};
      const s = ev.vals?.surprisal || 0;
      const t = Math.min(s / 8, 1);
      node.core.material.color.setHex(lerpColor(0x60d890, 0xf06060, t));
      node.core.material.emissive.setHex(lerpColor(0x102818, 0x401010, t));
      node.wire.material.color.setHex(lerpColor(0x60d890, 0xf06060, t));
      node.pulseRing.material.color.setHex(lerpColor(0x60d890, 0xf06060, t));
      tickNodeActivity(ev.src, 0.3);
      renderNodeStats(node);
      break;
    }

    case EK.EigenmodeDetected: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const energy = ev.vals?.dominant_energy || 0;
      const modeCount = ev.vals?.mode_count || 0;
      if (energy > 0) {
        node.wire.material.color.setHex(0xf0a848);
        node.wire.material.opacity = 0.6;
        const pos = node.group.position.clone();
        pos.y += 3.5;
        spawnFloater(pos, `eigenmode x${modeCount} E=${energy.toFixed(2)}`, '#f0a848');
        setTimeout(() => { node.wire.material.opacity = 0.25; }, 600);
        showEigenmodeCluster(ev.src, modeCount, energy);
      }
      break;
    }

    case EK.FieldPressure: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.pressure = ev.vals || {};
      const learn = ev.vals?.learning || 0;
      const decay = ev.vals?.decay || 0;
      node.targetPos.y = Math.max(-3, Math.min(3, (learn - decay) * 3));
      const pressure = Math.abs(learn) + Math.abs(decay);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (!e) continue;
        const peerId = e.from === ev.src ? e.to : e.from;
        updateFieldArc(ev.src, peerId, Math.min(pressure * 2, 1));
        pulseFieldArc(ev.src, peerId);
      }
      renderNodeStats(node);
      break;
    }

    case EK.TrieInsert: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.insertCount++;
      tickNodeActivity(ev.src, 1);
      pulseNode(ev.src);
      const seq = ev.meta?.sequence || '';
      if (seq) {
        node.data.recentSequences.push(seq);
        if (node.data.recentSequences.length > 10) node.data.recentSequences.shift();
        const pos = node.group.position.clone();
        pos.y -= 1;
        const display = seq.length > 30 ? `${seq.slice(0, 30)}...` : seq;
        spawnFloater(pos, display, '#60d890', new THREE.Vector3(0, -0.02, 0));
      }
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      if (node.tries.length > 0) {
        const trie = node.tries[Math.floor(Math.random() * node.tries.length)];
        trie.insertFlash = 1.0;
        if (trie.branches.children.length < 40) {
          const angle = Math.random() * Math.PI * 2;
          const r = 0.2 + Math.random() * 0.35;
          const leafGeo = new THREE.OctahedronGeometry(0.05, 0);
          const leaf = new THREE.Mesh(leafGeo, new THREE.MeshBasicMaterial({ color: 0x80f0b0, transparent: true, opacity: 0.6 }));
          leaf.position.set(Math.cos(angle) * r, 0.2 + Math.random() * 0.5, Math.sin(angle) * r);
          trie.branches.add(leaf);
        }
      }
      renderNodeStats(node);
      break;
    }

    case EK.TriePredict:
    case EK.TrieClassify: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.predictCount++;
      tickNodeActivity(ev.src, 0.8);
      pulseNode(ev.src);
      const conf = ev.vals?.confidence;
      const txt = ev.lbl + (conf !== undefined ? ` (${(conf*100).toFixed(0)}%)` : '');
      const pos = node.group.position.clone();
      pos.y += 2;
      spawnFloater(pos, txt, '#f0a848');
      renderNodeStats(node);
      break;
    }

    case EK.TrieExperience: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const pos = node.group.position.clone();
      pos.y -= 0.5;
      spawnFloater(pos, `exp s=${(ev.vals?.surprisal||0).toFixed(2)}`, '#508068');
      tickNodeActivity(ev.src, 0.3);
      break;
    }

    case EK.AdaptiveUpdate: {
      const node = state.nodes.get(ev.src);
      if (node) {
        Object.assign(node.data.vals, ev.vals || {});
        tickNodeActivity(ev.src, 0.2);
      }
      break;
    }

    case EK.PoolSchedule: {
      const name = ev.lbl || 'unknown';
      const inflight = ev.vals?.queue_size || 0;
      if (!state.compute.substrates[name]) {
        state.compute.substrates[name] = { inflight: 0, lastDurationMs: 0, totalDispatches: 0, emaDurationMs: 0 };
      }
      state.compute.substrates[name].inflight = inflight;
      state.compute.substrates[name].totalDispatches++;
      state.compute.totalDispatches++;
      state.compute.recentActions.push(`-> ${name} (inflight:${inflight})`);
      if (state.compute.recentActions.length > 8) state.compute.recentActions.shift();
      renderComputePanel();
      break;
    }

    case EK.PoolComplete: {
      const name = ev.lbl || 'unknown';
      const durationMs = ev.vals?.duration_ms || 0;
      if (!state.compute.substrates[name]) {
        state.compute.substrates[name] = { inflight: 0, lastDurationMs: 0, totalDispatches: 0, emaDurationMs: 0 };
      }
      const s = state.compute.substrates[name];
      s.inflight = Math.max(0, s.inflight - 1);
      s.lastDurationMs = durationMs;
      s.emaDurationMs = s.emaDurationMs === 0 ? durationMs : s.emaDurationMs + (durationMs - s.emaDurationMs) * 0.125;
      state.compute.recentActions.push(`ok ${name} ${durationMs}ms`);
      if (state.compute.recentActions.length > 8) state.compute.recentActions.shift();
      renderComputePanel();
      break;
    }

    case EK.TrieCoupling: {
      // trie_a / trie_b are digest-participant indices in kadabra.Field, not
      // node.tries[] slots; only draw an arc when both already exist as visuals.
      const trieA = ev.vals?.trie_a | 0;
      const trieB = ev.vals?.trie_b | 0;
      const coupling = ev.vals?.coupling || 0;
      if (trieA < MAX_VIZ_TRIE_VISUALS && trieB < MAX_VIZ_TRIE_VISUALS) {
        updateTrieCouplingArc(ev.src, trieA, trieB, coupling);
      }
      break;
    }

    case EK.TrieMode: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const modeIdx = ev.vals?.mode_idx | 0;
      const aligned = (ev.vals?.aligned || 0) > 0.5;
      const energy = ev.vals?.energy || 0;
      while (node.trieModes.length <= trieIdx) node.trieModes.push({ aligned: false, modeIdx: -1, energy: 0 });
      node.trieModes[trieIdx] = { aligned, modeIdx, energy };
      updateTrieAppearance(ev.src, trieIdx);
      break;
    }

    case EK.TriePressure: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const decay = ev.vals?.decay || 0;
      const learn = ev.vals?.learn || 0;
      const decayMul = ev.vals?.decay_mul || 0;
      const learnMul = ev.vals?.learn_mul || 0;
      while (node.triePressures.length <= trieIdx) node.triePressures.push({ decay: 0, learn: 0, decayMul: 1, learnMul: 1 });
      node.triePressures[trieIdx] = { decay, learn, decayMul, learnMul };
      updateTrieAppearance(ev.src, trieIdx);
      tickNodeActivity(ev.src, 0.3);
      break;
    }

    case EK.TrieSignal: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const surprisal = ev.vals?.surprisal || 0;
      const entropy = ev.vals?.entropy || 0;
      const growth = ev.vals?.growth || 0;
      while (node.trieSignals.length <= trieIdx) node.trieSignals.push({ surprisal: 0, entropy: 0, growth: 0 });
      node.trieSignals[trieIdx] = { surprisal, entropy, growth };
      updateTrieAppearance(ev.src, trieIdx);
      break;
    }

    case EK.BeamCollect: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamCollect', ev.src, ev.vals);
      const trieCount = ev.vals?.trie_count || 0;
      const contCount = ev.vals?.continuation_count || 0;
      const beam = node.beam;
      beam.collecting = true;
      beam.lastCollect = performance.now();
      beam.activeCount = contCount;
      tickNodeActivity(ev.src, 2);

      // Spawn collection rays: animated tubes from each trie up to the node core.
      for (const oldRay of beam.rays) {
        node.group.remove(oldRay.mesh);
        oldRay.mesh.geometry.dispose();
        oldRay.mesh.material.dispose();
      }
      beam.rays.length = 0;

      const nodeY = 0;
      const trieBaseY = -5.5;
      const numRays = Math.min(node.tries.length, trieCount, 12);
      for (let i = 0; i < numRays; i++) {
        const trie = node.tries[i];
        if (!trie) continue;
        const from = new THREE.Vector3(trie.group.position.x, trieBaseY, trie.group.position.z);
        const to = new THREE.Vector3(0, nodeY, 0);
        const mid = from.clone().add(to).multiplyScalar(0.5);
        mid.x += (Math.random() - 0.5) * 1.5;
        mid.z += (Math.random() - 0.5) * 1.5;
        const curve = new THREE.QuadraticBezierCurve3(from, mid, to);
        const geo = new THREE.TubeGeometry(curve, 12, 0.06, 4, false);
        const mat = new THREE.MeshBasicMaterial({
          color: 0x40a0f0, transparent: true, opacity: 0.0,
        });
        const mesh = new THREE.Mesh(geo, mat);
        node.group.add(mesh);
        beam.rays.push({ mesh, t: 0, from, to, trieIdx: i });
        trie.insertFlash = 1.0;
      }

      // Also pulse the node strongly during collection.
      node.pulseAlpha = 0.8;
      node.pulseScale = 1.0;
      node.pulseRing.material.color.setHex(0x40a0f0);
      break;
    }

    case EK.BeamCompose: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamCompose', ev.src, ev.vals);
      const selected = ev.vals?.selected_count || 0;
      const rejected = ev.vals?.rejected_count || 0;
      const score = ev.vals?.best_score || 0;
      const beam = node.beam;
      beam.lastCompose = performance.now();
      beam.activeCount = selected;
      beam.rejectedCount = rejected;
      beam.bestScore = score;
      beam.collecting = false;

      // Clear old hypotheses.
      for (const hyp of beam.hypotheses) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
      }
      beam.hypotheses.length = 0;

      // Spawn hypothesis orbs orbiting the node — one per selected candidate.
      const orbitR = 3.0;
      const orbitY = 2.5;
      for (let i = 0; i < Math.min(selected, 8); i++) {
        const angle = (i / Math.min(selected, 8)) * Math.PI * 2;
        const geo = new THREE.SphereGeometry(0.25 + score * 0.15, 8, 8);
        const brightness = 0.4 + (1 - i / Math.min(selected, 8)) * 0.6;
        const mat = new THREE.MeshBasicMaterial({
          color: rejected > 0 ? 0xf0a848 : 0x60f0b0,
          transparent: true,
          opacity: brightness * 0.8,
        });
        const mesh = new THREE.Mesh(geo, mat);
        mesh.position.set(
          Math.cos(angle) * orbitR,
          orbitY,
          Math.sin(angle) * orbitR,
        );
        node.group.add(mesh);
        beam.hypotheses.push({ mesh, score: brightness, origin: i, angle, fade: 1.0 });
      }

      pulseNode(ev.src);

      // Small floater with stats.
      const pos = node.group.position.clone();
      pos.y += 3;
      const color = rejected > 0 ? '#f0a848' : '#60d890';
      spawnFloater(pos, `${selected}↑ ${rejected}✗ (${score.toFixed(2)})`, color);
      break;
    }

    case EK.BeamBreak: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamBreak', ev.src, ev.tgt, ev.vals);
      const beam = node.beam;
      tickNodeActivity(ev.src, 2);

      // Pick a trie to shatter — flash red and spawn break particles falling down.
      const trieIdx = node.tries.length > 0 ? Math.floor(Math.random() * node.tries.length) : -1;
      if (trieIdx >= 0) {
        const trie = node.tries[trieIdx];
        trie.trunk.material.color.setHex(0xf06060);
        trie.trunk.material.emissive.setHex(0x601010);
        trie.trunk.scale.setScalar(1.6);
        setTimeout(() => {
          trie.trunk.material.color.setHex(0x607090);
          trie.trunk.material.emissive.setHex(0x101820);
          trie.trunk.scale.setScalar(0.6);
        }, 600);
        setTimeout(() => { trie.trunk.scale.setScalar(1.0); }, 1200);

        // Shatter particles: small red fragments flying outward from trie.
        const trieWorldPos = new THREE.Vector3();
        trie.group.getWorldPosition(trieWorldPos);
        for (let p = 0; p < 8; p++) {
          const geo = new THREE.TetrahedronGeometry(0.06, 0);
          const mat = new THREE.MeshBasicMaterial({ color: 0xf06060, transparent: true, opacity: 0.9 });
          const mesh = new THREE.Mesh(geo, mat);
          mesh.position.copy(trieWorldPos);
          scene.add(mesh);
          beam.breakParticles.push({
            mesh,
            velocity: new THREE.Vector3(
              (Math.random() - 0.5) * 0.08,
              -0.02 - Math.random() * 0.04,
              (Math.random() - 0.5) * 0.08,
            ),
            life: 1.0,
          });
        }
      }

      // Flash a red X descending from node to trie area.
      const pos = node.group.position.clone();
      pos.y -= 2;
      spawnFloater(pos, '✗ BREAK', '#f06060', new THREE.Vector3(0, -0.025, 0));
      break;
    }

    case EK.BeamConverge: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamConverge', ev.src, ev.lbl, ev.vals);
      const seq = ev.lbl || '';
      const score = ev.vals?.score || 0;
      const beam = node.beam;

      // Clear hypotheses — they've resolved.
      for (const hyp of beam.hypotheses) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
      }
      beam.hypotheses.length = 0;

      // Convergence ring: expanding bright ring around the node.
      if (beam.convergeRing) {
        node.group.remove(beam.convergeRing.mesh);
        beam.convergeRing.mesh.geometry.dispose();
        beam.convergeRing.mesh.material.dispose();
      }
      const ringGeo = new THREE.RingGeometry(1.0, 1.4, 32);
      const ringMat = new THREE.MeshBasicMaterial({
        color: 0x60f0b0, transparent: true, opacity: 0.9,
        side: THREE.DoubleSide,
      });
      const ring = new THREE.Mesh(ringGeo, ringMat);
      ring.rotation.x = -Math.PI / 2;
      ring.position.y = 1.5;
      node.group.add(ring);
      beam.convergeRing = { mesh: ring, scale: 1.0, fade: 1.0 };

      // Bright node flash.
      node.core.material.emissive.setHex(0x30a060);
      node.core.material.color.setHex(0x60f0b0);
      setTimeout(() => {
        node.core.material.emissive.setHex(0x182840);
        node.core.material.color.setHex(0x6ea8fe);
      }, 1500);

      pulseNode(ev.src);

      // Result floater.
      const pos = node.group.position.clone();
      pos.y += 4.5;
      const display = seq.length > 40 ? `${seq.slice(0, 40)}...` : seq;
      spawnFloater(pos, `→ ${display}`, '#60f0b0');

      if (score > 0) {
        const pos2 = node.group.position.clone();
        pos2.y += 3.5;
        spawnFloater(pos2, `score: ${score.toFixed(3)}`, '#40d0a0');
      }
      break;
    }

    case EK.Prompt: {
      const pos = new THREE.Vector3(0, 6, 0);
      spawnFloater(pos, ev.meta?.prompt || 'prompt', '#80b0f0');
      break;
    }

    case EK.PromptResult: {
      const pos = new THREE.Vector3(0, 4, 0);
      spawnFloater(pos, ev.meta?.generation || 'result', '#60d890');
      break;
    }
  }

  if (state.selected && (ev.src === state.selected || ev.tgt === state.selected)) {
    refreshInspector();
  }
}

// Raycasting.
const raycaster = new THREE.Raycaster();
const mouse = new THREE.Vector2();

canvas.addEventListener('click', (e) => {
  mouse.x = (e.clientX / window.innerWidth) * 2 - 1;
  mouse.y = -(e.clientY / window.innerHeight) * 2 + 1;
  raycaster.setFromCamera(mouse, camera);

  const meshes = [...state.nodes.values()].map(n => n.core);
  const hits = raycaster.intersectObjects(meshes);
  if (hits.length > 0) {
    selectNode(hits[0].object.userData.id);
  } else {
    closeInspector();
  }
});

function selectNode(id) {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.25;
  }
  state.selected = id;
  const node = state.nodes.get(id);
  if (node) node.wire.material.opacity = 0.6;
  refreshInspector();
  document.getElementById('inspector').classList.add('open');
}

function closeInspector() {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.25;
  }
  state.selected = null;
  document.getElementById('inspector').classList.remove('open');
}

function refreshInspector() {
  const node = state.nodes.get(state.selected);
  if (!node) return;

  document.getElementById('inspector-title').textContent = node.data.label;
  const body = document.getElementById('inspector-body');
  let html = '';
  const d = node.data;

  // Digest.
  html += '<div class="insp-section"><h4>field digest</h4>';
  for (const key of ['surprisal', 'entropy', 'growth']) {
    const v = d.digest[key];
    if (v === undefined) continue;
    const pct = Math.min(Math.abs(v) / 10, 1) * 100;
    const barColor = key === 'surprisal' ? (v > 5 ? '#f06060' : v > 2 ? '#e0a040' : '#60d890') : '#6ea8fe';
    html += `<div class="insp-row"><span class="insp-key">${key}</span><span class="insp-val">${v.toFixed(4)}</span></div>`;
    html += `<div class="insp-bar"><div class="insp-bar-fill" style="width:${pct}%;background:${barColor}"></div></div>`;
  }
  html += '</div>';

  // Pressure.
  if (Object.keys(d.pressure).length) {
    html += '<div class="insp-section"><h4>field pressure</h4>';
    for (const [k, v] of Object.entries(d.pressure)) {
      html += `<div class="insp-row"><span class="insp-key">${k}</span><span class="insp-val">${typeof v === 'number' ? v.toFixed(6) : v}</span></div>`;
    }
    html += '</div>';
  }

  // Activity.
  html += '<div class="insp-section"><h4>activity</h4>';
  html += `<div class="insp-row"><span class="insp-key">inserts</span><span class="insp-val">${d.insertCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">predictions</span><span class="insp-val">${d.predictCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">gossip</span><span class="insp-val">${d.gossipCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">tries</span><span class="insp-val">${d.trieCount}</span></div>`;
  html += '</div>';

  // Peer latencies.
  const latencyEntries = Object.entries(d.latencies);
  if (latencyEntries.length) {
    html += '<div class="insp-section"><h4>peer latencies</h4>';
    for (const [peerId, ms] of latencyEntries) {
      const color = ms > 50 ? '#f06060' : ms > 10 ? '#e8a840' : '#60d890';
      html += `<div class="insp-row"><span class="insp-key">${peerId.substring(0,16)}</span><span class="insp-val" style="color:${color}">${ms.toFixed(1)}ms</span></div>`;
    }
    html += '</div>';
  }

  // Labels.
  const labels = Object.entries(d.labelCounts).sort((a,b) => b[1]-a[1]);
  if (labels.length) {
    html += '<div class="insp-section"><h4>label distribution</h4>';
    const total = labels.reduce((s, [,v]) => s + v, 0);
    for (const [label, count] of labels) {
      const pct = (count / total * 100).toFixed(1);
      html += `<div class="insp-row"><span class="insp-key">${label}</span><span class="insp-val">${count} (${pct}%)</span></div>`;
      html += `<div class="insp-bar"><div class="insp-bar-fill" style="width:${pct}%;background:#a080e0"></div></div>`;
    }
    html += '</div>';
  }

  // Recent sequences.
  if (d.recentSequences.length) {
    html += '<div class="insp-section"><h4>recent sequences</h4>';
    for (const seq of [...d.recentSequences].reverse().slice(0, 8)) {
      html += `<div class="insp-sequence">${seq}</div>`;
    }
    html += '</div>';
  }

  // Per-trie field state.
  if (node.trieSignals.length > 0 || node.trieModes.length > 0) {
    html += '<div class="insp-section"><h4>trie field state</h4>';
    const maxIdx = Math.max(node.trieSignals.length, node.trieModes.length, node.triePressures.length);
    for (let ti = 0; ti < maxIdx; ti++) {
      const sig = node.trieSignals[ti];
      const mode = node.trieModes[ti];
      const pres = node.triePressures[ti];
      const modeLabel = mode ? (mode.aligned ? '<span style="color:#f0a848">aligned</span>' : '<span style="color:#607090">misaligned</span>') : '—';
      html += `<div style="margin-bottom:4px;padding:3px 0;border-bottom:1px solid rgba(60,80,120,0.1);">`;
      html += `<div class="insp-row"><span class="insp-key">T${ti}</span><span class="insp-val">${modeLabel}</span></div>`;
      if (sig) {
        const sColor = sig.surprisal > 5 ? '#f06060' : sig.surprisal > 2 ? '#e8a840' : '#60d890';
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">surprisal</span><span class="insp-val" style="color:${sColor}">${sig.surprisal.toFixed(4)}</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">entropy</span><span class="insp-val">${sig.entropy.toFixed(4)}</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">growth</span><span class="insp-val" style="color:${sig.growth > 0 ? '#60d890' : '#e06888'}">${sig.growth >= 0 ? '+' : ''}${sig.growth.toFixed(4)}</span></div>`;
      }
      if (pres) {
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">decay</span><span class="insp-val" style="color:#e06888">${pres.decay.toFixed(6)} (x${pres.decayMul.toFixed(2)})</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">learn</span><span class="insp-val" style="color:#60d890">${pres.learn.toFixed(6)} (x${pres.learnMul.toFixed(2)})</span></div>`;
      }
      html += `</div>`;
    }
    html += '</div>';
  }

  // Beam search state.
  const bm = node.beam;
  if (bm.lastCompose > 0 || bm.activeCount > 0) {
    html += '<div class="insp-section"><h4>beam search</h4>';
    html += `<div class="insp-row"><span class="insp-key">active hypotheses</span><span class="insp-val" style="color:#60f0b0">${bm.activeCount}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">last rejected</span><span class="insp-val" style="color:#f06060">${bm.rejectedCount}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">best score</span><span class="insp-val" style="color:#f0a848">${bm.bestScore.toFixed(4)}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">collection rays</span><span class="insp-val">${bm.rays.length}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">orbiting hyps</span><span class="insp-val">${bm.hypotheses.length}</span></div>`;
    html += '</div>';
  }

  // Peers with edge stats.
  html += `<div class="insp-section"><h4>peers (${node.edges.size})</h4>`;
  for (const eid of node.edges) {
    const e = state.edges.get(eid);
    if (e) {
      const peer = e.from === state.selected ? e.to : e.from;
      const details = [];
      if (e.latencyMs > 0) details.push(`${e.latencyMs.toFixed(1)}ms`);
      if (e.gossipCount > 0) details.push(`gossip:${e.gossipCount}`);
      if (e.replicationCount > 0) details.push(`repl:${e.replicationCount}`);
      html += `<div class="insp-row"><span class="insp-key">${peer.substring(0,16)}</span><span class="insp-val">${details.join(' ')}</span></div>`;
    }
  }
  html += '</div>';

  body.innerHTML = html;
}

// Event log.
const logEl = document.getElementById('eventlog');
function addLogEntry(ev) {
  const div = document.createElement('div');
  div.className = 'log-entry';
  const ts = new Date(ev.ts / 1000).toISOString().substr(11, 12);
  const kc = kindClass(ev.kind);
  div.innerHTML = `<span class="log-time">${ts}</span><span class="log-kind ${kc}">${KIND_NAMES[ev.kind]||ev.kind}</span><span class="log-src">${ev.src}${ev.tgt ? ` > ${ev.tgt}` : ''}</span>${ev.lbl ? ` ${ev.lbl}` : ''}`;
  if (ev.meta?.sequence) {
    const meta = document.createElement('span');
    meta.className = 'log-meta';
    meta.textContent = ev.meta.sequence;
    div.appendChild(meta);
  }
  logEl.appendChild(div);
  while (logEl.children.length > 500) logEl.removeChild(logEl.firstChild);
  logEl.scrollTop = logEl.scrollHeight;
}

// Timeline.
const timelineBar = document.getElementById('timeline-bar');
const timelineFill = document.getElementById('timeline-fill');
const timelineCursor = document.getElementById('timeline-cursor');
const timelineLabel = document.getElementById('timeline-label');

function updateTimeline() {
  const total = state.events.length;
  const pos = state.scrubPos >= 0 ? state.scrubPos : total;
  const pct = total > 0 ? (pos / total) * 100 : 0;
  timelineFill.style.width = `${pct}%`;
  timelineCursor.style.left = `${pct}%`;
  timelineLabel.textContent = `${pos} / ${total}`;
}

timelineBar.addEventListener('click', (e) => {
  if (!state.paused) return;
  const rect = timelineBar.getBoundingClientRect();
  scrubTo(Math.floor(((e.clientX - rect.left) / rect.width) * state.events.length));
});

function scrubTo(pos) {
  pos = Math.max(0, Math.min(pos, state.events.length));
  state.scrubPos = pos;
  clearScene();
  for (let i = 0; i < pos; i++) replayEvent(state.events[i]);
  updateTimeline();
  state.statsDirty = true;
}

function clearScene() {
  for (const [, n] of state.nodes) {
    // Clean up beam particles in world space.
    for (const bp of n.beam.breakParticles) { scene.remove(bp.mesh); bp.mesh.geometry.dispose(); bp.mesh.material.dispose(); }
    n.beam.breakParticles.length = 0;
    scene.remove(n.group);
  }
  for (const [, e] of state.edges) { scene.remove(e.mesh); scene.remove(e.labelSprite); }
  for (const [, a] of state.fieldArcs) { scene.remove(a.mesh); a.mesh.geometry.dispose(); a.mesh.material.dispose(); }
  if (state.eigenmodeRing) { scene.remove(state.eigenmodeRing); state.eigenmodeRing = null; }
  for (const f of state.floaters) scene.remove(f.sprite);
  for (const p of state.particles) scene.remove(p.mesh);
  for (const ep of state.edgeParticles) { scene.remove(ep.mesh); ep.trail.forEach(t => {scene.remove(t)}); }
  state.nodes.clear();
  state.edges.clear();
  state.fieldArcs.clear();
  state.floaters.length = 0;
  state.particles.length = 0;
  state.edgeParticles.length = 0;
}

// Buttons.
document.getElementById('btn-pause').addEventListener('click', function() {
  state.paused = !state.paused;
  this.textContent = state.paused ? 'resume' : 'pause';
  this.classList.toggle('active', state.paused);
  if (!state.paused) { state.scrubPos = -1; clearScene(); for (const ev of state.events) replayEvent(ev); state.statsDirty = true; }
});
document.getElementById('btn-log').addEventListener('click', function() { logEl.classList.toggle('open'); this.classList.toggle('active'); });
document.getElementById('btn-prompt').addEventListener('click', function() {
  const p = document.getElementById('prompt-panel'); p.classList.toggle('open'); this.classList.toggle('active');
  if (p.classList.contains('open')) document.getElementById('prompt-input').focus();
});
document.getElementById('btn-close-inspector').addEventListener('click', closeInspector);
document.getElementById('btn-tl-start').addEventListener('click', () => { if (state.paused) scrubTo(0); });
document.getElementById('btn-tl-end').addEventListener('click', () => { if (state.paused) scrubTo(state.events.length); });
document.getElementById('btn-save').addEventListener('click', () => {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob([JSON.stringify(state.events)], {type:'application/json'}));
  a.download = `six-viz-${Date.now()}.json`; a.click();
});
document.getElementById('btn-load').addEventListener('click', () => {
  const input = document.createElement('input'); input.type = 'file'; input.accept = '.json';
  input.onchange = async (e) => {
    const events = JSON.parse(await e.target.files[0].text());
    state.events = events; state.eventCount = events.length; state.paused = true;
    document.getElementById('btn-pause').textContent = 'resume';
    document.getElementById('btn-pause').classList.add('active');
    scrubTo(events.length);
  };
  input.click();
});
document.getElementById('prompt-input').addEventListener('keydown', async (e) => {
  if (e.key !== 'Enter') return;
  const prompt = e.target.value.trim(); if (!prompt) return;
  e.target.value = '';
  const r = document.getElementById('prompt-result'); r.textContent = 'Sending...';
  try {
    const resp = await (await fetch('/api/prompt', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({prompt}) })).json();
    let text = `Generation: ${resp.generation||'(empty)'}\n\nClassification:\n`;
    if (resp.classification) for (const [k,v] of Object.entries(resp.classification)) text += `  ${k}: ${v.toFixed(2)}%\n`;
    r.textContent = text;
  } catch(err) { r.textContent = `Error: ${err.message}`; }
});

// WebSocket.
let ws = null, reconnectTimer = null;
function connect() {
  const url = `${location.protocol==='https:'?'wss:':'ws:'}//${location.host}/ws`;
  ws = new WebSocket(url);
  ws.onopen = () => { document.title = 'Six — Connected'; };
  ws.onmessage = (msg) => {
    try {
      const ev = JSON.parse(msg.data);
      if (ev.action) { handleServerResponse(ev); return; }
      normalizeVizEvent(ev);
      handleEvent(ev);
    } catch(e) { console.error('viz: event parse error', e); }
  };
  ws.onclose = () => { document.title = 'Six — Disconnected'; clearTimeout(reconnectTimer); reconnectTimer = setTimeout(connect, 2000); };
  ws.onerror = () => { ws.close(); };
}
function handleServerResponse(resp) {
  if (resp.action === 'bootstrap' && Array.isArray(resp.nodes)) {
    for (const id of resp.nodes) {
      if (typeof id === 'string' && id.startsWith('node_')) createNode(id, id);
    }
    state.statsDirty = true;
  } else if (resp.action === 'scrub_result' && resp.events) { clearScene(); for (const ev of resp.events) replayEvent(ev); state.statsDirty = true; }
  else if (resp.action === 'stats') { state.droppedCount = resp.dropped || 0; updateStats(); }
}
connect();

function lerpColor(a, b, t) {
  const ar=(a>>16)&0xff, ag=(a>>8)&0xff, ab=a&0xff;
  const br=(b>>16)&0xff, bg=(b>>8)&0xff, bb=b&0xff;
  return (Math.round(ar+(br-ar)*t)<<16)|(Math.round(ag+(bg-ag)*t)<<8)|Math.round(ab+(bb-ab)*t);
}

function updateStats() {
  document.getElementById('stat-nodes').textContent = state.nodes.size;
  document.getElementById('stat-tries').textContent = [...state.nodes.values()].reduce((s,n) => s+n.tries.length, 0);
  document.getElementById('stat-edges').textContent = state.edges.size;
  document.getElementById('stat-events').textContent = state.eventCount;
  document.getElementById('stat-dropped').textContent = state.droppedCount;
}

// --- Render loop ---
let lastTime = performance.now(), frameCount = 0, fpsTime = 0;
let statsRedrawTimer = 0;

function animate(now) {
  requestAnimationFrame(animate);
  const dt = now - lastTime; lastTime = now;
  frameCount++; fpsTime += dt;
  if (fpsTime > 1000) { document.getElementById('stat-fps').textContent = frameCount; frameCount = 0; fpsTime = 0; }

  if (state.statsDirty) {
    state.statsDirty = false;
    updateStats();
  }

  controls.update();

  // Smooth node positioning + pulse ring animation.
  for (const [, node] of state.nodes) {
    node.group.position.lerp(node.targetPos, 0.04);
    node.core.rotation.y += 0.004;
    node.core.rotation.x += 0.001;
    node.wire.rotation.y += 0.004;
    node.wire.rotation.x += 0.001;

    // Pulse ring expands and fades.
    if (node.pulseAlpha > 0.01) {
      node.pulseScale += 0.03;
      node.pulseAlpha *= 0.95;
      node.pulseRing.material.opacity = node.pulseAlpha;
      node.pulseRing.scale.setScalar(node.pulseScale);
    } else {
      node.pulseRing.material.opacity = 0;
      node.pulseScale = 1.0;
      node.pulseRing.scale.setScalar(1.0);
    }

    // Trie insert flash.
    for (const trie of node.tries) {
      if (trie.insertFlash > 0.01) {
        trie.insertFlash *= 0.93;
        trie.trunk.material.emissive.setHex(lerpColor(0x102818, 0x40f080, trie.insertFlash));
      }
    }

    // Trie coupling arc glow fade.
    for (const [, arc] of node.trieCouplings) {
      if (arc.glow > 0.01) {
        arc.glow *= 0.96;
        arc.mesh.material.opacity = Math.min(arc.coupling * 0.6 + arc.glow * 0.3, 0.7);
        arc.mesh.material.color.setHex(arc.glow > 0.4 ? 0xf0d080 : 0xe8a840);
      } else {
        arc.mesh.material.opacity = Math.max(arc.mesh.material.opacity - 0.003, Math.min(arc.coupling * 0.3, 0.25));
      }
    }

    // Beam collection rays: animate opacity sweep from trie to node.
    const beam = node.beam;
    for (let ri = beam.rays.length - 1; ri >= 0; ri--) {
      const ray = beam.rays[ri];
      ray.t += 0.025;
      if (ray.t < 1.0) {
        ray.mesh.material.opacity = Math.sin(ray.t * Math.PI) * 0.7;
        ray.mesh.material.color.setHex(ray.t > 0.5 ? 0x60c0f0 : 0x40a0f0);
      } else {
        node.group.remove(ray.mesh);
        ray.mesh.geometry.dispose();
        ray.mesh.material.dispose();
        beam.rays.splice(ri, 1);
      }
    }

    // Beam hypotheses: orbit around node, slowly fade.
    for (let hi = beam.hypotheses.length - 1; hi >= 0; hi--) {
      const hyp = beam.hypotheses[hi];
      hyp.angle += 0.02;
      hyp.fade -= 0.0008;
      const orbitR = 2.2;
      hyp.mesh.position.set(
        Math.cos(hyp.angle) * orbitR,
        1.8 + Math.sin(hyp.angle * 2) * 0.3,
        Math.sin(hyp.angle) * orbitR,
      );
      hyp.mesh.material.opacity = Math.max(0, hyp.score * hyp.fade * 0.8);
      if (hyp.fade <= 0) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
        beam.hypotheses.splice(hi, 1);
      }
    }

    // Beam break particles: fly outward and fade.
    for (let bi = beam.breakParticles.length - 1; bi >= 0; bi--) {
      const bp = beam.breakParticles[bi];
      bp.mesh.position.add(bp.velocity);
      bp.velocity.y -= 0.001; // gravity
      bp.life -= 0.02;
      bp.mesh.material.opacity = Math.max(0, bp.life);
      bp.mesh.rotation.x += 0.1;
      bp.mesh.rotation.z += 0.08;
      if (bp.life <= 0) {
        scene.remove(bp.mesh);
        bp.mesh.geometry.dispose();
        bp.mesh.material.dispose();
        beam.breakParticles.splice(bi, 1);
      }
    }

    // Convergence ring: expand and fade.
    if (beam.convergeRing) {
      const cr = beam.convergeRing;
      cr.scale += 0.04;
      cr.fade -= 0.006;
      cr.mesh.scale.setScalar(cr.scale);
      cr.mesh.material.opacity = Math.max(0, cr.fade * 0.9);
      if (cr.fade <= 0) {
        node.group.remove(cr.mesh);
        cr.mesh.geometry.dispose();
        cr.mesh.material.dispose();
        beam.convergeRing = null;
      }
    }
  }

  // Update edges.
  for (const [, edge] of state.edges) {
    if (edge.activity > 0) {
      edge.activity = Math.max(0, edge.activity - 0.02);
      edge.mesh.material.opacity = 0.15 + edge.activity * 0.5;
    } else {
      edge.mesh.material.opacity = Math.max(edge.mesh.material.opacity - 0.005, 0.1);
      edge.mesh.material.color.setHex(EDGE_COLORS.peer);
    }

    const nA = state.nodes.get(edge.from);
    const nB = state.nodes.get(edge.to);
    if (nA && nB) {
      const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
      mid.y += 2;
      const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
      const newGeo = new THREE.TubeGeometry(curve, 20, 0.04 + edge.activity * 0.06, 4, false);
      edge.mesh.geometry.dispose();
      edge.mesh.geometry = newGeo;
    }
  }

  // Field arcs — distance-proportional height.
  for (const [, arc] of state.fieldArcs) {
    if (arc.glow > 0) {
      arc.glow = Math.max(0, arc.glow - 0.02);
      arc.mesh.material.opacity = Math.min(arc.coupling * 0.5 + arc.glow * 0.4, 0.6);
      arc.mesh.material.color.setHex(arc.glow > 0.3 ? 0xf0d080 : 0xf0a848);
    } else {
      arc.mesh.material.opacity = Math.max(arc.mesh.material.opacity - 0.003, Math.min(arc.coupling * 0.25, 0.2));
    }
    const nA = state.nodes.get(arc.from);
    const nB = state.nodes.get(arc.to);
    if (nA && nB) {
      const dist = nA.group.position.distanceTo(nB.group.position);
      const arcHeight = Math.max(1.5, dist * 0.25 + arc.coupling * 1.5);
      const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
      mid.y += arcHeight;
      const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
      const newGeo = new THREE.TubeGeometry(curve, 24, 0.02 + arc.coupling * 0.03, 4, false);
      arc.mesh.geometry.dispose();
      arc.mesh.geometry = newGeo;
    }
  }

  // Floaters.
  for (let i = state.floaters.length - 1; i >= 0; i--) {
    const f = state.floaters[i];
    f.sprite.position.add(f.velocity);
    f.life -= f.decay;
    f.sprite.material.opacity = Math.max(0, f.life);
    if (f.life <= 0) {
      scene.remove(f.sprite);
      f.sprite.material.map?.dispose();
      f.sprite.material.dispose();
      state.floaters.splice(i, 1);
    }
  }

  // Arc particles (old style).
  for (let i = state.particles.length - 1; i >= 0; i--) {
    const p = state.particles[i];
    p.t += p.speed;
    if (p.t >= 1) { scene.remove(p.mesh); state.particles.splice(i, 1); continue; }
    p.mesh.position.lerpVectors(p.from, p.to, p.t);
    p.mesh.position.y += Math.sin(p.t * Math.PI) * 2;
    p.mesh.material.opacity = 1 - p.t * 0.7;
  }

  // Edge particles — travel along the edge curve between nodes.
  for (let i = state.edgeParticles.length - 1; i >= 0; i--) {
    const ep = state.edgeParticles[i];
    ep.t += ep.speed;

    if (ep.t >= 1) {
      scene.remove(ep.mesh);
      ep.trail.forEach(t => {scene.remove(t)});
      state.edgeParticles.splice(i, 1);
      continue;
    }

    const nA = state.nodes.get(ep.from);
    const nB = state.nodes.get(ep.to);
    if (!nA || !nB) { scene.remove(ep.mesh); ep.trail.forEach(t => {scene.remove(t)}); state.edgeParticles.splice(i, 1); continue; }

    const posA = nA.group.position;
    const posB = nB.group.position;
    const mid = posA.clone().add(posB).multiplyScalar(0.5);
    mid.y += 2;

    // Quadratic bezier evaluation.
    const t1 = ep.t;
    const omt = 1 - t1;
    ep.mesh.position.set(
      omt * omt * posA.x + 2 * omt * t1 * mid.x + t1 * t1 * posB.x,
      omt * omt * posA.y + 2 * omt * t1 * mid.y + t1 * t1 * posB.y,
      omt * omt * posA.z + 2 * omt * t1 * mid.z + t1 * t1 * posB.z,
    );
    ep.mesh.material.opacity = 1 - ep.t * 0.5;

    // Trail follows with delay.
    for (let ti = 0; ti < ep.trail.length; ti++) {
      const tt = Math.max(0, t1 - (ti + 1) * 0.04);
      const omt2 = 1 - tt;
      ep.trail[ti].position.set(
        omt2 * omt2 * posA.x + 2 * omt2 * tt * mid.x + tt * tt * posB.x,
        omt2 * omt2 * posA.y + 2 * omt2 * tt * mid.y + tt * tt * posB.y,
        omt2 * omt2 * posA.z + 2 * omt2 * tt * mid.z + tt * tt * posB.z,
      );
      ep.trail[ti].material.opacity = (0.4 - ti * 0.1) * (1 - ep.t * 0.5);
    }
  }

  // Periodically re-render node stats (sparklines update).
  statsRedrawTimer += dt;
  if (statsRedrawTimer > 1000) {
    statsRedrawTimer = 0;
    for (const [, node] of state.nodes) renderNodeStats(node);
  }

  updateTimeline();
  renderer.render(scene, camera);
}
requestAnimationFrame(animate);

window.addEventListener('resize', () => {
  camera.aspect = window.innerWidth / window.innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(window.innerWidth, window.innerHeight);
});

document.addEventListener('keydown', (e) => {
  if (e.target.tagName === 'INPUT') return;
  switch(e.key) {
    case ' ': e.preventDefault(); document.getElementById('btn-pause').click(); break;
    case 'l': document.getElementById('btn-log').click(); break;
    case 'p': document.getElementById('btn-prompt').click(); break;
    case 'Escape': closeInspector(); document.getElementById('prompt-panel').classList.remove('open'); break;
    case 'ArrowLeft': if (state.paused && state.scrubPos > 0) scrubTo(state.scrubPos - 1); break;
    case 'ArrowRight': if (state.paused) scrubTo((state.scrubPos >= 0 ? state.scrubPos : state.events.length) + 1); break;
  }
});
