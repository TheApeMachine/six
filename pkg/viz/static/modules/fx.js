import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';
import { textSprite } from './text.js';

export function spawnFloater(position, text, color, direction) {
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

export function spawnEdgeParticle(fromId, toId, color) {
  const nA = state.nodes.get(fromId);
  const nB = state.nodes.get(toId);
  if (!nA || !nB) return;

  const geo = new THREE.SphereGeometry(0.08, 6, 6);
  const mat = new THREE.MeshBasicMaterial({ color: color || 0xa080e0, transparent: true });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.position.copy(nA.group.position);
  scene.add(mesh);

  const trail = [];
  for (let i = 0; i < 4; i++) {
    const tGeo = new THREE.SphereGeometry(0.04 - i * 0.008, 4, 4);
    const tMat = new THREE.MeshBasicMaterial({ color: color || 0xa080e0, transparent: true, opacity: 0.4 - i * 0.1 });
    const tMesh = new THREE.Mesh(tGeo, tMat);
    tMesh.position.copy(nA.group.position);
    scene.add(tMesh);
    trail.push(tMesh);
  }

  state.edgeParticles.push({
    mesh, trail,
    from: fromId, to: toId,
    t: 0, speed: 0.012 + Math.random() * 0.008,
  });
}
