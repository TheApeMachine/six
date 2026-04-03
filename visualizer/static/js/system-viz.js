/* ═══════════════════════════════════════════════════════════
   system-viz.js — system-level orbit around the live machine
   Adds the runtime topology labels:
   Machine, Tokenizer, Control plane, Backend, CUDA, Metal, CPU.
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { valueGroup } from './scene.js';
import { SYS, resolveZoneKey } from './architecture.js';

const SYSTEM_RING_RADIUS = 9.3;
const SYSTEM_RING_THICKNESS = 0.22;
const SYSTEM_CHILD_RADIUS = 11.1;
const SYSTEM_Y = 3.0;

const SYSTEM_CORE = [
  {
    id: 'machine',
    label: 'Machine',
    detail: 'Read → Backend.Queue; Write → tokenizer',
    angle: -Math.PI / 2,
    radius: SYSTEM_RING_RADIUS,
    kind: 'machine',
    count: 1,
  },
  {
    id: 'stream',
    label: 'Tokenizer',
    detail: 'ring · pipe to Read',
    angle: Math.PI,
    radius: SYSTEM_RING_RADIUS,
    kind: 'stream',
    count: 1,
  },
  {
    id: 'controlplane',
    label: 'Control plane',
    detail: 'Kademlia · bucket LSM',
    angle: Math.PI / 2,
    radius: SYSTEM_RING_RADIUS,
    kind: 'controlplane',
    count: 1,
  },
  {
    id: 'backend',
    label: 'Backend',
    detail: 'queues · pool · batch · substrates',
    angle: 0,
    radius: SYSTEM_RING_RADIUS,
    kind: 'backend',
    count: 1,
  },
];

const SYSTEM_CHILDREN = [
  {
    id: 'cuda',
    label: 'Cuda',
    detail: 'NVIDIA',
    angle: -0.35,
    radius: SYSTEM_CHILD_RADIUS,
    kind: 'cuda',
  },
  {
    id: 'metal',
    label: 'Metal',
    detail: 'Apple GPU',
    angle: 0,
    radius: SYSTEM_CHILD_RADIUS + 0.35,
    kind: 'metal',
  },
  {
    id: 'cpu',
    label: 'CPU',
    detail: 'fallback',
    angle: 0.35,
    radius: SYSTEM_CHILD_RADIUS,
    kind: 'cpu',
  },
];

const SYSTEM_COLORS = {
  machine: 0xffcc66,
  stream: 0x5090d0,
  controlplane: 0xc080f0,
  emitter: 0x50c0a0,
  pool: 0x8fdc7a,
  exec: 0x9040c0,
  backend: 0xa070e0,
  cuda: 0x7fb8ff,
  metal: 0x6de0c0,
  cpu: 0xffb84d,
  ring: 0x4070a0,
};

const DEFAULT_SYSTEM_TOPOLOGY = {
  title: 'SYSTEM',
  subtitle: 'machine · tokenizer · Kademlia/LSM · backend',
  core: SYSTEM_CORE,
  hardware: SYSTEM_CHILDREN,
  links: [
    { from: 'machine', to: 'stream' },
    { from: 'stream', to: 'machine' },
    { from: 'stream', to: 'controlplane' },
    { from: 'controlplane', to: 'backend' },
    { from: 'machine', to: 'emitter' },
    { from: 'emitter', to: 'backend' },
    { from: 'backend', to: 'pool' },
    { from: 'pool', to: 'machine' },
    { from: 'backend', to: 'cuda' },
    { from: 'backend', to: 'metal' },
    { from: 'backend', to: 'cpu' },
  ],
};

function cloneNode(node) {
  return { ...node };
}

function normalizeNode(base, patch = null) {
  const node = cloneNode(base);
  if (!patch) return node;

  if (patch.label !== undefined) node.label = patch.label;
  if (patch.detail !== undefined) node.detail = patch.detail;
  if (patch.kind !== undefined) node.kind = patch.kind;
  if (patch.count !== undefined) node.count = Number(patch.count) || 0;
  if (patch.angle !== undefined) node.angle = patch.angle;
  if (patch.radius !== undefined) node.radius = patch.radius;
  return node;
}

function normalizeTopology(topology) {
  const src = topology && typeof topology === 'object' ? topology : {};
  const coreById = new Map((Array.isArray(src.core) ? src.core : []).map((node) => [String(node.id), node]));
  const hardwareById = new Map((Array.isArray(src.hardware) ? src.hardware : []).map((node) => [String(node.id), node]));

  return {
    title: src.title || DEFAULT_SYSTEM_TOPOLOGY.title,
    subtitle: src.subtitle || DEFAULT_SYSTEM_TOPOLOGY.subtitle,
    core: DEFAULT_SYSTEM_TOPOLOGY.core.map((node) => normalizeNode(node, coreById.get(node.id))),
    hardware: DEFAULT_SYSTEM_TOPOLOGY.hardware.map((node) => normalizeNode(node, hardwareById.get(node.id))),
    links: Array.isArray(src.links) && src.links.length > 0
      ? src.links.map((link) => ({
        from: String(link.from || ''),
        to: String(link.to || ''),
      })).filter((link) => link.from && link.to)
      : DEFAULT_SYSTEM_TOPOLOGY.links.map((link) => ({ ...link })),
  };
}

let systemTopology = normalizeTopology(DEFAULT_SYSTEM_TOPOLOGY);
let systemOrbitGroup = null;
let systemRing = null;
let systemCenterLabel = null;
const systemNodes = new Map();
const systemLinks = [];
const systemPulseTimers = new Map();
let systemBackendRanges = [];

function systemColor(kind) {
  return SYSTEM_COLORS[kind] || SYSTEM_COLORS.ring;
}

function disposeGroup(group) {
  if (!group) return;
  const geometries = new Set();
  group.traverse((obj) => {
    if (obj.geometry) geometries.add(obj.geometry);
    if (obj.material) {
      const mats = Array.isArray(obj.material) ? obj.material : [obj.material];
      for (const mat of mats) {
        if (mat && typeof mat.dispose === 'function') mat.dispose();
      }
    }
  });
  for (const geo of geometries) {
    geo.dispose();
  }
}

function clearTimers() {
  for (const timer of systemPulseTimers.values()) {
    clearTimeout(timer);
  }
  systemPulseTimers.clear();
}

function removeSystemOrbit() {
  if (!systemOrbitGroup) return;
  valueGroup.remove(systemOrbitGroup);
  disposeGroup(systemOrbitGroup);
  systemOrbitGroup = null;
  systemRing = null;
  systemCenterLabel = null;
  systemNodes.clear();
  systemLinks.length = 0;
  clearTimers();
}

export function setSystemTopology(topology) {
  systemTopology = normalizeTopology(topology);
}

function truncateDetail(detail, limit = 48) {
  const text = String(detail || '');
  if (text.length <= limit) return text;
  return `${text.slice(0, limit - 1)}…`;
}

function createLabel(title, detail, kind, count = 0) {
  const div = document.createElement('div');
  div.className = `system-orbit-label ${kind}`;
  if (count <= 0 && kind !== 'cpu') {
    div.classList.add('offline');
  } else {
    div.classList.add('online');
  }

  const titleSpan = document.createElement('span');
  titleSpan.className = 'system-orbit-title';
  titleSpan.textContent = title;
  div.appendChild(titleSpan);

  const detailSpan = document.createElement('span');
  detailSpan.className = 'system-orbit-detail';
  detailSpan.textContent = truncateDetail(detail);
  div.appendChild(detailSpan);

  return { div, titleSpan, detailSpan };
}

function placeNode(group, node, angle, radius, height = 0.28) {
  const x = Math.cos(angle) * radius;
  const z = Math.sin(angle) * radius;

  const label = createLabel(node.label, node.detail, node.kind, node.count);
  const lbl = new CSS2DObject(label.div);
  lbl.position.set(x, height, z);
  group.add(lbl);

  const pos = new THREE.Vector3(x, height, z);
  const entry = {
    id: node.id,
    kind: node.kind,
    label: node.label,
    detail: node.detail,
    baseDetail: node.detail,
    count: node.count || 0,
    angle,
    radius,
    pos,
    lbl,
    div: label.div,
    titleSpan: label.titleSpan,
    detailSpan: label.detailSpan,
  };
  systemNodes.set(node.id, entry);
  return entry;
}

function rebuildBackendRanges() {
  systemBackendRanges = [];
  let start = 0;

  for (const node of systemTopology.hardware) {
    const span = Math.max(0, Number(node.count) || 0);
    if (span <= 0) continue;

    systemBackendRanges.push({
      id: node.id,
      start,
      end: start + span - 1,
    });
    start += span;
  }

  if (systemBackendRanges.length === 0) {
    systemBackendRanges.push({ id: 'cpu', start: 0, end: 0 });
  }
}

function linkNodes(group, from, to, color, opacity = 0.28) {
  if (!from || !to) return;
  const geo = new THREE.BufferGeometry().setFromPoints([from.pos, to.pos]);
  const mat = new THREE.LineBasicMaterial({
    color,
    transparent: true,
    opacity,
  });
  const line = new THREE.Line(geo, mat);
  group.add(line);
  systemLinks.push({ line, from: from.id, to: to.id });
}

export function buildSystemOrbit() {
  removeSystemOrbit();
  rebuildBackendRanges();

  systemOrbitGroup = new THREE.Group();
  const anchor = SYS.machine || { x: 0, z: 0 };
  systemOrbitGroup.position.set(anchor.x, SYSTEM_Y, anchor.z);

  const ringGeo = new THREE.RingGeometry(
    SYSTEM_RING_RADIUS - SYSTEM_RING_THICKNESS,
    SYSTEM_RING_RADIUS + SYSTEM_RING_THICKNESS,
    160,
    1,
  );
  const ringMat = new THREE.MeshBasicMaterial({
    color: SYSTEM_COLORS.ring,
    transparent: true,
    opacity: 0.08,
    side: THREE.DoubleSide,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  systemRing = new THREE.Mesh(ringGeo, ringMat);
  systemRing.rotation.x = -Math.PI / 2;
  systemOrbitGroup.add(systemRing);

  const centerDiv = document.createElement('div');
  centerDiv.className = 'system-orbit-label center';
  const centerTitle = document.createElement('span');
  centerTitle.className = 'system-orbit-title';
  centerTitle.textContent = systemTopology.title;
  centerDiv.appendChild(centerTitle);
  const centerDetail = document.createElement('span');
  centerDetail.className = 'system-orbit-detail';
  centerDetail.textContent = systemTopology.subtitle;
  centerDiv.appendChild(centerDetail);
  systemCenterLabel = new CSS2DObject(centerDiv);
  systemCenterLabel.position.set(0, 0.5, 0);
  systemOrbitGroup.add(systemCenterLabel);

  for (const node of systemTopology.core) {
    placeNode(systemOrbitGroup, node, node.angle, node.radius, 0.26);
  }

  for (const node of systemTopology.hardware) {
    placeNode(systemOrbitGroup, node, node.angle, node.radius, 0.22);
  }

  for (const link of systemTopology.links) {
    const from = systemNodes.get(link.from);
    const to = systemNodes.get(link.to);
    const color = from ? systemColor(from.kind) : systemColor(link.from);
    const opacity = link.to === 'cpu' ? 0.16 : 0.24;
    linkNodes(systemOrbitGroup, from, to, color, opacity);
  }

  valueGroup.add(systemOrbitGroup);
}

export function pulseSystemNode(id, detail = '') {
  let key = resolveZoneKey(id);
  let node = systemNodes.get(key);

  // EXEC exists only in the 3D architecture; orbit still names that volume “Backend”.
  if (!node && key === 'exec') {
    key = 'backend';
    node = systemNodes.get(key);
  }

  if (!node) return;

  node.div.classList.add('active');
  if (detail) {
    node.detailSpan.textContent = truncateDetail(detail);
  }

  const existing = systemPulseTimers.get(node.id);
  if (existing) clearTimeout(existing);

  const timer = setTimeout(() => {
    node.div.classList.remove('active');
    node.detailSpan.textContent = node.baseDetail;
    systemPulseTimers.delete(node.id);
  }, 1200);

  systemPulseTimers.set(node.id, timer);
}

export function pulseSystemBackendSelection(index, detail = '') {
  if (!systemBackendRanges.length) {
    pulseSystemNode('backend', detail || `route #${index}`);
    return 'backend';
  }

  const routeIndex = Number(index);
  let nodeId = 'cpu';

  if (Number.isFinite(routeIndex)) {
    for (const range of systemBackendRanges) {
      if (routeIndex >= range.start && routeIndex <= range.end) {
        nodeId = range.id;
        break;
      }
    }
  }

  const routeDetail = detail || `route #${Number.isFinite(routeIndex) ? routeIndex : '?'}`;
  if (nodeId === 'backend') {
    pulseSystemNode('backend', routeDetail);
    return 'backend';
  }
  pulseSystemNode('backend', `backend · ${routeDetail}`);
  pulseSystemNode(nodeId, `${nodeId} · ${routeDetail}`);
  return nodeId;
}

export function animateSystemOrbit(time) {
  if (!systemOrbitGroup) return;
  systemOrbitGroup.position.y = SYSTEM_Y + Math.sin(time * 0.00045) * 0.08;
}
