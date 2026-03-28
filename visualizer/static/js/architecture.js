/* ═══════════════════════════════════════════════════════════
   architecture.js — 3D subsystem zones, connections, arrows,
                     zone planes (raycasting), and zone labels
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { zoneGroup } from './scene.js';

/* Actual runtime subsystems:
   machine → stream → emitter → backend → pool, with hardware substrates above. */
export const SYS = {
  machine: { x: 0,   z: -10, y: 0, w: 12, h: 8, depth: 6, label: 'MACHINE', color: 0xffcc66, accent: true },
  stream:  { x: -16, z: 6,   y: 0, w: 10, h: 6, depth: 4, label: 'STREAM',  color: 0x5090d0 },
  emitter: { x: -2,  z: 6,   y: 0, w: 10, h: 6, depth: 4, label: 'EMITTER', color: 0x50c0a0 },
  backend: { x: 14,  z: 6,   y: 0, w: 10, h: 6, depth: 5, label: 'BACKEND', color: 0xa070e0 },
  pool:    { x: 30,  z: 6,   y: 0, w: 10, h: 6, depth: 5, label: 'POOL',    color: 0x8fdc7a },
  cuda:    { x: 12,  z: 18,  y: 0, w: 8,  h: 5, depth: 4, label: 'CUDA',    color: 0x7fb8ff },
  metal:   { x: 24,  z: 18,  y: 0, w: 8,  h: 5, depth: 4, label: 'METAL',   color: 0x6de0c0 },
  cpu:     { x: 36,  z: 18,  y: 0, w: 8,  h: 5, depth: 4, label: 'CPU',     color: 0xffb84d },
};

export const CONNS = [
  { from: 'machine', to: 'stream',  tag: 'load' },
  { from: 'stream',  to: 'emitter', tag: 'frame' },
  { from: 'emitter', to: 'backend', tag: 'dispatch' },
  { from: 'backend', to: 'pool',    tag: 'jobs' },
  { from: 'pool',    to: 'machine', tag: 'result' },
  { from: 'backend', to: 'cuda',    tag: 'CUDA' },
  { from: 'backend', to: 'metal',   tag: 'Metal' },
  { from: 'backend', to: 'cpu',     tag: 'CPU' },
];

const ZONE_ALIASES = {
  dataset: 'machine',
  frame: 'stream',
  chamber: 'emitter',
  kernel: 'backend',
};

export function resolveZoneKey(sysKey) {
  const key = String(sysKey || '');
  if (SYS[key]) return key;
  return ZONE_ALIASES[key] || key;
}

export const zonePlanes = {};

function createWireframeBox(w, h, d, color, opacity = 0.25) {
  const geo = new THREE.BoxGeometry(w, d, h);
  const edges = new THREE.EdgesGeometry(geo);
  const mat = new THREE.LineBasicMaterial({
    color,
    transparent: true,
    opacity,
  });
  return new THREE.LineSegments(edges, mat);
}

function createGlassPanel(w, h, d, color, opacity = 0.03) {
  const geo = new THREE.BoxGeometry(w, d, h);
  const mat = new THREE.MeshPhysicalMaterial({
    color,
    transparent: true,
    opacity,
    roughness: 0.3,
    metalness: 0.1,
    side: THREE.DoubleSide,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  return new THREE.Mesh(geo, mat);
}

function createCornerBrackets(w, h, y, color, opacity = 0.35) {
  const group = new THREE.Group();
  const mat = new THREE.LineBasicMaterial({ color, transparent: true, opacity });
  const hw = w / 2;
  const hh = h / 2;
  const cs = 0.7;

  const corners = [
    [-hw, -hh], [hw, -hh], [hw, hh], [-hw, hh],
  ];

  for (const [cx, cz] of corners) {
    const hg = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(cx - cs * Math.sign(cx || 1), y, cz),
      new THREE.Vector3(cx + cs * Math.sign(cx || 1), y, cz),
    ]);
    group.add(new THREE.Line(hg, mat));

    const vg = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(cx, y, cz - cs * Math.sign(cz || 1)),
      new THREE.Vector3(cx, y, cz + cs * Math.sign(cz || 1)),
    ]);
    group.add(new THREE.Line(vg, mat));
  }

  return group;
}

// ── Build Architecture ─────────────────────────────────────
export function buildArchitecture() {
  for (const [key, sys] of Object.entries(SYS)) {
    const baseY = sys.depth / 2;

    const wireBox = createWireframeBox(sys.w, sys.h, sys.depth, sys.color, 0.3);
    wireBox.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(wireBox);

    const glass = createGlassPanel(sys.w, sys.h, sys.depth, sys.color, sys.accent ? 0.04 : 0.02);
    glass.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(glass);

    const innerGrid = new THREE.GridHelper(
      Math.max(1, Math.min(sys.w, sys.h) - 1),
      Math.max(1, Math.min(sys.w, sys.h) - 1),
      sys.color,
      sys.color,
    );
    innerGrid.material = innerGrid.material.clone();
    innerGrid.material.transparent = true;
    innerGrid.material.opacity = 0.06;
    innerGrid.position.set(sys.x, 0.02, sys.z);
    zoneGroup.add(innerGrid);

    const brackets = createCornerBrackets(sys.w, sys.h, 0.03, sys.color, 0.4);
    brackets.position.set(sys.x, 0, sys.z);
    zoneGroup.add(brackets);

    const topBrackets = createCornerBrackets(sys.w, sys.h, sys.depth, sys.color, 0.2);
    topBrackets.position.set(sys.x, 0, sys.z);
    zoneGroup.add(topBrackets);

    const zonePL = new THREE.PointLight(sys.color, sys.accent ? 0.25 : 0.1, 15, 2);
    zonePL.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(zonePL);

    const div = document.createElement('div');
    div.className = sys.accent ? 'subsystem-label center' : 'subsystem-label';
    div.textContent = sys.label;
    const lbl = new CSS2DObject(div);
    lbl.position.set(sys.x, sys.depth + 0.5, sys.z);
    zoneGroup.add(lbl);

    sys.center = new THREE.Vector3(sys.x, baseY, sys.z);
    sys.labelObj = lbl;
    sys.wireBox = wireBox;
    sys.glassPanel = glass;

    const planeGeo = new THREE.BoxGeometry(sys.w, sys.depth, sys.h);
    const planeMat = new THREE.MeshBasicMaterial({
      color: sys.color,
      transparent: true,
      opacity: 0.0,
      side: THREE.DoubleSide,
      depthWrite: false,
    });
    const plane = new THREE.Mesh(planeGeo, planeMat);
    plane.position.set(sys.x, baseY, sys.z);
    plane.userData = { sysKey: key };
    zoneGroup.add(plane);
    zonePlanes[key] = plane;
  }

  const connMat = new THREE.LineDashedMaterial({
    color: 0x3060a0,
    transparent: true,
    opacity: 0.2,
    dashSize: 0.8,
    gapSize: 0.4,
  });

  for (const conn of CONNS) {
    const fromSys = SYS[conn.from];
    const toSys = SYS[conn.to];
    const midY = 1.5;
    const fromPt = new THREE.Vector3(fromSys.x, midY, fromSys.z);
    const toPt = new THREE.Vector3(toSys.x, midY, toSys.z);

    const midPt = new THREE.Vector3().lerpVectors(fromPt, toPt, 0.5);
    midPt.y += 3;

    const curve = new THREE.QuadraticBezierCurve3(fromPt, midPt, toPt);
    const curvePoints = curve.getPoints(40);
    const curveGeo = new THREE.BufferGeometry().setFromPoints(curvePoints);
    const curveLine = new THREE.Line(curveGeo, connMat);
    curveLine.computeLineDistances();
    zoneGroup.add(curveLine);

    const tagDiv = document.createElement('div');
    tagDiv.className = 'conn-label';
    tagDiv.textContent = conn.tag;
    const tagLbl = new CSS2DObject(tagDiv);
    tagLbl.position.copy(midPt);
    zoneGroup.add(tagLbl);

    const arrowPos = curve.getPointAt(0.7);
    const arrowDir = curve.getTangentAt(0.7);
    const arrowGeo = new THREE.ConeGeometry(0.2, 0.6, 4);
    const arrowMat = new THREE.MeshBasicMaterial({
      color: 0x3060a0,
      transparent: true,
      opacity: 0.25,
    });
    const arrow = new THREE.Mesh(arrowGeo, arrowMat);
    arrow.position.copy(arrowPos);
    arrow.quaternion.setFromUnitVectors(
      new THREE.Vector3(0, 1, 0),
      arrowDir.clone().normalize(),
    );
    zoneGroup.add(arrow);

    conn.fromPt = fromPt;
    conn.toPt = toPt;
    conn.curve = curve;
  }
}

// ── Zone Activity Labels ───────────────────────────────────
const zoneLabels = new Map();
const MAX_ZONE_LABELS = 5;
const ZONE_LABEL_SPACING = 0.6;

export function addZoneLabel(sysKey, text) {
  const actualKey = resolveZoneKey(sysKey);
  const sys = SYS[actualKey];
  if (!sys) return;

  const displayText = (text || '').trim().slice(0, 28) || '·';
  const arr = zoneLabels.get(actualKey) || [];

  for (const item of arr) {
    item.div.classList.remove('fresh');
    item.div.classList.add('fading');
  }

  const div = document.createElement('div');
  div.className = 'zone-activity-label fresh';
  div.textContent = displayText;

  const lbl = new CSS2DObject(div);
  zoneGroup.add(lbl);
  arr.push({ lbl, div, time: Date.now() });

  while (arr.length > MAX_ZONE_LABELS) {
    const old = arr.shift();
    if (old.lbl?.element?.parentNode) {
      old.lbl.element.remove();
    }
    zoneGroup.remove(old.lbl);
    old.lbl = null;
    old.div = null;
  }

  const hw = sys.w / 2;
  for (let i = 0; i < arr.length; i++) {
    const slot = arr.length - 1 - i;
    arr[i].lbl.position.set(
      sys.x - hw + 0.3,
      sys.depth + 0.3,
      sys.z + slot * ZONE_LABEL_SPACING - 0.5,
    );
  }

  zoneLabels.set(actualKey, arr);
}

export function clearZoneLabels(sysKey) {
  const actualKey = resolveZoneKey(sysKey);
  const arr = zoneLabels.get(actualKey);
  if (!arr) return;
  for (const item of arr) {
    if (item.lbl?.element?.parentNode) {
      item.lbl.element.remove();
    }
    zoneGroup.remove(item.lbl);
    if (item.el) item.el = null;
    if (item.dom) item.dom = null;
    if (item.div) item.div = null;
    item.lbl = null;
  }
  arr.length = 0;
}

export function clearAllZoneLabels() {
  for (const [key] of zoneLabels) clearZoneLabels(key);
  zoneLabels.clear();
}

// ── Zone Hover Effects ─────────────────────────────────────
let currentHoverKey = null;

export function setZoneHover(key) {
  const actualKey = resolveZoneKey(key);
  if (actualKey === currentHoverKey) return;

  if (currentHoverKey && SYS[currentHoverKey]) {
    const sys = SYS[currentHoverKey];
    if (sys.wireBox) {
      sys.wireBox.material.opacity = 0.3;
    }
    if (zonePlanes[currentHoverKey]) {
      zonePlanes[currentHoverKey].material.opacity = 0.0;
    }
  }

  currentHoverKey = actualKey;

  if (actualKey && SYS[actualKey]) {
    const sys = SYS[actualKey];
    if (sys.wireBox) {
      sys.wireBox.material.opacity = 0.6;
    }
    if (zonePlanes[actualKey]) {
      zonePlanes[actualKey].material.opacity = 0.04;
    }
  }
}

// ── Pulse Effect ───────────────────────────────────────────
const pulseTargets = new Map();

export function pulseZone(sysKey) {
  pulseTargets.set(resolveZoneKey(sysKey), Date.now());
}

export function updateZonePulses() {
  const now = Date.now();
  for (const [key, startTime] of pulseTargets) {
    const elapsed = now - startTime;
    if (elapsed > 600) {
      pulseTargets.delete(key);
      const sys = SYS[key];
      if (sys?.glassPanel) {
        sys.glassPanel.material.opacity = sys.accent ? 0.04 : 0.02;
      }
      continue;
    }
    const t = elapsed / 600;
    const pulse = Math.sin(t * Math.PI) * 0.08;
    const sys = SYS[key];
    if (sys?.glassPanel) {
      sys.glassPanel.material.opacity = (sys.accent ? 0.04 : 0.02) + pulse;
    }
  }
}
