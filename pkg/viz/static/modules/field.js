import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';

export function updateFieldArc(fromId, toId, coupling) {
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

export function pulseFieldArc(fromId, toId) {
  const aid = fromId < toId ? `${fromId}|${toId}` : `${toId}|${fromId}`;
  const arc = state.fieldArcs.get(aid);
  if (arc) arc.glow = 1.0;
}

export function showEigenmodeCluster(nodeId, modeCount, dominantEnergy) {
  const node = state.nodes.get(nodeId);
  if (!node) return;
  node.data.eigenmode = { modeCount, dominantEnergy, flash: 1.0 };
  rebuildEigenmodeRing();
}

export function rebuildEigenmodeRing() {
  if (state.eigenmodeRing) {
    scene.remove(state.eigenmodeRing);
    state.eigenmodeRing.traverse((c) => {
      if (c.geometry) c.geometry.dispose();
      if (c.material) c.material.dispose();
    });
    state.eigenmodeRing = null;
  }

  const eigenNodes = [...state.nodes.values()].filter((n) => n.data.eigenmode && n.data.eigenmode.dominantEnergy > 0);
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
