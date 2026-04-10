import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';
import { GF_LAYER, NODE_RADIUS } from './constants.js';
import { createPhaseDirectionArrow } from './gf_markers.js';
import { textSprite } from './text.js';
import { updateStats } from './stats.js';

export function nodeAngle(idx, total) {
  return (idx / Math.max(total, 1)) * Math.PI * 2;
}

export function repositionNodes() {
  const total = state.nodes.size;
  let idx = 0;
  for (const [, node] of state.nodes) {
    const angle = nodeAngle(idx, total);
    node.targetPos.set(
      Math.cos(angle) * NODE_RADIUS,
      0,
      Math.sin(angle) * NODE_RADIUS,
    );
    idx++;
  }
}

/*
Blueprint-style palette for node geometry.
*/
const BLUEPRINT_BLUE = 0x3a6090;
const BLUEPRINT_EDGE = 0x4a80c0;

export function createNode(id, label) {
  if (state.nodes.has(id)) return;

  const group = new THREE.Group();
  scene.add(group);

  /*
  Kadabra host — wireframe box with edge highlight lines. No spinning.
  The EdgesGeometry gives the clean technical-drawing look.
  */
  const boxGeo = new THREE.BoxGeometry(2.2, 2.2, 2.2);
  const boxEdges = new THREE.EdgesGeometry(boxGeo);
  const core = new THREE.LineSegments(
    boxEdges,
    new THREE.LineBasicMaterial({ color: BLUEPRINT_EDGE, transparent: true, opacity: 0.8 }),
  );
  core.userData.id = id;
  core.userData.kind = 'node';
  group.add(core);

  /*
  Faint filled face for a slight sense of volume — very low opacity.
  */
  const faceMat = new THREE.MeshBasicMaterial({
    color: BLUEPRINT_BLUE, transparent: true, opacity: 0.06, side: THREE.DoubleSide,
  });
  const face = new THREE.Mesh(boxGeo, faceMat);
  group.add(face);

  /*
  Wire shell — outer boundary for hover/selection highlight.
  */
  const wireGeo = new THREE.BoxGeometry(2.6, 2.6, 2.6);
  const wireEdges = new THREE.EdgesGeometry(wireGeo);
  const wire = new THREE.LineSegments(
    wireEdges,
    new THREE.LineBasicMaterial({ color: BLUEPRINT_BLUE, transparent: true, opacity: 0.15 }),
  );
  group.add(wire);

  /*
  Pulse ring — flat ring that expands on activity (horizontal, blueprint style).
  */
  const pulseGeo = new THREE.RingGeometry(1.8, 1.9, 32);
  const pulseMat = new THREE.MeshBasicMaterial({
    color: BLUEPRINT_EDGE, transparent: true, opacity: 0.0, side: THREE.DoubleSide,
  });
  const pulseRing = new THREE.Mesh(pulseGeo, pulseMat);
  pulseRing.rotation.x = -Math.PI / 2;
  group.add(pulseRing);

  /*
  GF(8191) phase ring — horizontal torus kept as a thin ring with phase arrow.
  No spinning — arrow rotates only when real phase data arrives.
  */
  const torusMajor = 2.38;
  const torusMinor = 0.04;
  const field8191Shell = new THREE.Group();
  field8191Shell.position.y = -2.72;

  const field8191Geo = new THREE.TorusGeometry(torusMajor, torusMinor, 8, 48);
  const field8191Mat = new THREE.MeshBasicMaterial({
    color: GF_LAYER.node.color, transparent: true, opacity: 0.35,
  });
  const field8191Ring = new THREE.Mesh(field8191Geo, field8191Mat);
  field8191Ring.rotation.x = Math.PI / 2;
  field8191Shell.add(field8191Ring);

  const phaseArrow8191 = createPhaseDirectionArrow(GF_LAYER.node.color, 0.5, 0.13, 0.09);
  phaseArrow8191.position.set(torusMajor - torusMinor * 0.6, 0, 0);
  field8191Shell.add(phaseArrow8191);

  group.add(field8191Shell);

  const nameSprite = textSprite(label || id, '#4a80c0', 22, true);
  nameSprite.position.y = 2.6;
  nameSprite.scale.set(4.5, 1.1, 1);
  group.add(nameSprite);

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

  const trieGroup = new THREE.Group();
  trieGroup.position.y = -5.5;
  group.add(trieGroup);

  /*
  Dashed connector from host box down to trie cluster.
  */
  const stemGeo = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(0, -1.5, 0),
    new THREE.Vector3(0, -5, 0),
  ]);
  const stem = new THREE.Line(stemGeo, new THREE.LineDashedMaterial({
    color: 0x203858, transparent: true, opacity: 0.3,
    dashSize: 0.3, gapSize: 0.2,
  }));
  stem.computeLineDistances();
  group.add(stem);

  const nodeData = {
    group, core, face, wire, pulseRing, field8191Shell, field8191Ring, phaseArrow8191,
    trieGroup,
    statsCanvas, statsTex, statsSprite,
    targetPos: new THREE.Vector3(),
    pulseScale: 1.0,
    pulseAlpha: 0.0,
    glowIntensity: 0,
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
    trieCouplings: new Map(),
    trieSignals: [],
    trieModes: [],
    triePressures: [],
    beam: {
      collecting: false,
      rays: [],
      hypotheses: [],
      breakParticles: [],
      convergeRing: null,
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

export function tickNodeActivity(nodeId, amount) {
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

export function startSparklineTicker() {
  const sparklineIntervalId = setInterval(advanceSparklines, 500);
  window.addEventListener('beforeunload', () => {
    clearInterval(sparklineIntervalId);
  });
}

export function renderNodeStats(node) {
  const ctx = node.statsCanvas.getContext('2d');
  const w = node.statsCanvas.width;
  const h = node.statsCanvas.height;
  const d = node.data;

  ctx.clearRect(0, 0, w, h);

  ctx.fillStyle = 'rgba(10,14,20,0.85)';
  ctx.beginPath();
  ctx.roundRect(0, 0, w, h, 12);
  ctx.fill();
  ctx.strokeStyle = 'rgba(58,96,144,0.3)';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.roundRect(0, 0, w, h, 12);
  ctx.stroke();

  ctx.fillStyle = 'rgba(26,40,64,0.3)';
  ctx.fillRect(0, 0, w, 36);

  ctx.font = 'bold 22px monospace';
  ctx.fillStyle = '#4a80c0';
  ctx.fillText(d.label, 14, 26);

  ctx.font = '16px monospace';
  ctx.fillStyle = '#203858';
  ctx.textAlign = 'right';
  ctx.fillText(d.id.substring(0, 20), w - 14, 26);
  ctx.textAlign = 'left';

  let y = 58;
  const ROW = 26;
  const COL2 = 200;
  const COL3 = 500;

  const label = (text, x) => {
    ctx.font = '18px monospace';
    ctx.fillStyle = '#3a5878';
    ctx.fillText(text, x || 14, y);
  };
  const value = (text, color, x) => {
    ctx.font = 'bold 18px monospace';
    ctx.fillStyle = color || '#6090c0';
    ctx.fillText(text, x || COL2, y);
  };

  const bar = (val, max, color, x, barW) => {
    const bx = x || COL2;
    const bw = barW || 200;
    ctx.fillStyle = 'rgba(16,24,40,0.8)';
    ctx.fillRect(bx, y - 12, bw, 10);
    const pct = Math.min(Math.abs(val) / max, 1);
    ctx.fillStyle = color;
    ctx.fillRect(bx, y - 12, bw * pct, 10);
  };

  const s = d.digest.surprisal;
  const sColor = s > 5 ? '#c04040' : s > 2 ? '#c08030' : '#408060';
  label('surprisal');
  if (s !== undefined) {
    value(s.toFixed(4), sColor);
    bar(s, 10, sColor, COL2 + 120, 160);
  } else {
    value('\u2014', '#203858');
  }
  y += ROW;

  const ent = d.digest.entropy;
  label('entropy');
  if (ent !== undefined) {
    value(ent.toFixed(4), '#4a80c0');
    bar(ent, 5, '#4a80c0', COL2 + 120, 160);
  } else {
    value('\u2014', '#203858');
  }
  y += ROW;

  const gr = d.digest.growth;
  label('growth');
  if (gr !== undefined) {
    const grColor = gr > 0 ? '#408060' : '#c04060';
    value((gr >= 0 ? '+' : '') + gr.toFixed(4), grColor);
  } else {
    value('\u2014', '#203858');
  }
  y += ROW;

  ctx.strokeStyle = 'rgba(58,96,144,0.15)';
  ctx.beginPath();
  ctx.moveTo(14, y - 8);
  ctx.lineTo(w - 14, y - 8);
  ctx.stroke();

  const pr = d.pressure;
  if (pr.decay !== undefined || pr.learning !== undefined) {
    label('decay');
    value(pr.decay !== undefined ? pr.decay.toFixed(6) : '\u2014', pr.decay > 0 ? '#c04060' : '#408060');
    label('learn', COL3);
    value(pr.learning !== undefined ? pr.learning.toFixed(6) : '\u2014', pr.learning > 0 ? '#408060' : '#c04060', COL3 + 100);
    y += ROW;
  }

  ctx.strokeStyle = 'rgba(58,96,144,0.15)';
  ctx.beginPath();
  ctx.moveTo(14, y - 8);
  ctx.lineTo(w - 14, y - 8);
  ctx.stroke();

  label('trie_count');
  value(String(d.trieCount), '#408060');
  label('columns', COL3);
  value(String(node.tries.length), '#308070', COL3 + 100);
  y += ROW;

  label('inserts');
  value(String(d.insertCount), '#c04060');
  label('predict', COL3);
  value(String(d.predictCount), '#c08030', COL3 + 100);
  y += ROW;

  label('gossip');
  value(String(d.gossipCount), '#7050a0');
  y += ROW;

  const bm = node.beam;
  if (bm && (bm.lastCompose > 0 || bm.lastCollect > 0 || bm.activeCount > 0)) {
    ctx.strokeStyle = 'rgba(58,96,144,0.15)';
    ctx.beginPath();
    ctx.moveTo(14, y - 8);
    ctx.lineTo(w - 14, y - 8);
    ctx.stroke();
    label('beam');
    const beamTxt = `${bm.activeCount} hyps \u00b7 rej ${bm.rejectedCount} \u00b7 best ${bm.bestScore.toFixed(3)}`;
    value(beamTxt, '#c08030', COL2);
    y += ROW;
  }

  ctx.strokeStyle = 'rgba(58,96,144,0.15)';
  ctx.beginPath();
  ctx.moveTo(14, y - 6);
  ctx.lineTo(w - 14, y - 6);
  ctx.stroke();

  const sparkW = w - 28;
  const sparkH = 24;
  const sparkY = y;
  ctx.fillStyle = 'rgba(10,14,24,0.6)';
  ctx.fillRect(14, sparkY, sparkW, sparkH);

  let sparkMax = 1;
  for (let i = 0; i < 60; i++) {
    if (d.activitySpark[i] > sparkMax) sparkMax = d.activitySpark[i];
  }

  ctx.beginPath();
  ctx.strokeStyle = '#4a80c0';
  ctx.lineWidth = 1.5;
  for (let i = 0; i < 60; i++) {
    const si = (d.sparkIdx + 1 + i) % 60;
    const sx = 14 + (i / 59) * sparkW;
    const sy = sparkY + sparkH - (d.activitySpark[si] / sparkMax) * sparkH;
    if (i === 0) ctx.moveTo(sx, sy);
    else ctx.lineTo(sx, sy);
  }
  ctx.stroke();
  ctx.lineWidth = 1;

  ctx.font = '12px monospace';
  ctx.fillStyle = '#203858';
  ctx.fillText('activity', 16, sparkY + 11);
  y += sparkH + 8;

  const labels = Object.entries(d.labelCounts).sort((a, b) => b[1] - a[1]).slice(0, 4);
  if (labels.length) {
    ctx.strokeStyle = 'rgba(58,96,144,0.15)';
    ctx.beginPath();
    ctx.moveTo(14, y - 8);
    ctx.lineTo(w - 14, y - 8);
    ctx.stroke();

    const total = labels.reduce((s, [, v]) => s + v, 0) || 1;
    const barStart = 14;
    const barTotal = w - 28;
    const colors = ['#7050a0', '#4a80c0', '#408060', '#c08030'];

    for (let i = 0; i < labels.length; i++) {
      const [lbl, cnt] = labels[i];
      const pct = cnt / total;
      const bw = barTotal * pct;
      ctx.fillStyle = colors[i % colors.length];
      ctx.globalAlpha = 0.3;
      ctx.fillRect(barStart + barTotal * (labels.slice(0, i).reduce((s2, [, v2]) => s2 + v2 / total, 0)), y - 10, bw, 16);
      ctx.globalAlpha = 1;
      ctx.font = '14px monospace';
      ctx.fillStyle = colors[i % colors.length];
      const labelX = barStart + barTotal * (labels.slice(0, i).reduce((s2, [, v2]) => s2 + v2 / total, 0)) + 4;
      ctx.fillText(`${lbl} ${cnt}`, labelX, y + 2);
    }
    y += ROW;
  }

  const seqs = d.recentSequences.slice(-5).reverse();
  if (seqs.length) {
    ctx.strokeStyle = 'rgba(58,96,144,0.15)';
    ctx.beginPath();
    ctx.moveTo(14, y - 6);
    ctx.lineTo(w - 14, y - 6);
    ctx.stroke();

    ctx.font = '14px monospace';
    const seqMaxPx = w - 28;
    for (const seq of seqs) {
      ctx.fillStyle = '#305848';
      let display = seq;
      ctx.font = '14px monospace';
      while (display.length > 8 && ctx.measureText(display).width > seqMaxPx) {
        display = `${display.slice(0, display.length - 4)}\u2026`;
      }
      ctx.fillText(display, 14, y + 8);
      y += 18;
    }
  }

  node.statsTex.needsUpdate = true;
}

export function pulseNode(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  node.pulseAlpha = 0.6;
  node.pulseScale = 1.0;
  node.glowIntensity = 1.0;
}
