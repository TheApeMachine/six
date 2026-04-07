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
  Prompt:22, PromptResult:23,
};
const KIND_NAMES = Object.fromEntries(Object.entries(EK).map(([k,v])=>[v,k]));

function kindClass(kind) {
  if (kind <= 2) return 'c-node';
  if (kind <= 5) return 'c-peer';
  if (kind <= 7) return 'c-data';
  if (kind <= 12) return 'c-field';
  if (kind <= 18) return 'c-trie';
  if (kind <= 20) return 'c-pool';
  return 'c-user';
}

const state = {
  nodes: new Map(),
  edges: new Map(),
  fieldArcs: new Map(),   // nodeA|nodeB → { mesh, coupling }
  eigenmodeRing: null,    // group showing dominant eigenmode boundary
  floaters: [],
  particles: [],
  events: [],
  paused: false,
  scrubPos: -1,
  selected: null,
  eventCount: 0,
  droppedCount: 0,
  compute: {
    substrates: {},       // name → { inflight, lastDurationMs, totalDispatches, emaDurationMs }
    totalDispatches: 0,
    recentActions: [],    // last 8 compute actions
  },
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

// Lighting — bright enough to see detail.
scene.add(new THREE.AmbientLight(0x606880, 1.5));
const sun = new THREE.DirectionalLight(0xd0e0ff, 0.8);
sun.position.set(20, 40, 20);
scene.add(sun);
const rim = new THREE.DirectionalLight(0x4060a0, 0.3);
rim.position.set(-20, 5, -20);
scene.add(rim);

// Subtle ground reference — just a few concentric rings.
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

  // Kadabra node — wireframe dodecahedron, recognizable and see-through.
  const coreGeo = new THREE.DodecahedronGeometry(1.4, 0);
  const coreMat = new THREE.MeshPhongMaterial({
    color: 0x6ea8fe, emissive: 0x182840, shininess: 90,
    wireframe: false, transparent: true, opacity: 0.7,
  });
  const core = new THREE.Mesh(coreGeo, coreMat);
  core.userData.id = id;
  group.add(core);

  // Wireframe overlay.
  const wireGeo = new THREE.DodecahedronGeometry(1.5, 0);
  const wireMat = new THREE.MeshBasicMaterial({ color: 0x6ea8fe, wireframe: true, transparent: true, opacity: 0.25 });
  const wire = new THREE.Mesh(wireGeo, wireMat);
  group.add(wire);

  // Name label.
  const nameSprite = textSprite(label || id, '#6ea8fe', 22, true);
  nameSprite.position.y = 2.6;
  nameSprite.scale.set(4.5, 1.1, 1);
  group.add(nameSprite);

  // Live stats panel — high-DPI canvas texture for crisp text.
  const statsCanvas = document.createElement('canvas');
  statsCanvas.width = 800;
  statsCanvas.height = 480;
  const statsTex = new THREE.CanvasTexture(statsCanvas);
  statsTex.minFilter = THREE.LinearFilter;
  statsTex.magFilter = THREE.LinearFilter;
  const statsMat = new THREE.SpriteMaterial({ map: statsTex, transparent: true, depthWrite: false });
  const statsSprite = new THREE.Sprite(statsMat);
  statsSprite.position.y = -3.2;
  statsSprite.scale.set(8, 4.8, 1);
  group.add(statsSprite);

  // Trie cluster — small shapes that appear below the node.
  const trieGroup = new THREE.Group();
  trieGroup.position.y = -5;
  group.add(trieGroup);

  // Vertical line connecting node to trie area.
  const stemGeo = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(0, -1.5, 0),
    new THREE.Vector3(0, -4.5, 0),
  ]);
  const stem = new THREE.Line(stemGeo, new THREE.LineDashedMaterial({
    color: 0x405060, transparent: true, opacity: 0.3,
    dashSize: 0.3, gapSize: 0.2,
  }));
  stem.computeLineDistances();
  group.add(stem);

  const nodeData = {
    group, core, wire, trieGroup,
    statsCanvas, statsTex, statsSprite,
    targetPos: new THREE.Vector3(),
    data: {
      id, label: label || id,
      vals: {}, pressure: {}, digest: {},
      trieCount: 0,
      recentSequences: [],     // last N sequences inserted
      labelCounts: {},         // label → count
      insertCount: 0,
      predictCount: 0,
      gossipCount: 0,
    },
    edges: new Set(),
    tries: [],
  };

  state.nodes.set(id, nodeData);
  repositionNodes();
  renderNodeStats(nodeData);
  updateStats();
}

function renderNodeStats(node) {
  const ctx = node.statsCanvas.getContext('2d');
  const w = node.statsCanvas.width;
  const h = node.statsCanvas.height;
  const d = node.data;

  ctx.clearRect(0, 0, w, h);

  // Background panel.
  ctx.fillStyle = 'rgba(14,16,22,0.82)';
  ctx.beginPath(); ctx.roundRect(0, 0, w, h, 12); ctx.fill();
  ctx.strokeStyle = 'rgba(80,110,160,0.25)';
  ctx.lineWidth = 1.5;
  ctx.beginPath(); ctx.roundRect(0, 0, w, h, 12); ctx.stroke();

  // Header line.
  ctx.fillStyle = 'rgba(60,80,120,0.15)';
  ctx.fillRect(0, 0, w, 36);

  ctx.font = 'bold 22px monospace';
  ctx.fillStyle = '#6ea8fe';
  ctx.fillText(d.label, 14, 26);

  // Right-aligned ID.
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

  // Mini bar for numeric values.
  const bar = (val, max, color, x, barW) => {
    const bx = x || COL2;
    const bw = barW || 200;
    ctx.fillStyle = 'rgba(30,38,55,0.8)';
    ctx.fillRect(bx, y - 12, bw, 10);
    const pct = Math.min(Math.abs(val) / max, 1);
    ctx.fillStyle = color;
    ctx.fillRect(bx, y - 12, bw * pct, 10);
  };

  // Row 1: Surprisal + Entropy.
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

  // Separator.
  ctx.strokeStyle = 'rgba(60,80,120,0.15)';
  ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();

  // Row 2: Field pressure.
  const pr = d.pressure;
  if (pr.decay !== undefined || pr.learning !== undefined) {
    label('decay');
    value(pr.decay !== undefined ? pr.decay.toFixed(6) : '—', pr.decay > 0 ? '#e06888' : '#60d890');
    label('learn', COL3);
    value(pr.learning !== undefined ? pr.learning.toFixed(6) : '—', pr.learning > 0 ? '#60d890' : '#e06888', COL3 + 100);
    y += ROW;
  }

  // Row 3: Activity counters.
  ctx.strokeStyle = 'rgba(60,80,120,0.15)';
  ctx.beginPath(); ctx.moveTo(14, y - 8); ctx.lineTo(w - 14, y - 8); ctx.stroke();

  label('tries');   value(String(d.trieCount), '#60d890');
  label('inserts', COL3); value(String(d.insertCount), '#e06888', COL3 + 100);
  y += ROW;

  label('predict'); value(String(d.predictCount), '#f0a848');
  label('gossip', COL3);  value(String(d.gossipCount), '#a080e0', COL3 + 100);
  y += ROW;

  // Label distribution as inline bars.
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

  // Recent sequences — last 3.
  const seqs = d.recentSequences.slice(-3).reverse();
  if (seqs.length) {
    ctx.strokeStyle = 'rgba(60,80,120,0.15)';
    ctx.beginPath(); ctx.moveTo(14, y - 6); ctx.lineTo(w - 14, y - 6); ctx.stroke();

    ctx.font = '15px monospace';
    for (const seq of seqs) {
      ctx.fillStyle = '#407858';
      const display = seq.length > 60 ? seq.slice(0, 60) + '...' : seq;
      ctx.fillText(display, 14, y + 8);
      y += 20;
    }
  }

  node.statsTex.needsUpdate = true;
}

function addTrieVisual(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node) return;

  const idx = node.tries.length;
  const spread = 1.8;
  const offset = (idx - (node.data.trieCount - 1) / 2) * spread;

  // Trie as a small branching shape.
  const trieGroup = new THREE.Group();
  trieGroup.position.x = offset;

  // Trunk.
  const trunkGeo = new THREE.CylinderGeometry(0.06, 0.06, 1.0, 4);
  const trunkMat = new THREE.MeshBasicMaterial({ color: 0x60d890, transparent: true, opacity: 0.5 });
  const trunk = new THREE.Mesh(trunkGeo, trunkMat);
  trieGroup.add(trunk);

  // Branch tips — represent depth/breadth.
  const branches = new THREE.Group();
  branches.position.y = 0.5;
  for (let b = 0; b < 3; b++) {
    const angle = (b / 3) * Math.PI * 2;
    const branchGeo = new THREE.CylinderGeometry(0.03, 0.03, 0.5, 3);
    const branch = new THREE.Mesh(branchGeo, trunkMat.clone());
    branch.position.set(Math.cos(angle) * 0.3, 0.25, Math.sin(angle) * 0.3);
    branch.rotation.z = angle * 0.3;
    branches.add(branch);

    // Leaf node.
    const leafGeo = new THREE.SphereGeometry(0.08, 4, 4);
    const leaf = new THREE.Mesh(leafGeo, new THREE.MeshBasicMaterial({ color: 0x80f0b0 }));
    leaf.position.set(Math.cos(angle) * 0.3, 0.5, Math.sin(angle) * 0.3);
    branches.add(leaf);
  }
  trieGroup.add(branches);

  // Index label.
  const label = textSprite(`T${idx}`, '#60d890', 12);
  label.position.y = -0.8;
  label.scale.set(1.5, 0.4, 1);
  trieGroup.add(label);

  node.trieGroup.add(trieGroup);
  node.tries.push({ group: trieGroup, branches });
  updateStats();
}

function addEdge(fromId, toId) {
  const eid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  if (state.edges.has(eid)) return;

  const nA = state.nodes.get(fromId);
  const nB = state.nodes.get(toId);
  if (!nA || !nB) return;

  // Curved edge via quadratic bezier.
  const curve = new THREE.QuadraticBezierCurve3(
    nA.group.position.clone(),
    new THREE.Vector3(0, 3, 0),
    nB.group.position.clone(),
  );
  const geo = new THREE.TubeGeometry(curve, 20, 0.04, 4, false);
  const mat = new THREE.MeshBasicMaterial({ color: 0x4868a8, transparent: true, opacity: 0.2 });
  const mesh = new THREE.Mesh(geo, mat);
  scene.add(mesh);

  state.edges.set(eid, { mesh, from: fromId, to: toId, activity: 0 });
  nA.edges.add(eid);
  nB.edges.add(eid);
  updateStats();
}

// --- Field visualization: coupling arcs between nodes ---

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

  // Create a curved arc above the peer edges to show field coupling.
  const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
  mid.y += 5 + coupling * 3;
  const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
  const geo = new THREE.TubeGeometry(curve, 24, 0.02 + coupling * 0.04, 4, false);
  const mat = new THREE.MeshBasicMaterial({
    color: 0xf0a848, transparent: true, opacity: Math.min(coupling * 0.6, 0.5),
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

  // Mark this node as part of the dominant eigenmode.
  node.data.eigenmode = { modeCount, dominantEnergy, flash: 1.0 };

  // Rebuild the eigenmode ring around the dominant cluster.
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

  // Draw golden connecting arcs between eigenmode members.
  for (let i = 0; i < eigenNodes.length; i++) {
    for (let j = i + 1; j < eigenNodes.length; j++) {
      const pA = eigenNodes[i].group.position;
      const pB = eigenNodes[j].group.position;
      const mid = pA.clone().add(pB).multiplyScalar(0.5);
      mid.y += 6;
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

function pulseEdge(fromId, toId, color) {
  const eid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const edge = state.edges.get(eid);
  if (!edge) return;
  edge.activity = 1.0;
  edge.mesh.material.color.setHex(color || 0xa080e0);
}

// Floating text that drifts upward and fades — shows actual data.
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

function spawnParticle(from, to, color, size) {
  const geo = new THREE.SphereGeometry(size || 0.1, 4, 4);
  const mat = new THREE.MeshBasicMaterial({ color: color || 0xe06888, transparent: true });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.position.copy(from);
  scene.add(mesh);

  state.particles.push({
    mesh, from: from.clone(), to: to.clone(),
    t: 0, speed: 0.015 + Math.random() * 0.01,
  });
}

function textSprite(text, color, fontSize, bold) {
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
  const mat = new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false });
  return new THREE.Sprite(mat);
}

function handleEvent(ev) {
  state.events.push(ev);
  state.eventCount++;
  if (state.paused && state.scrubPos >= 0) return;
  applyEvent(ev);
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
        const newCount = Math.floor(ev.vals.trie_count);
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
        if (e) { scene.remove(e.mesh); state.edges.delete(eid); }
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

    case EK.PeerLatency:
      pulseEdge(ev.src, ev.tgt, 0x6ea8fe);
      break;

    case EK.ValuePublished: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.insertCount++;
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      // Show the label floating up from the node.
      const pos = node.group.position.clone();
      pos.y += 1;
      spawnFloater(pos, ev.lbl || 'publish', '#e06888');
      renderNodeStats(node);
      break;
    }

    case EK.ValueReplicated: {
      const nA = state.nodes.get(ev.src);
      const nB = state.nodes.get(ev.tgt);
      if (nA && nB) spawnParticle(nA.group.position, nB.group.position, 0xe06888, 0.15);
      break;
    }

    case EK.GossipSent: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.gossipCount++;
      // Pulse all edges from this node.
      for (const _ of node.edges) pulseEdge(ev.src, '', 0xa080e0);
      break;
    }

    case EK.GossipReceived: {
      const nA = state.nodes.get(ev.tgt);
      const nB = state.nodes.get(ev.src);
      if (nA && nB) {
        pulseEdge(ev.src, ev.tgt, 0xa080e0);
        spawnParticle(nA.group.position, nB.group.position, 0xa080e0, 0.08);
      }
      break;
    }

    case EK.FieldDigest: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.digest = ev.vals || {};
      // Color the core by surprisal.
      const s = ev.vals?.surprisal || 0;
      const t = Math.min(s / 8, 1);
      node.core.material.color.setHex(lerpColor(0x60d890, 0xf06060, t));
      node.core.material.emissive.setHex(lerpColor(0x102818, 0x401010, t));
      node.wire.material.color.setHex(lerpColor(0x60d890, 0xf06060, t));
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
        spawnFloater(pos, `eigenmode ×${modeCount} E=${energy.toFixed(2)}`, '#f0a848');
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
      // Update field arcs from this node to all peers based on pressure magnitude.
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
      const seq = ev.meta?.sequence || '';
      if (seq) {
        node.data.recentSequences.push(seq);
        if (node.data.recentSequences.length > 10) node.data.recentSequences.shift();
        // Show the sequence flowing downward into the trie area.
        const pos = node.group.position.clone();
        pos.y -= 1;
        const display = seq.length > 30 ? seq.slice(0, 30) + '...' : seq;
        spawnFloater(pos, display, '#60d890', new THREE.Vector3(0, -0.02, 0));
      }
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      // Grow a trie branch slightly.
      if (node.tries.length > 0) {
        const trie = node.tries[Math.floor(Math.random() * node.tries.length)];
        if (trie.branches.children.length < 30) {
          const angle = Math.random() * Math.PI * 2;
          const r = 0.2 + Math.random() * 0.3;
          const leafGeo = new THREE.SphereGeometry(0.05, 3, 3);
          const leaf = new THREE.Mesh(leafGeo, new THREE.MeshBasicMaterial({ color: 0x80f0b0 }));
          leaf.position.set(Math.cos(angle) * r, 0.3 + Math.random() * 0.4, Math.sin(angle) * r);
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
      break;
    }

    case EK.AdaptiveUpdate: {
      const node = state.nodes.get(ev.src);
      if (node) Object.assign(node.data.vals, ev.vals || {});
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
      state.compute.recentActions.push(`→ ${name} (inflight:${inflight})`);
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
      state.compute.recentActions.push(`✓ ${name} ${durationMs}ms`);
      if (state.compute.recentActions.length > 8) state.compute.recentActions.shift();
      renderComputePanel();
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
  // Deselect previous.
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

  // Peers.
  html += `<div class="insp-section"><h4>peers (${node.edges.size})</h4>`;
  for (const eid of node.edges) {
    const e = state.edges.get(eid);
    if (e) {
      const peer = e.from === state.selected ? e.to : e.from;
      html += `<div class="insp-row"><span class="insp-key">${peer}</span></div>`;
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
  div.innerHTML = `<span class="log-time">${ts}</span><span class="log-kind ${kc}">${KIND_NAMES[ev.kind]||ev.kind}</span><span class="log-src">${ev.src}${ev.tgt ? ' > '+ev.tgt : ''}</span>${ev.lbl ? ' '+ev.lbl : ''}`;
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
  timelineFill.style.width = pct + '%';
  timelineCursor.style.left = pct + '%';
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
  for (let i = 0; i < pos; i++) applyEvent(state.events[i]);
  updateTimeline();
}

function clearScene() {
  for (const [, n] of state.nodes) scene.remove(n.group);
  for (const [, e] of state.edges) scene.remove(e.mesh);
  for (const [, a] of state.fieldArcs) { scene.remove(a.mesh); a.mesh.geometry.dispose(); a.mesh.material.dispose(); }
  if (state.eigenmodeRing) { scene.remove(state.eigenmodeRing); state.eigenmodeRing = null; }
  for (const f of state.floaters) scene.remove(f.sprite);
  for (const p of state.particles) scene.remove(p.mesh);
  state.nodes.clear();
  state.edges.clear();
  state.fieldArcs.clear();
  state.floaters.length = 0;
  state.particles.length = 0;
}

// Buttons.
document.getElementById('btn-pause').addEventListener('click', function() {
  state.paused = !state.paused;
  this.textContent = state.paused ? 'resume' : 'pause';
  this.classList.toggle('active', state.paused);
  if (!state.paused) { state.scrubPos = -1; clearScene(); for (const ev of state.events) applyEvent(ev); }
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
      handleEvent(ev);
    } catch(e) {
      console.error('viz: event parse error', e);
    }
  };
  ws.onclose = () => { document.title = 'Six — Disconnected'; clearTimeout(reconnectTimer); reconnectTimer = setTimeout(connect, 2000); };
  ws.onerror = () => { ws.close(); };
}
function handleServerResponse(resp) {
  if (resp.action === 'scrub_result' && resp.events) { clearScene(); for (const ev of resp.events) applyEvent(ev); }
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

// Render loop.
let lastTime = performance.now(), frameCount = 0, fpsTime = 0;

function animate(now) {
  requestAnimationFrame(animate);
  const dt = now - lastTime; lastTime = now;
  frameCount++; fpsTime += dt;
  if (fpsTime > 1000) { document.getElementById('stat-fps').textContent = frameCount; frameCount = 0; fpsTime = 0; }

  controls.update();

  // Smooth node positioning.
  for (const [, node] of state.nodes) {
    node.group.position.lerp(node.targetPos, 0.04);
    node.core.rotation.y += 0.004;
    node.core.rotation.x += 0.001;
    node.wire.rotation.y += 0.004;
    node.wire.rotation.x += 0.001;
  }

  // Update edges.
  for (const [, edge] of state.edges) {
    // Fade activity.
    if (edge.activity > 0) {
      edge.activity = Math.max(0, edge.activity - 0.02);
      edge.mesh.material.opacity = 0.15 + edge.activity * 0.5;
    } else {
      edge.mesh.material.opacity = Math.max(edge.mesh.material.opacity - 0.005, 0.1);
      edge.mesh.material.color.setHex(0x4868a8);
    }
    // Rebuild curve if nodes moved.
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

  // Field arcs — animate coupling and rebuild geometry when nodes move.
  for (const [, arc] of state.fieldArcs) {
    if (arc.glow > 0) {
      arc.glow = Math.max(0, arc.glow - 0.02);
      arc.mesh.material.opacity = Math.min(arc.coupling * 0.6 + arc.glow * 0.4, 0.7);
      arc.mesh.material.color.setHex(arc.glow > 0.3 ? 0xf0d080 : 0xf0a848);
    } else {
      arc.mesh.material.opacity = Math.max(arc.mesh.material.opacity - 0.003, Math.min(arc.coupling * 0.3, 0.25));
    }
    const nA = state.nodes.get(arc.from);
    const nB = state.nodes.get(arc.to);
    if (nA && nB) {
      const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
      mid.y += 5 + arc.coupling * 3;
      const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
      const newGeo = new THREE.TubeGeometry(curve, 24, 0.02 + arc.coupling * 0.04, 4, false);
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

  // Particles.
  for (let i = state.particles.length - 1; i >= 0; i--) {
    const p = state.particles[i];
    p.t += p.speed;
    if (p.t >= 1) { scene.remove(p.mesh); state.particles.splice(i, 1); continue; }
    p.mesh.position.lerpVectors(p.from, p.to, p.t);
    p.mesh.position.y += Math.sin(p.t * Math.PI) * 2;
    p.mesh.material.opacity = 1 - p.t * 0.7;
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
