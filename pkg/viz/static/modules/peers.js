import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';
import { EDGE_COLORS } from './constants.js';
import { textSprite, textMaterialFromText } from './text.js';
import { updateStats } from './stats.js';

export function addEdge(fromId, toId) {
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

export function updateEdgeLabel(edge) {
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

export function pulseEdge(fromId, toId, color) {
  const eid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const edge = state.edges.get(eid);
  if (!edge) return;
  edge.activity = 1.0;
  edge.mesh.material.color.setHex(color || 0xa080e0);
}
