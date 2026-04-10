import * as THREE from 'three';
import { state } from './state.js';
import { GF_LAYER, MAX_VIZ_TRIE_VISUALS, TRIE_COLUMN_RING_RADIUS } from './constants.js';
import { nodeAngle } from './nodes.js';
import { createPhaseDirectionArrow } from './gf_markers.js';
import { textMaterialFromText, textSprite } from './text.js';
import { updateStats } from './stats.js';

/*
Trie columns use the same angular rule as top-level nodes (nodeAngle): equal
spacing on a full 2π circle at a fixed local radius under the host.
*/
function repositionTrieColumns(node) {
  const total = node.tries.length;
  if (total === 0) return;

  const radius = TRIE_COLUMN_RING_RADIUS;
  for (let columnIdx = 0; columnIdx < total; columnIdx++) {
    const angle = nodeAngle(columnIdx, total);
    const column = node.tries[columnIdx].group;
    column.position.x = Math.cos(angle) * radius;
    column.position.z = Math.sin(angle) * radius;
    column.position.y = 0;
  }

  refreshTrieCouplingArcs(node);
}

/*
Coupling tubes are built from column positions; rebuild them after a layout change.
*/
function refreshTrieCouplingArcs(node) {
  const nodeId = node.data.id;
  const saved = [];
  for (const [aid, arc] of node.trieCouplings) {
    const parts = aid.split('|');
    if (parts.length !== 2) continue;
    const trieA = Number(parts[0]);
    const trieB = Number(parts[1]);
    saved.push({ trieA, trieB, coupling: arc.coupling });
    node.trieGroup.remove(arc.mesh);
    arc.mesh.geometry.dispose();
    arc.mesh.material.dispose();
  }
  node.trieCouplings.clear();
  for (const row of saved) {
    updateTrieCouplingArc(nodeId, row.trieA, row.trieB, row.coupling);
  }
}

function layoutTriePositions(rootVid, nodeList, edgeList) {
  const children = new Map();
  for (const e of edgeList || []) {
    if (!children.has(e.from)) children.set(e.from, []);
    children.get(e.from).push({ to: e.to, token: e.token });
  }
  for (const [, arr] of children) {
    arr.sort((a, b) => {
      if (a.token < b.token) return -1;
      if (a.token > b.token) return 1;
      if (a.to < b.to) return -1;
      if (a.to > b.to) return 1;
      return 0;
    });
  }
  const xPos = new Map();
  let leafCounter = 0;
  function place(nid) {
    const ch = children.get(nid);
    if (!ch || ch.length === 0) {
      const x = leafCounter++;
      xPos.set(nid, x);
      return [x, x];
    }
    let lo = Infinity;
    let hi = -Infinity;
    for (const { to } of ch) {
      const [a, b] = place(to);
      lo = Math.min(lo, a);
      hi = Math.max(hi, b);
    }
    const mid = (lo + hi) / 2;
    xPos.set(nid, mid);
    return [lo, hi];
  }
  if (!nodeList.length) return new Map();
  const haveRoot = nodeList.some((n) => n.vid === rootVid);
  const rid = haveRoot ? rootVid : nodeList[0].vid;
  place(rid);
  const xs = [...xPos.values()];
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const span = Math.max(maxX - minX, 1e-6);
  const maxDepth = Math.max(...nodeList.map((n) => n.depth), 0);
  const depthSpan = Math.max(maxDepth, 1);
  const hScale = Math.min(2.8 / span, 1.15);
  const vScale = Math.min(3.0 / depthSpan, 0.82);
  const pos = new Map();
  const midX = (minX + maxX) / 2;
  for (const n of nodeList) {
    const x = ((xPos.get(n.vid) ?? 0) - midX) * hScale;
    const y = -n.depth * vScale;
    pos.set(n.vid, { x, y, z: 0 });
  }
  return pos;
}

function clearTrieGraphLayers(trie) {
  if (!trie || !trie.graphGroup) return;
  while (trie.graphGroup.children.length) {
    const ch = trie.graphGroup.children[0];
    trie.graphGroup.remove(ch);
    if (ch.geometry && ch.geometry !== trie.sharedSphereGeo) ch.geometry.dispose();
    if (ch.material) {
      if (Array.isArray(ch.material)) ch.material.forEach((m) => {m.dispose();});
      else ch.material.dispose();
    }
  }
}

export function applyTrieGraphSnapshot(kadNode, trieIdx, payload) {
  const trie = kadNode.tries[trieIdx];
  if (!trie || !payload || !payload.nodes) return;

  clearTrieGraphLayers(trie);
  trie.pickMeshes.length = 0;
  trie.graphPayload = payload;
  trie.graphNodeByVid = new Map(payload.nodes.map((n) => [n.vid, n]));

  const positions = layoutTriePositions(payload.root_vid ?? 0, payload.nodes, payload.edges);
  if (!trie.sharedSphereGeo) {
    trie.sharedSphereGeo = new THREE.SphereGeometry(0.14, 10, 8);
  }
  const baseMat = new THREE.MeshPhongMaterial({
    color: 0x60d890,
    emissive: 0x102818,
    transparent: true,
    opacity: 0.85,
    shininess: 40,
  });
  for (const n of payload.nodes) {
    const p = positions.get(n.vid);
    if (!p) continue;
    const mesh = new THREE.Mesh(trie.sharedSphereGeo, baseMat.clone());
    mesh.position.set(p.x, p.y, p.z);
    mesh.userData.kind = 'trieVertex';
    mesh.userData.kadabraNodeId = kadNode.data.id;
    mesh.userData.trieIdx = trieIdx;
    mesh.userData.graphVertexVid = n.vid;
    trie.graphGroup.add(mesh);
    trie.pickMeshes.push(mesh);
  }

  const lineVerts = [];
  for (const e of payload.edges || []) {
    const a = positions.get(e.from);
    const b = positions.get(e.to);
    if (!a || !b) continue;
    lineVerts.push(a.x, a.y, a.z, b.x, b.y, b.z);
  }
  if (lineVerts.length) {
    const lg = new THREE.BufferGeometry();
    lg.setAttribute('position', new THREE.Float32BufferAttribute(lineVerts, 3));
    const lm = new THREE.LineBasicMaterial({ color: 0x408868, transparent: true, opacity: 0.9 });
    trie.graphGroup.add(new THREE.LineSegments(lg, lm));
  }

  if (trie.truncSprite) {
    trie.truncSprite.visible = !!payload.truncated;
    if (payload.truncated) {
      const msg = `truncated snapshot`;
      trie.truncSprite.material.dispose();
      trie.truncSprite.material = textMaterialFromText(msg, '#e8a840', 9);
    }
  }

  updateTrieAppearance(kadNode.data.id, trieIdx);
}

export function addTrieVisual(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  if (node.tries.length >= MAX_VIZ_TRIE_VISUALS) return;

  const columnIdx = node.tries.length;

  const trieGroup = new THREE.Group();

  const graphGroup = new THREE.Group();
  trieGroup.add(graphGroup);

  /*
  GF(257) is trie-local in the system — only this column’s ring + arrow (rotate
  field257Shell for live phase). Node-scale field is GF(8191) on the host torus.
  */
  const field257Geo = new THREE.RingGeometry(0.4, 0.47, 28);
  const field257Mat = new THREE.MeshBasicMaterial({
    color: GF_LAYER.trie.color,
    transparent: true,
    opacity: 0.48,
    side: THREE.DoubleSide,
  });
  const field257Ring = new THREE.Mesh(field257Geo, field257Mat);
  field257Ring.rotation.x = -Math.PI / 2;
  field257Ring.position.y = -0.2;

  const trieMidR = 0.435;
  const phaseArrow257 = createPhaseDirectionArrow(GF_LAYER.trie.color, 0.16, 0.052, 0.038);
  phaseArrow257.position.set(trieMidR, -0.19, 0);

  const field257Shell = new THREE.Group();
  field257Shell.add(field257Ring);
  field257Shell.add(phaseArrow257);
  trieGroup.add(field257Shell);

  const title = textSprite(`T${columnIdx}`, '#60d890', 12);
  title.position.y = 1.05;
  title.scale.set(1.5, 0.4, 1);
  trieGroup.add(title);

  const truncSprite = textSprite('', '#e8a840', 9);
  truncSprite.position.y = -0.22;
  truncSprite.scale.set(2.2, 0.3, 1);
  truncSprite.visible = false;
  trieGroup.add(truncSprite);

  node.trieGroup.add(trieGroup);
  node.tries.push({
    group: trieGroup,
    graphGroup,
    field257Shell,
    field257Ring,
    phaseArrow257,
    titleSprite: title,
    truncSprite,
    pickMeshes: [],
    sharedSphereGeo: null,
    graphPayload: null,
    graphNodeByVid: new Map(),
    insertFlash: 0,
  });
  repositionTrieColumns(node);
  updateStats();
}

/*
removeLastTrieVisual drops the highest-index column when NodeUpdated shrinks trie_count.
*/
export function removeLastTrieVisual(nodeId) {
  const node = state.nodes.get(nodeId);
  if (!node || node.tries.length === 0) return;

  const lastIdx = node.tries.length - 1;

  for (const [aid, arc] of [...node.trieCouplings.entries()]) {
    const parts = aid.split('|');
    if (parts.length !== 2) continue;
    const trieA = Number(parts[0]);
    const trieB = Number(parts[1]);
    if (trieA === lastIdx || trieB === lastIdx) {
      node.trieGroup.remove(arc.mesh);
      arc.mesh.geometry.dispose();
      arc.mesh.material.dispose();
      node.trieCouplings.delete(aid);
    }
  }

  const trie = node.tries[lastIdx];
  clearTrieGraphLayers(trie);
  trie.pickMeshes.length = 0;
  if (trie.sharedSphereGeo) {
    trie.sharedSphereGeo.dispose();
    trie.sharedSphereGeo = null;
  }

  node.trieGroup.remove(trie.group);
  trie.group.traverse((ch) => {
    if (ch.geometry) ch.geometry.dispose();
    if (ch.material) {
      if (Array.isArray(ch.material)) {
        for (const mat of ch.material) mat.dispose();
      }
      else {
        if (ch.material.map) ch.material.map.dispose();
        ch.material.dispose();
      }
    }
  });

  node.tries.pop();
  if (node.trieSignals.length > node.tries.length) node.trieSignals.length = node.tries.length;
  if (node.trieModes.length > node.tries.length) node.trieModes.length = node.tries.length;
  if (node.triePressures.length > node.tries.length) node.triePressures.length = node.tries.length;

  repositionTrieColumns(node);
  updateStats();
}

export function updateTrieCouplingArc(nodeId, trieA, trieB, coupling) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  if (trieA >= node.tries.length || trieB >= node.tries.length) return;

  const aid = `${Math.min(trieA, trieB)}|${Math.max(trieA, trieB)}`;
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

export function updateTrieAppearance(nodeId, trieIdx) {
  const node = state.nodes.get(nodeId);
  if (!node || trieIdx >= node.tries.length) return;

  const trie = node.tries[trieIdx];
  const mode = node.trieModes[trieIdx];
  const pressure = node.triePressures[trieIdx];
  const signal = node.trieSignals[trieIdx];

  if (!trie || !trie.pickMeshes || trie.pickMeshes.length === 0) return;

  const baseColor = mode?.aligned ? 0xf0a848 : (mode ? 0x607090 : 0x60d890);
  const emissiveColor = mode?.aligned ? 0x402810 : (mode ? 0x101820 : 0x102818);

  let scale = 1.0;
  if (pressure) {
    const pressureMag = Math.abs(pressure.decay) + Math.abs(pressure.learn);
    scale = 1.0 + Math.min(pressureMag * 2, 0.8);
  }

  let opacity = 0.82;
  if (signal) {
    opacity = 0.4 + Math.min(signal.surprisal / 8, 0.6);
  }

  for (const mesh of trie.pickMeshes) {
    mesh.material.color.setHex(baseColor);
    mesh.material.emissive.setHex(emissiveColor);
    mesh.scale.setScalar(scale);
    mesh.material.opacity = opacity;
  }
}
