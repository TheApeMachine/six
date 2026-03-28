/* ═══════════════════════════════════════════════════════════
   architecture.js — 3D Subsystem zones, connections, arrows,
                     zone planes (raycasting), and zone labels
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { zoneGroup } from './scene.js';

/* Five zones: the actual substrate path
   (dataset → frame → orchestrator → chamber → kernel). */
export const SYS = {
  dataset: { x: -16, z: 14, y: 0,   w: 8, h: 6, depth: 4, label: 'DATASET',        color: 0x6080c0 },
  frame:   { x: 0,   z: 16, y: 0,   w: 8, h: 6, depth: 4, label: 'FRAME · 1024 B', color: 0x5090d0 },
  machine: { x: 0,   z: 0,  y: 0,   w: 10, h: 8, depth: 6, label: 'ORCHESTRATOR',  color: 0xffcc66, accent: true },
  chamber: { x: 16,  z: 0,  y: 0,   w: 8, h: 8, depth: 5, label: 'VALUE · CHAMBER', color: 0x50c0a0 },
  kernel:  { x: 0,   z: -16, y: 0,  w: 9, h: 6, depth: 5, label: 'CPU KERNEL',     color: 0x9070e0 },
};

export const CONNS = [
  { from: 'dataset', to: 'frame',   tag: '1024 B frame' },
  { from: 'frame',   to: 'machine', tag: 'Value.Write' },
  { from: 'machine', to: 'chamber', tag: 'io.Copy merge' },
  { from: 'chamber', to: 'kernel',  tag: 'motor + ALU' },
  { from: 'kernel',  to: 'machine', tag: 'frame out' },
];

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
  const hw = w / 2, hh = h / 2;
  const cs = 0.7; // corner bracket size

  const corners = [
    [-hw, -hh], [hw, -hh], [hw, hh], [-hw, hh],
  ];

  for (const [cx, cz] of corners) {
    // Horizontal bracket
    const hg = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(cx - cs * Math.sign(cx || 1), y, cz),
      new THREE.Vector3(cx + cs * Math.sign(cx || 1), y, cz),
    ]);
    group.add(new THREE.Line(hg, mat));

    // Vertical bracket
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

    // Wireframe box (main structure)
    const wireBox = createWireframeBox(sys.w, sys.h, sys.depth, sys.color, 0.3);
    wireBox.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(wireBox);

    // Glass fill
    const glass = createGlassPanel(sys.w, sys.h, sys.depth, sys.color, sys.accent ? 0.04 : 0.02);
    glass.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(glass);

    // Floor grid within zone
    const innerGrid = new THREE.GridHelper(
      Math.min(sys.w, sys.h) - 1,
      Math.min(sys.w, sys.h) - 1,
      sys.color, sys.color
    );
    innerGrid.material = innerGrid.material.clone();
    innerGrid.material.transparent = true;
    innerGrid.material.opacity = 0.06;
    innerGrid.position.set(sys.x, 0.02, sys.z);
    zoneGroup.add(innerGrid);

    // Corner brackets on floor
    const brackets = createCornerBrackets(sys.w, sys.h, 0.03, sys.color, 0.4);
    brackets.position.set(sys.x, 0, sys.z);
    zoneGroup.add(brackets);

    // Top corner brackets
    const topBrackets = createCornerBrackets(sys.w, sys.h, sys.depth, sys.color, 0.2);
    topBrackets.position.set(sys.x, 0, sys.z);
    zoneGroup.add(topBrackets);

    // Point light inside zone
    const zonePL = new THREE.PointLight(sys.color, sys.accent ? 0.25 : 0.1, 15, 2);
    zonePL.position.set(sys.x, baseY, sys.z);
    zoneGroup.add(zonePL);

    // Label
    const div = document.createElement('div');
    div.className = sys.accent ? 'subsystem-label center' : 'subsystem-label';
    div.textContent = sys.label;
    const lbl = new CSS2DObject(div);
    lbl.position.set(sys.x, sys.depth + 0.5, sys.z);
    zoneGroup.add(lbl);

    // Store metadata
    sys.center = new THREE.Vector3(sys.x, baseY, sys.z);
    sys.labelObj = lbl;
    sys.wireBox = wireBox;
    sys.glassPanel = glass;

    // Clickable plane for raycasting
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

  // ── Connection Lines ───────────────────────────────────────
  const connMat = new THREE.LineDashedMaterial({
    color: 0x3060a0,
    transparent: true,
    opacity: 0.2,
    dashSize: 0.8,
    gapSize: 0.4,
  });

  for (const conn of CONNS) {
    const fromSys = SYS[conn.from], toSys = SYS[conn.to];
    const midY = 1.5;
    const fromPt = new THREE.Vector3(fromSys.x, midY, fromSys.z);
    const toPt = new THREE.Vector3(toSys.x, midY, toSys.z);

    // Build a curved path using an intermediate point above
    const midPt = new THREE.Vector3().lerpVectors(fromPt, toPt, 0.5);
    midPt.y += 3; // arc height

    const curve = new THREE.QuadraticBezierCurve3(fromPt, midPt, toPt);
    const curvePoints = curve.getPoints(40);
    const curveGeo = new THREE.BufferGeometry().setFromPoints(curvePoints);
    const curveLine = new THREE.Line(curveGeo, connMat);
    curveLine.computeLineDistances();
    zoneGroup.add(curveLine);

    // Connection tag at midpoint
    const tagDiv = document.createElement('div');
    tagDiv.className = 'conn-label';
    tagDiv.textContent = conn.tag;
    const tagLbl = new CSS2DObject(tagDiv);
    tagLbl.position.copy(midPt);
    zoneGroup.add(tagLbl);

    // Arrow at 70% along the curve
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
  const sys = SYS[sysKey];
  if (!sys) return;

  const displayText = (text || '').trim().slice(0, 28) || '·';
  const arr = zoneLabels.get(sysKey) || [];

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

  zoneLabels.set(sysKey, arr);
}

export function clearZoneLabels(sysKey) {
  const arr = zoneLabels.get(sysKey);
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
  if (key === currentHoverKey) return;

  // Reset previous
  if (currentHoverKey && SYS[currentHoverKey]) {
    const sys = SYS[currentHoverKey];
    if (sys.wireBox) {
      sys.wireBox.material.opacity = 0.3;
    }
    if (zonePlanes[currentHoverKey]) {
      zonePlanes[currentHoverKey].material.opacity = 0.0;
    }
  }

  currentHoverKey = key;

  // Highlight new
  if (key && SYS[key]) {
    const sys = SYS[key];
    if (sys.wireBox) {
      sys.wireBox.material.opacity = 0.6;
    }
    if (zonePlanes[key]) {
      zonePlanes[key].material.opacity = 0.04;
    }
  }
}

// ── Pulse Effect ───────────────────────────────────────────
const pulseTargets = new Map();

export function pulseZone(sysKey) {
  pulseTargets.set(sysKey, Date.now());
}

export function updateZonePulses() {
  const now = Date.now();
  for (const [key, startTime] of pulseTargets) {
    const elapsed = now - startTime;
    if (elapsed > 600) {
      pulseTargets.delete(key);
      const sys = SYS[key];
      if (sys.glassPanel) {
        sys.glassPanel.material.opacity = sys.accent ? 0.04 : 0.02;
      }
      continue;
    }
    const t = elapsed / 600;
    const pulse = Math.sin(t * Math.PI) * 0.08;
    const sys = SYS[key];
    if (sys.glassPanel) {
      sys.glassPanel.material.opacity = (sys.accent ? 0.04 : 0.02) + pulse;
    }
  }
}
