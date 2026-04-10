import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';
import { textSprite } from './text.js';

/*
Pipeline layout — "3D architecture drawing" style.

                        [Dataset]
                           |
                     =====[Machine]=====
                     |                 |
               [Tokenizer]       [Backend]
                                   |
                              [Queue/Pool]
                             /    |     \
                         [CPU] [Metal] [CUDA]

Machine sits at world origin (0, 4, 0). Other components are positioned
relative to it, connected by dashed lines (connector tubes).
*/

const MACHINE_POS   = new THREE.Vector3(0, 4, 0);
const DATASET_POS   = new THREE.Vector3(0, 4, -14);
const TOKENIZER_POS = new THREE.Vector3(-10, 4, 0);
const BACKEND_POS   = new THREE.Vector3(10, 4, 0);
const QUEUE_POS     = new THREE.Vector3(10, 4, 10);
const CPU_POS       = new THREE.Vector3(5, 4, 17);
const METAL_POS     = new THREE.Vector3(10, 4, 17);
const CUDA_POS      = new THREE.Vector3(15, 4, 17);

const BLUEPRINT_LINE = 0x4a80c0;
const BLUEPRINT_FILL = 0x1a2840;
const BLUEPRINT_DIM  = 0x203858;

const STAGE_DEFS = [
  { id: 'machine',   label: 'Machine',   pos: MACHINE_POS,   color: 0x4a80c0, size: [3.5, 3.5, 3.5] },
  { id: 'dataset',   label: 'Dataset',   pos: DATASET_POS,   color: 0x408060, size: null, shape: 'cylinder' },
  { id: 'tokenizer', label: 'Tokenizer', pos: TOKENIZER_POS, color: 0x308070, size: [2.4, 2.4, 2.4], shape: 'octahedron' },
  { id: 'backend',   label: 'Backend',   pos: BACKEND_POS,   color: 0x806030, size: [2.6, 2.6, 2.6] },
  { id: 'queue',     label: 'Queue',     pos: QUEUE_POS,     color: 0x7050a0, size: null, shape: 'queue' },
  { id: 'cpu',       label: 'CPU',       pos: CPU_POS,       color: 0x4a80c0, size: [1.6, 1.6, 1.6] },
  { id: 'metal',     label: 'Metal',     pos: METAL_POS,     color: 0x7050a0, size: [1.6, 1.6, 1.6] },
  { id: 'cuda',      label: 'CUDA',      pos: CUDA_POS,      color: 0x608030, size: [1.6, 1.6, 1.6] },
];

const CONNECTIONS = [
  ['dataset', 'machine'],
  ['machine', 'tokenizer'],
  ['machine', 'backend'],
  ['backend', 'queue'],
  ['queue', 'cpu'],
  ['queue', 'metal'],
  ['queue', 'cuda'],
];

function createEdgesBox(w, h, d) {
  const geo = new THREE.BoxGeometry(w, h, d);
  const edges = new THREE.EdgesGeometry(geo);
  return { geo, edges };
}

function createStageGroup(def) {
  const group = new THREE.Group();
  group.position.copy(def.pos);

  let coreObj;
  let pickGeo;

  if (def.shape === 'cylinder') {
    const geo = new THREE.CylinderGeometry(1.2, 1.2, 2.8, 16, 1, true);
    const edges = new THREE.EdgesGeometry(geo);
    coreObj = new THREE.LineSegments(edges, new THREE.LineBasicMaterial({
      color: def.color, transparent: true, opacity: 0.8,
    }));

    const capTop = new THREE.RingGeometry(0, 1.2, 16);
    const capMat = new THREE.MeshBasicMaterial({
      color: def.color, transparent: true, opacity: 0.05, side: THREE.DoubleSide,
    });
    const top = new THREE.Mesh(capTop, capMat);
    top.rotation.x = -Math.PI / 2;
    top.position.y = 1.4;
    group.add(top);

    const bottom = top.clone();
    bottom.position.y = -1.4;
    group.add(bottom);

    pickGeo = new THREE.CylinderGeometry(1.2, 1.2, 2.8, 16);
  } else if (def.shape === 'octahedron') {
    const geo = new THREE.OctahedronGeometry(1.4);
    const edges = new THREE.EdgesGeometry(geo);
    coreObj = new THREE.LineSegments(edges, new THREE.LineBasicMaterial({
      color: def.color, transparent: true, opacity: 0.8,
    }));
    pickGeo = geo;
  } else if (def.shape === 'queue') {
    /*
    Queue is a long thin box (rack-like) with internal goroutine slots
    shown as small line-boxes inside.
    */
    const { edges } = createEdgesBox(3.6, 1.2, 1.2);
    coreObj = new THREE.LineSegments(edges, new THREE.LineBasicMaterial({
      color: def.color, transparent: true, opacity: 0.8,
    }));
    pickGeo = new THREE.BoxGeometry(3.6, 1.2, 1.2);

    const slotCount = 6;
    const slotW = 0.35;
    const gap = (3.2 - slotCount * slotW) / (slotCount + 1);
    for (let i = 0; i < slotCount; i++) {
      const sx = -1.6 + gap + (gap + slotW) * i + slotW / 2;
      const slotEdges = new THREE.EdgesGeometry(new THREE.BoxGeometry(slotW, 0.7, 0.7));
      const slot = new THREE.LineSegments(slotEdges, new THREE.LineBasicMaterial({
        color: def.color, transparent: true, opacity: 0.3,
      }));
      slot.position.x = sx;
      group.add(slot);
    }
  } else {
    const [w, h, d] = def.size;
    const { edges } = createEdgesBox(w, h, d);
    coreObj = new THREE.LineSegments(edges, new THREE.LineBasicMaterial({
      color: def.color, transparent: true, opacity: 0.8,
    }));
    pickGeo = new THREE.BoxGeometry(w, h, d);
  }

  coreObj.userData.id = def.id;
  coreObj.userData.kind = 'pipeline';
  group.add(coreObj);

  /*
  Invisible pick mesh for raycasting — LineSegments can't be picked reliably.
  */
  if (pickGeo) {
    const pickMesh = new THREE.Mesh(pickGeo, new THREE.MeshBasicMaterial({
      visible: false,
    }));
    pickMesh.userData.id = def.id;
    pickMesh.userData.kind = 'pipeline';
    group.add(pickMesh);
  }

  /*
  Faint fill for subtle volume.
  */
  if (def.size && !def.shape) {
    const [w, h, d] = def.size;
    const fillMat = new THREE.MeshBasicMaterial({
      color: BLUEPRINT_FILL, transparent: true, opacity: 0.06, side: THREE.DoubleSide,
    });
    group.add(new THREE.Mesh(new THREE.BoxGeometry(w, h, d), fillMat));
  }

  /*
  Pulse ring.
  */
  const pulseGeo = new THREE.RingGeometry(1.6, 1.7, 32);
  const pulseMat = new THREE.MeshBasicMaterial({
    color: def.color, transparent: true, opacity: 0.0, side: THREE.DoubleSide,
  });
  const pulseRing = new THREE.Mesh(pulseGeo, pulseMat);
  pulseRing.rotation.x = -Math.PI / 2;
  group.add(pulseRing);

  /*
  Label above.
  */
  const hexCss = `#${def.color.toString(16).padStart(6, '0')}`;
  const labelSprite = textSprite(def.label, hexCss, 20, true);
  labelSprite.position.y = 2.8;
  labelSprite.scale.set(4.5, 1.1, 1);
  group.add(labelSprite);

  /*
  Stats canvas.
  */
  const statsCanvas = document.createElement('canvas');
  statsCanvas.width = 480;
  statsCanvas.height = 320;
  const statsTex = new THREE.CanvasTexture(statsCanvas);
  statsTex.minFilter = THREE.LinearFilter;
  statsTex.magFilter = THREE.LinearFilter;
  const statsMat = new THREE.SpriteMaterial({ map: statsTex, transparent: true, depthWrite: false });
  const statsSprite = new THREE.Sprite(statsMat);
  statsSprite.position.y = -2.8;
  statsSprite.scale.set(6, 4, 1);
  group.add(statsSprite);

  scene.add(group);

  return {
    def,
    group,
    core: coreObj,
    pulseRing,
    statsCanvas, statsTex, statsSprite,
    pulseAlpha: 0.0,
    pulseScale: 1.0,
    glow: 0,
    connectors: [],
    metrics: {
      totalEvents: 0,
      bytesProcessed: 0,
      inflight: 0,
      lastDurationMs: 0,
      emaDurationMs: 0,
      recentOps: [],
      activitySpark: new Float32Array(60),
      sparkIdx: 0,
    },
  };
}

function createConnector(fromStage, toStage) {
  const from = fromStage.group.position;
  const to = toStage.group.position;
  const mid = from.clone().add(to).multiplyScalar(0.5);
  mid.y += 1.0;

  const points = [from.clone(), mid.clone(), to.clone()];
  const curve = new THREE.QuadraticBezierCurve3(points[0], points[1], points[2]);

  const linePoints = curve.getPoints(40);
  const lineGeo = new THREE.BufferGeometry().setFromPoints(linePoints);
  const lineMat = new THREE.LineDashedMaterial({
    color: BLUEPRINT_DIM, transparent: true, opacity: 0.35,
    dashSize: 0.4, gapSize: 0.25,
  });
  const line = new THREE.Line(lineGeo, lineMat);
  line.computeLineDistances();
  scene.add(line);

  return { line, curve, fromId: fromStage.def.id, toId: toStage.def.id };
}

export function initPipeline() {
  for (const def of STAGE_DEFS) {
    const stageData = createStageGroup(def);
    state.pipeline.stages.set(def.id, stageData);
    renderStageStats(stageData);
  }

  for (const [fromId, toId] of CONNECTIONS) {
    const fromStage = state.pipeline.stages.get(fromId);
    const toStage = state.pipeline.stages.get(toId);
    if (!fromStage || !toStage) continue;
    const conn = createConnector(fromStage, toStage);
    fromStage.connectors.push(conn);
  }
}

export function pulseStage(stageId) {
  const stage = state.pipeline.stages.get(stageId);
  if (!stage) return;
  stage.pulseAlpha = 0.6;
  stage.pulseScale = 1.0;
}

export function tickStageActivity(stageId, amount) {
  const stage = state.pipeline.stages.get(stageId);
  if (!stage) return;
  stage.metrics.activitySpark[stage.metrics.sparkIdx % 60] += amount;
}

export function addStageOp(stageId, text) {
  const stage = state.pipeline.stages.get(stageId);
  if (!stage) return;
  stage.metrics.recentOps.push(text);
  if (stage.metrics.recentOps.length > 8) stage.metrics.recentOps.shift();
}

/*
spawnPipelineFlowParticle sends a particle along a connector between stages.
*/
export function spawnPipelineFlowParticle(fromId, toId, color) {
  const fromStage = state.pipeline.stages.get(fromId);
  if (!fromStage) return;
  const conn = fromStage.connectors.find((c) => c.toId === toId);
  if (!conn) return;

  const geo = new THREE.SphereGeometry(0.08, 6, 6);
  const mat = new THREE.MeshBasicMaterial({ color: color || BLUEPRINT_LINE, transparent: true });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.position.copy(fromStage.group.position);
  scene.add(mesh);

  const trail = [];
  for (let i = 0; i < 3; i++) {
    const tGeo = new THREE.SphereGeometry(0.04 - i * 0.01, 4, 4);
    const tMat = new THREE.MeshBasicMaterial({ color: color || BLUEPRINT_LINE, transparent: true, opacity: 0.25 - i * 0.06 });
    const tMesh = new THREE.Mesh(tGeo, tMat);
    tMesh.position.copy(fromStage.group.position);
    scene.add(tMesh);
    trail.push(tMesh);
  }

  state.pipeline.flowParticles.push({
    mesh, trail,
    curve: conn.curve,
    t: 0,
    speed: 0.012 + Math.random() * 0.008,
  });
}

export function renderStageStats(stage) {
  const ctx = stage.statsCanvas.getContext('2d');
  const w = stage.statsCanvas.width;
  const h = stage.statsCanvas.height;
  const m = stage.metrics;
  const hexCss = `#${stage.def.color.toString(16).padStart(6, '0')}`;

  ctx.clearRect(0, 0, w, h);

  ctx.fillStyle = 'rgba(10,14,20,0.85)';
  ctx.beginPath();
  ctx.roundRect(0, 0, w, h, 10);
  ctx.fill();
  ctx.strokeStyle = 'rgba(58,96,144,0.3)';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.roundRect(0, 0, w, h, 10);
  ctx.stroke();

  ctx.fillStyle = 'rgba(26,40,64,0.3)';
  ctx.fillRect(0, 0, w, 28);

  ctx.font = 'bold 18px monospace';
  ctx.fillStyle = hexCss;
  ctx.fillText(stage.def.label, 10, 20);

  let y = 48;
  const ROW = 22;

  ctx.font = '14px monospace';
  ctx.fillStyle = '#3a5878';
  ctx.fillText('events', 10, y);
  ctx.fillStyle = '#6090c0';
  ctx.font = 'bold 14px monospace';
  ctx.fillText(String(m.totalEvents), 120, y);
  y += ROW;

  if (m.bytesProcessed > 0) {
    ctx.font = '14px monospace';
    ctx.fillStyle = '#3a5878';
    ctx.fillText('bytes', 10, y);
    ctx.fillStyle = '#6090c0';
    ctx.font = 'bold 14px monospace';
    const kb = (m.bytesProcessed / 1024).toFixed(1);
    ctx.fillText(`${kb} KiB`, 120, y);
    y += ROW;
  }

  if (m.inflight > 0) {
    ctx.font = '14px monospace';
    ctx.fillStyle = '#3a5878';
    ctx.fillText('inflight', 10, y);
    ctx.fillStyle = '#6090c0';
    ctx.font = 'bold 14px monospace';
    ctx.fillText(String(m.inflight), 120, y);
    y += ROW;
  }

  if (m.emaDurationMs > 0) {
    ctx.font = '14px monospace';
    ctx.fillStyle = '#3a5878';
    ctx.fillText('ema', 10, y);
    ctx.fillStyle = '#6090c0';
    ctx.font = 'bold 14px monospace';
    ctx.fillText(`${m.emaDurationMs.toFixed(1)}ms`, 120, y);
    y += ROW;
  }

  const sparkW = w - 20;
  const sparkH = 20;
  ctx.fillStyle = 'rgba(10,14,24,0.6)';
  ctx.fillRect(10, y, sparkW, sparkH);

  let sparkMax = 1;
  for (let i = 0; i < 60; i++) {
    if (m.activitySpark[i] > sparkMax) sparkMax = m.activitySpark[i];
  }

  ctx.beginPath();
  ctx.strokeStyle = hexCss;
  ctx.lineWidth = 1.2;
  for (let i = 0; i < 60; i++) {
    const si = (m.sparkIdx + 1 + i) % 60;
    const sx = 10 + (i / 59) * sparkW;
    const sy = y + sparkH - (m.activitySpark[si] / sparkMax) * sparkH;
    if (i === 0) ctx.moveTo(sx, sy);
    else ctx.lineTo(sx, sy);
  }
  ctx.stroke();
  ctx.lineWidth = 1;
  y += sparkH + 6;

  if (m.recentOps.length) {
    ctx.strokeStyle = 'rgba(58,96,144,0.15)';
    ctx.beginPath();
    ctx.moveTo(10, y - 4);
    ctx.lineTo(w - 10, y - 4);
    ctx.stroke();

    ctx.font = '11px monospace';
    ctx.fillStyle = '#3a5878';
    for (const op of m.recentOps.slice(-5)) {
      ctx.fillText(op.substring(0, 48), 10, y + 8);
      y += 14;
    }
  }

  stage.statsTex.needsUpdate = true;
}

export function advancePipelineSparklines() {
  for (const [, stage] of state.pipeline.stages) {
    stage.metrics.sparkIdx++;
    stage.metrics.activitySpark[stage.metrics.sparkIdx % 60] = 0;
  }
}

/*
animatePipeline — called each frame. No spinning. Pulses decay, glow fades,
flow particles advance along curves.
*/
export function animatePipeline() {
  for (const [, stage] of state.pipeline.stages) {
    if (stage.glow > 0.01) {
      stage.glow *= 0.95;
      stage.core.material.opacity = 0.8 + stage.glow * 0.2;
    }

    if (stage.pulseAlpha > 0.01) {
      stage.pulseScale += 0.025;
      stage.pulseAlpha *= 0.94;
      stage.pulseRing.material.opacity = stage.pulseAlpha;
      stage.pulseRing.scale.setScalar(stage.pulseScale);
    } else {
      stage.pulseRing.material.opacity = 0;
      stage.pulseScale = 1.0;
      stage.pulseRing.scale.setScalar(1.0);
    }
  }

  for (let i = state.pipeline.flowParticles.length - 1; i >= 0; i--) {
    const fp = state.pipeline.flowParticles[i];
    fp.t += fp.speed;

    if (fp.t >= 1) {
      scene.remove(fp.mesh);
      fp.trail.forEach((t) => { scene.remove(t); });
      state.pipeline.flowParticles.splice(i, 1);
      continue;
    }

    const pt = fp.curve.getPointAt(fp.t);
    fp.mesh.position.copy(pt);
    fp.mesh.material.opacity = 1 - fp.t * 0.4;

    for (let ti = 0; ti < fp.trail.length; ti++) {
      const tt = Math.max(0, fp.t - (ti + 1) * 0.04);
      const tpt = fp.curve.getPointAt(tt);
      fp.trail[ti].position.copy(tpt);
      fp.trail[ti].material.opacity = (0.25 - ti * 0.06) * (1 - fp.t * 0.4);
    }
  }
}

/*
getPipelinePickMeshes returns all meshes that should be raycast-pickable
for the pipeline stages.
*/
export function getPipelinePickMeshes() {
  const meshes = [];
  for (const [, stage] of state.pipeline.stages) {
    for (const child of stage.group.children) {
      if (child.userData.kind === 'pipeline') {
        meshes.push(child);
      }
    }
  }
  return meshes;
}
