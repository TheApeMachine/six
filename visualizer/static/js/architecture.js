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
  machine: { x: 0,   z: 14,  y: 0, w: 12, h: 8, depth: 6, label: 'MACHINE', color: 0xffcc66, accent: true },
  stream:  { x: -14, z: 0,   y: 0, w: 10, h: 6, depth: 4, label: 'STREAM',  color: 0x5090d0 },
  emitter: { x: 0,   z: 0,   y: 0, w: 10, h: 6, depth: 4, label: 'EMITTER', color: 0x50c0a0 },
  backend: { x: 14,  z: 0,   y: 0, w: 10, h: 6, depth: 5, label: 'BACKEND', color: 0xa070e0 },
  pool:    { x: 28,  z: 0,   y: 0, w: 10, h: 6, depth: 5, label: 'POOL',    color: 0x8fdc7a },
  cuda:    { x: 8,   z: -12, y: 0, w: 6,  h: 4, depth: 3, label: 'CUDA',    color: 0x7fb8ff },
  metal:   { x: 18,  z: -12, y: 0, w: 6,  h: 4, depth: 3, label: 'METAL',   color: 0x6de0c0 },
  cpu:     { x: 28,  z: -12, y: 0, w: 6,  h: 4, depth: 3, label: 'CPU',     color: 0xffb84d },
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

  buildStreamRing();
  buildUniConnViz();

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

// ── Stream Pipe Ring ──────────────────────────────────────
// Shows the actual multi-region ring buffer rotation. Each region is a distinct
// frame slot around the ring. The rotation pointer (ptr) advances on Write(),
// causing Read() to access frames at (i + ptr) % regions — so frame slots
// shift position each cycle, mixing which emitter reads which frame.
// Count defaults to 4; sync with GET /api/system → streamRegions via setStreamRegionCount + rebuildStreamRing.
export let streamRegionCount = 4;
let streamSlots = [];     // Visual slot meshes (+ CSS2D labels)
let streamPtrIndicator = null;
let streamPtrAngle = 0;   // Current rotation offset (simulates ptr)
let streamPtrTarget = 0;  // Target angle after a Write event

/** Clamp and apply region count from server topology (streamRegions). */
export function setStreamRegionCount(n) {
  const v = parseInt(String(n), 10);
  streamRegionCount = Number.isFinite(v) && v >= 1 ? Math.min(v, 64) : 4;
}

/** Remove stream ring meshes and rebuild with current streamRegionCount. */
export function rebuildStreamRing() {
  const sys = SYS.stream;
  if (!sys) return;
  for (const s of streamSlots) {
    zoneGroup.remove(s.arc);
    if (s.lbl) zoneGroup.remove(s.lbl);
    s.arc.geometry.dispose();
    s.mat.dispose();
  }
  streamSlots.length = 0;
  if (streamPtrIndicator) {
    zoneGroup.remove(streamPtrIndicator);
    streamPtrIndicator.geometry.dispose();
    streamPtrIndicator.material.dispose();
    streamPtrIndicator = null;
  }
  streamPtrAngle = 0;
  streamPtrTarget = 0;
  buildStreamRing();
}

function buildStreamRing() {
  const sys = SYS.stream;
  if (!sys) return;

  const n = streamRegionCount;
  const ringRadius = Math.min(sys.w, sys.h) * 0.28;
  const slotArc = (Math.PI * 2) / n;
  const slotGap = slotArc * 0.08;

  // Build one arc segment per region
  for (let i = 0; i < n; i++) {
    const startAngle = i * slotArc + slotGap;
    const endAngle = (i + 1) * slotArc - slotGap;
    const arcGeo = new THREE.RingGeometry(
      ringRadius - 0.2, ringRadius + 0.2, 16, 1, startAngle, endAngle - startAngle,
    );
    const hue = (i / n) * 0.25 + 0.55; // Blue-cyan range
    const color = new THREE.Color().setHSL(hue, 0.6, 0.5);
    const arcMat = new THREE.MeshBasicMaterial({
      color,
      transparent: true,
      opacity: 0.15,
      side: THREE.DoubleSide,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });
    const arc = new THREE.Mesh(arcGeo, arcMat);
    arc.rotation.x = -Math.PI / 2;
    arc.position.set(sys.x, sys.depth / 2, sys.z);
    zoneGroup.add(arc);

    // Region label
    const midAngle = (startAngle + endAngle) / 2;
    const lx = Math.cos(midAngle) * (ringRadius + 0.5);
    const lz = Math.sin(midAngle) * (ringRadius + 0.5);
    const div = document.createElement('div');
    div.className = 'region-label default';
    div.textContent = `R${i}`;
    div.style.fontSize = '7px';
    div.style.opacity = '0.5';
    const lbl = new CSS2DObject(div);
    lbl.position.set(sys.x + lx, sys.depth / 2, sys.z + lz);
    zoneGroup.add(lbl);

    streamSlots.push({ arc, lbl, mat: arcMat, idx: i, baseOpacity: 0.15 });
  }

  // Pointer indicator — a small bright dot showing the current ptr offset
  const ptrGeo = new THREE.SphereGeometry(0.12, 8, 8);
  const ptrMat = new THREE.MeshBasicMaterial({
    color: 0xffcc66,
    transparent: true,
    opacity: 0.8,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  });
  streamPtrIndicator = new THREE.Mesh(ptrGeo, ptrMat);
  streamPtrIndicator.position.set(
    sys.x + ringRadius, sys.depth / 2 + 0.2, sys.z,
  );
  zoneGroup.add(streamPtrIndicator);
}

// Called when a stream Write event occurs — advances the pointer by one slot
export function advanceStreamPtr() {
  streamPtrTarget += (Math.PI * 2) / streamRegionCount;
}

export function animateStreamRing(time, paused) {
  if (!streamPtrIndicator || paused) return;

  const sys = SYS.stream;
  const ringRadius = Math.min(sys.w, sys.h) * 0.28;

  // Smoothly interpolate pointer angle toward target
  streamPtrAngle += (streamPtrTarget - streamPtrAngle) * 0.08;

  const px = sys.x + Math.cos(streamPtrAngle) * ringRadius;
  const pz = sys.z + Math.sin(streamPtrAngle) * ringRadius;
  streamPtrIndicator.position.set(px, sys.depth / 2 + 0.2, pz);

  // Highlight the slot the pointer is currently over
  const n = streamRegionCount;
  const currentSlot = Math.floor(
    ((streamPtrAngle % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2)
    / ((Math.PI * 2) / n),
  ) % n;

  for (const slot of streamSlots) {
    slot.mat.opacity = slot.idx === currentSlot ? 0.45 : slot.baseOpacity;
  }
}

// ── Fold Effect ───────────────────────────────────────────
// Shows the actual fold: Value B (incoming) is written into Value A (receiver).
// A's program executes UniversalBitwise(a, b) — the ALU runs A's truth tables
// against both memories. Visual: B flows as a stream into A, A's box pulses
// as the ALU fires, then A emits the mutated result.
const foldEffects = [];
const MAX_FOLD_EFFECTS = 8;

export function spawnFoldEffect(receiverLabel, incomingLabel, firmware) {
  const sys = SYS.emitter || SYS.backend;
  if (!sys) return;

  // Rate-limit
  if (foldEffects.length >= MAX_FOLD_EFFECTS) return;

  const cx = sys.x;
  const cy = sys.depth / 2;
  const cz = sys.z;

  // Incoming value (B) — small box approaching from left
  const inGeo = new THREE.BoxGeometry(0.3, 0.3, 0.3);
  const inMat = new THREE.MeshBasicMaterial({
    color: 0x5090d0, transparent: true, opacity: 0.7,
    blending: THREE.AdditiveBlending, depthWrite: false,
  });
  const inMesh = new THREE.Mesh(inGeo, inMat);
  inMesh.position.set(cx - 3, cy + 1.5, cz);
  zoneGroup.add(inMesh);

  // Receiver value (A) — slightly larger box, stays in place, will pulse
  const rcvGeo = new THREE.BoxGeometry(0.5, 0.5, 0.5);
  const rcvMat = new THREE.MeshBasicMaterial({
    color: 0xffcc66, transparent: true, opacity: 0.3,
    blending: THREE.AdditiveBlending, depthWrite: false,
  });
  const rcvMesh = new THREE.Mesh(rcvGeo, rcvMat);
  rcvMesh.position.set(cx, cy + 1.5, cz);
  zoneGroup.add(rcvMesh);

  // ALU flash (appears during execution phase)
  const aluGeo = new THREE.BoxGeometry(0.6, 0.15, 0.6);
  const aluMat = new THREE.MeshBasicMaterial({
    color: 0xa070e0, transparent: true, opacity: 0,
    blending: THREE.AdditiveBlending, depthWrite: false,
  });
  const aluMesh = new THREE.Mesh(aluGeo, aluMat);
  aluMesh.position.set(cx, cy + 1.1, cz);
  zoneGroup.add(aluMesh);

  foldEffects.push({
    inMesh, rcvMesh, aluMesh,
    cx, cy, cz,
    t0: Date.now(),
    dur: 1600,
    firmware: firmware || '',
  });
}

export function updateFoldEffects() {
  const now = Date.now();
  for (let i = foldEffects.length - 1; i >= 0; i--) {
    const fx = foldEffects[i];
    const t = (now - fx.t0) / fx.dur;

    if (t >= 1) {
      zoneGroup.remove(fx.inMesh);
      zoneGroup.remove(fx.rcvMesh);
      zoneGroup.remove(fx.aluMesh);
      fx.inMesh.geometry.dispose(); fx.inMesh.material.dispose();
      fx.rcvMesh.geometry.dispose(); fx.rcvMesh.material.dispose();
      fx.aluMesh.geometry.dispose(); fx.aluMesh.material.dispose();
      foldEffects.splice(i, 1);
      continue;
    }

    // Phase 1 (0..0.3): Incoming B slides into position next to A
    // Phase 2 (0.3..0.7): ALU fires — A pulses, ALU bar flashes
    // Phase 3 (0.7..1): Result — A emits mutated state, B fades
    if (t < 0.3) {
      const p = t / 0.3;
      fx.inMesh.position.x = fx.cx - 3 * (1 - p);
      fx.inMesh.material.opacity = 0.7;
      fx.rcvMesh.material.opacity = 0.3;
      fx.aluMesh.material.opacity = 0;
    } else if (t < 0.7) {
      const p = (t - 0.3) / 0.4;
      fx.inMesh.position.x = fx.cx - 0.4;
      // ALU execution — flash and pulse
      fx.aluMesh.material.opacity = Math.sin(p * Math.PI * 3) * 0.5 + 0.1;
      fx.rcvMesh.material.opacity = 0.3 + Math.sin(p * Math.PI * 3) * 0.2;
      fx.rcvMesh.scale.setScalar(1 + Math.sin(p * Math.PI * 3) * 0.1);
      // B fades as its data is consumed
      fx.inMesh.material.opacity = 0.7 * (1 - p * 0.6);
      fx.inMesh.scale.setScalar(1 - p * 0.3);
    } else {
      const p = (t - 0.7) / 0.3;
      // ALU done, B fully consumed, A emits result
      fx.aluMesh.material.opacity = 0.3 * (1 - p);
      fx.inMesh.material.opacity = 0.15 * (1 - p);
      fx.rcvMesh.material.opacity = (0.5 - p * 0.3);
      fx.rcvMesh.material.color.setHex(0x8fdc7a); // Green = mutated
      fx.rcvMesh.scale.setScalar(1);
    }
  }
}

// ── UniConn Network Visualization ─────────────────────────
// Shows the three transport types (IPC, UDP Multicast, QUIC) as distinct
// pathways beneath the machine. UniConn implements io.ReadWriteCloser and
// delegates to one of three underlying transports. Data flows as fixed
// 1024-byte Value frames. The Gate can switch from primary to secondary
// transport on EOF.
const UNICONN_TRANSPORTS = [
  { key: 'ipc',  label: 'IPC',  color: 0x7fb8ff, desc: 'Unix socket · same-machine' },
  { key: 'udp',  label: 'UDP',  color: 0x6de0c0, desc: 'Multicast · LAN broadcast' },
  { key: 'quic', label: 'QUIC', color: 0xffb84d, desc: 'Reliable stream · WAN' },
];

let uniconnGroup = null;
let uniconnLanes = [];
let uniconnFrames = [];  // Active frame particles in flight
let uniconnGateIndicator = null;
let uniconnActiveTransport = null; // 'ipc', 'udp', or 'quic'
let uniconnPeerNodes = [];
const MAX_UNICONN_FRAMES = 12;
const UNICONN_FRAME_SPEED = 0.012;

function buildUniConnViz() {
  const machine = SYS.machine;
  if (!machine) return;

  uniconnGroup = new THREE.Group();
  zoneGroup.add(uniconnGroup);

  // Position the network viz behind the machine (lower z)
  const baseX = machine.x;
  const baseZ = machine.z + machine.h / 2 + 6; // In front of machine
  const baseY = 0;
  const laneSpacing = 5;

  for (let i = 0; i < UNICONN_TRANSPORTS.length; i++) {
    const t = UNICONN_TRANSPORTS[i];
    const laneX = baseX + (i - 1) * laneSpacing;
    const laneZ = baseZ;
    const laneY = baseY;

    // Lane tube — a thin cylinder representing the transport pipe
    const tubeGeo = new THREE.CylinderGeometry(0.08, 0.08, 8, 8);
    const tubeMat = new THREE.MeshBasicMaterial({
      color: t.color, transparent: true, opacity: 0.15,
      blending: THREE.AdditiveBlending, depthWrite: false,
    });
    const tube = new THREE.Mesh(tubeGeo, tubeMat);
    tube.position.set(laneX, 4, laneZ);
    // Rotate so the cylinder runs vertically (Y axis)
    uniconnGroup.add(tube);

    // Transport type icon — a small distinctive shape at the top
    let iconMesh;
    if (t.key === 'ipc') {
      // IPC: unix socket — small square (local)
      const iconGeo = new THREE.BoxGeometry(0.4, 0.4, 0.4);
      const iconMat = new THREE.MeshBasicMaterial({
        color: t.color, transparent: true, opacity: 0.3,
        blending: THREE.AdditiveBlending, depthWrite: false,
      });
      iconMesh = new THREE.Mesh(iconGeo, iconMat);
    } else if (t.key === 'udp') {
      // UDP: multicast — radial burst (broadcast)
      const iconGeo = new THREE.OctahedronGeometry(0.3, 0);
      const iconMat = new THREE.MeshBasicMaterial({
        color: t.color, transparent: true, opacity: 0.3,
        blending: THREE.AdditiveBlending, depthWrite: false,
      });
      iconMesh = new THREE.Mesh(iconGeo, iconMat);
    } else {
      // QUIC: reliable stream — torus (connection)
      const iconGeo = new THREE.TorusGeometry(0.2, 0.06, 8, 16);
      const iconMat = new THREE.MeshBasicMaterial({
        color: t.color, transparent: true, opacity: 0.3,
        blending: THREE.AdditiveBlending, depthWrite: false,
      });
      iconMesh = new THREE.Mesh(iconGeo, iconMat);
    }
    iconMesh.position.set(laneX, 8.5, laneZ);
    uniconnGroup.add(iconMesh);

    // Transport label
    const div = document.createElement('div');
    div.className = 'uniconn-label';
    div.textContent = t.label;
    div.style.fontSize = '8px';
    div.style.letterSpacing = '2px';
    div.style.color = `#${t.color.toString(16).padStart(6, '0')}`;
    div.style.opacity = '0.5';
    const lbl = new CSS2DObject(div);
    lbl.position.set(laneX, 9.2, laneZ);
    uniconnGroup.add(lbl);

    // Connection endpoint at the bottom (toward machine)
    const endGeo = new THREE.SphereGeometry(0.15, 8, 8);
    const endMat = new THREE.MeshBasicMaterial({
      color: t.color, transparent: true, opacity: 0.2,
      blending: THREE.AdditiveBlending, depthWrite: false,
    });
    const endDot = new THREE.Mesh(endGeo, endMat);
    endDot.position.set(laneX, 0.3, laneZ);
    uniconnGroup.add(endDot);

    // Connection line from endpoint to machine
    const connPts = [
      new THREE.Vector3(laneX, 0.3, laneZ),
      new THREE.Vector3(laneX, 0.3, laneZ - 3),
      new THREE.Vector3(machine.x, 0.3, machine.z + machine.h / 2),
    ];
    const connCurve = new THREE.QuadraticBezierCurve3(connPts[0], connPts[1], connPts[2]);
    const connGeo = new THREE.BufferGeometry().setFromPoints(connCurve.getPoints(20));
    const connLineMat = new THREE.LineDashedMaterial({
      color: t.color, transparent: true, opacity: 0.1,
      dashSize: 0.5, gapSize: 0.3,
    });
    const connLine = new THREE.Line(connGeo, connLineMat);
    connLine.computeLineDistances();
    uniconnGroup.add(connLine);

    // State indicator ring around the endpoint
    const ringGeo = new THREE.RingGeometry(0.2, 0.28, 16);
    const ringMat = new THREE.MeshBasicMaterial({
      color: t.color, transparent: true, opacity: 0,
      side: THREE.DoubleSide, blending: THREE.AdditiveBlending, depthWrite: false,
    });
    const ring = new THREE.Mesh(ringGeo, ringMat);
    ring.rotation.x = -Math.PI / 2;
    ring.position.set(laneX, 0.05, laneZ);
    uniconnGroup.add(ring);

    uniconnLanes.push({
      key: t.key, color: t.color,
      tube, tubeMat, iconMesh,
      endDot, endMat, ring, ringMat,
      connLine, connLineMat,
      laneX, laneZ,
      active: false,
      state: 'idle', // idle, listening, connected, error
      bytesThrough: 0,
      lastActivity: 0,
    });
  }

  // Gate indicator — shows which transport is primary vs secondary
  const gateLabelDiv = document.createElement('div');
  gateLabelDiv.className = 'uniconn-gate-label';
  gateLabelDiv.textContent = 'UNICONN · GATE';
  gateLabelDiv.style.fontSize = '7px';
  gateLabelDiv.style.letterSpacing = '3px';
  gateLabelDiv.style.opacity = '0.4';
  const gateLbl = new CSS2DObject(gateLabelDiv);
  gateLbl.position.set(baseX, 9.8, baseZ);
  uniconnGroup.add(gateLbl);

  // Gate line connecting all three lanes at the top
  const gateLinePts = [
    new THREE.Vector3(baseX - laneSpacing, 8.5, baseZ),
    new THREE.Vector3(baseX, 8.5, baseZ),
    new THREE.Vector3(baseX + laneSpacing, 8.5, baseZ),
  ];
  const gateGeo = new THREE.BufferGeometry().setFromPoints(gateLinePts);
  const gateLineMat = new THREE.LineBasicMaterial({
    color: 0x3060a0, transparent: true, opacity: 0.2,
  });
  const gateLine = new THREE.Line(gateGeo, gateLineMat);
  uniconnGroup.add(gateLine);

  uniconnGateIndicator = { label: gateLbl, line: gateLine };
}

// Set which transport is currently active (call from event handler)
export function setUniConnTransport(transportKey) {
  uniconnActiveTransport = transportKey;
  for (const lane of uniconnLanes) {
    lane.active = lane.key === transportKey;
  }
}

// Update a transport's connection state
export function setUniConnState(transportKey, newState) {
  const lane = uniconnLanes.find(l => l.key === transportKey);
  if (lane) lane.state = newState;
}

// Spawn a frame particle along the active transport lane
export function spawnUniConnFrame(transportKey, direction) {
  const lane = uniconnLanes.find(l => l.key === (transportKey || uniconnActiveTransport));
  if (!lane) return;
  if (uniconnFrames.length >= MAX_UNICONN_FRAMES) return;

  lane.lastActivity = Date.now();
  lane.bytesThrough += 1024; // One Value frame = 1024 bytes

  const frameGeo = new THREE.BoxGeometry(0.15, 0.15, 0.15);
  const frameMat = new THREE.MeshBasicMaterial({
    color: lane.color, transparent: true, opacity: 0.8,
    blending: THREE.AdditiveBlending, depthWrite: false,
  });
  const frameMesh = new THREE.Mesh(frameGeo, frameMat);
  // direction: 'in' = top to bottom (remote → machine), 'out' = bottom to top
  const startY = direction === 'out' ? 0.5 : 8.0;
  frameMesh.position.set(lane.laneX, startY, lane.laneZ);
  uniconnGroup.add(frameMesh);

  uniconnFrames.push({
    mesh: frameMesh, mat: frameMat,
    laneX: lane.laneX, laneZ: lane.laneZ,
    y: startY,
    direction: direction === 'out' ? 1 : -1,
    speed: UNICONN_FRAME_SPEED + Math.random() * 0.004,
  });
}

// Add a peer node (for UDP multicast or QUIC remote)
export function addUniConnPeer(addr, transportKey) {
  const lane = uniconnLanes.find(l => l.key === transportKey);
  if (!lane) return;

  // Small dot above the lane representing the remote peer
  const peerGeo = new THREE.SphereGeometry(0.1, 6, 6);
  const peerMat = new THREE.MeshBasicMaterial({
    color: lane.color, transparent: true, opacity: 0.5,
    blending: THREE.AdditiveBlending, depthWrite: false,
  });
  const peer = new THREE.Mesh(peerGeo, peerMat);
  const offset = uniconnPeerNodes.filter(p => p.transportKey === transportKey).length;
  peer.position.set(
    lane.laneX + (offset % 3 - 1) * 0.5,
    8.0 + 0.5 + Math.floor(offset / 3) * 0.5,
    lane.laneZ + 0.5,
  );
  uniconnGroup.add(peer);

  // Peer label
  const div = document.createElement('div');
  div.className = 'uniconn-peer-label';
  div.textContent = addr.length > 16 ? addr.slice(0, 16) + '…' : addr;
  div.style.fontSize = '6px';
  div.style.opacity = '0.4';
  const lbl = new CSS2DObject(div);
  lbl.position.copy(peer.position);
  lbl.position.y += 0.2;
  uniconnGroup.add(lbl);

  uniconnPeerNodes.push({ mesh: peer, label: lbl, addr, transportKey });
}

// Animation tick for UniConn visualization
export function animateUniConn(time, paused) {
  if (!uniconnGroup || paused) return;

  const now = Date.now();

  // Animate lane states
  for (const lane of uniconnLanes) {
    const isActive = lane.active;
    const targetTubeOpacity = isActive ? 0.4 : 0.15;
    lane.tubeMat.opacity += (targetTubeOpacity - lane.tubeMat.opacity) * 0.05;

    // Icon pulse for active transport
    if (isActive && lane.iconMesh) {
      lane.iconMesh.material.opacity = 0.4 + Math.sin(time * 2) * 0.15;
      lane.iconMesh.rotation.y = time * 0.5;
    } else if (lane.iconMesh) {
      lane.iconMesh.material.opacity += (0.2 - lane.iconMesh.material.opacity) * 0.05;
    }

    // Connection line brightness
    lane.connLineMat.opacity = isActive ? 0.25 : 0.08;

    // Endpoint state ring
    if (lane.state === 'connected') {
      lane.ringMat.opacity = 0.4 + Math.sin(time * 3) * 0.1;
      lane.endMat.opacity = 0.5;
    } else if (lane.state === 'listening') {
      lane.ringMat.opacity = 0.15 + Math.sin(time * 1.5) * 0.1;
      lane.endMat.opacity = 0.3;
    } else if (lane.state === 'error') {
      lane.ringMat.opacity = Math.sin(time * 6) > 0 ? 0.5 : 0;
      lane.ringMat.color.setHex(0xff4444);
      lane.endMat.opacity = 0.4;
    } else {
      lane.ringMat.opacity = Math.max(0, lane.ringMat.opacity - 0.02);
      lane.endMat.opacity = 0.15;
    }

    // Activity glow on recent data
    const recentActivity = now - lane.lastActivity < 500;
    if (recentActivity) {
      lane.tubeMat.opacity = Math.min(0.6, lane.tubeMat.opacity + 0.05);
    }
  }

  // Animate frame particles
  for (let i = uniconnFrames.length - 1; i >= 0; i--) {
    const f = uniconnFrames[i];
    f.y += f.direction * f.speed * 60;
    f.mesh.position.y = f.y;

    // Slight wobble
    f.mesh.position.x = f.laneX + Math.sin(f.y * 2 + time) * 0.08;

    // Fade out near ends
    if (f.direction < 0 && f.y < 1.0) {
      f.mat.opacity = Math.max(0, f.y - 0.3);
    } else if (f.direction > 0 && f.y > 7.5) {
      f.mat.opacity = Math.max(0, (8.5 - f.y));
    }

    // Remove when out of range
    if (f.y < 0 || f.y > 9) {
      uniconnGroup.remove(f.mesh);
      f.mesh.geometry.dispose();
      f.mat.dispose();
      uniconnFrames.splice(i, 1);
    }
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
    item.div = null;
    item.lbl = null;
    item.time = null;
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
