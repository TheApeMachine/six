import { scene } from './scene.js';
import { state } from './state.js';

export function clearScene() {
  for (const [, n] of state.nodes) {
    for (const bp of n.beam.breakParticles) {
      scene.remove(bp.mesh);
      bp.mesh.geometry.dispose();
      bp.mesh.material.dispose();
    }
    n.beam.breakParticles.length = 0;
    scene.remove(n.group);
  }
  for (const [, e] of state.edges) {
    scene.remove(e.mesh);
    scene.remove(e.labelSprite);
  }
  for (const [, a] of state.fieldArcs) {
    scene.remove(a.mesh);
    a.mesh.geometry.dispose();
    a.mesh.material.dispose();
  }
  if (state.eigenmodeRing) {
    scene.remove(state.eigenmodeRing);
    state.eigenmodeRing = null;
  }
  for (const f of state.floaters) scene.remove(f.sprite);
  for (const p of state.particles) scene.remove(p.mesh);
  for (const ep of state.edgeParticles) {
    scene.remove(ep.mesh);
    ep.trail.forEach((t) => {
      scene.remove(t);
    });
  }

  for (const fp of state.pipeline.flowParticles) {
    scene.remove(fp.mesh);
    fp.trail.forEach((t) => {
      scene.remove(t);
    });
  }
  state.pipeline.flowParticles.length = 0;

  state.nodes.clear();
  state.edges.clear();
  state.fieldArcs.clear();
  state.floaters.length = 0;
  state.particles.length = 0;
  state.edgeParticles.length = 0;
}
