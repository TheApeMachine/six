import * as THREE from 'three';
import { scene, camera, renderer, controls, cameraFocus } from './scene.js';
import { state } from './state.js';
import { EDGE_COLORS } from './constants.js';
import { lerpColor } from './utils.js';
import { renderNodeStats } from './nodes.js';
import { updateTimeline } from './timeline.js';
import { updateStats } from './stats.js';
import { animatePipeline } from './pipeline.js';

let lastTime = performance.now();
let frameCount = 0;
let fpsTime = 0;
let statsRedrawTimer = 0;

/*
animate drives damping, pulses, beam FX, edge tubes, particles, camera focus,
and periodic stat HUD refresh. No spinning geometry — only data-driven motion.
*/
export function animate(now) {
  requestAnimationFrame(animate);
  const dt = now - lastTime;
  lastTime = now;
  frameCount++;
  fpsTime += dt;
  if (fpsTime > 1000) {
    document.getElementById('stat-fps').textContent = frameCount;
    frameCount = 0;
    fpsTime = 0;
  }

  if (state.statsDirty) {
    state.statsDirty = false;
    updateStats();
  }

  controls.update();

  /*
  Camera focus lerp — smooth transition when user clicks an object.
  */
  if (cameraFocus.active) {
    camera.position.lerp(cameraFocus.target, 0.04);
    controls.target.lerp(cameraFocus.lookAt, 0.04);
    if (camera.position.distanceTo(cameraFocus.target) < 0.1) {
      cameraFocus.active = false;
    }
  }

  for (const [, node] of state.nodes) {
    node.group.position.lerp(node.targetPos, 0.04);

    /*
    Glow decay — edge brightness fades back to normal after activity pulse.
    */
    if (node.glowIntensity > 0.01) {
      node.glowIntensity *= 0.95;
      node.core.material.opacity = 0.8 + node.glowIntensity * 0.2;
    }

    if (node.pulseAlpha > 0.01) {
      node.pulseScale += 0.03;
      node.pulseAlpha *= 0.95;
      node.pulseRing.material.opacity = node.pulseAlpha;
      node.pulseRing.scale.setScalar(node.pulseScale);
    } else {
      node.pulseRing.material.opacity = 0;
      node.pulseScale = 1.0;
      node.pulseRing.scale.setScalar(1.0);
    }

    for (const trie of node.tries) {
      if (trie.insertFlash > 0.01 && trie.pickMeshes && trie.pickMeshes.length) {
        trie.insertFlash *= 0.93;
        const em = lerpColor(0x102818, 0x40f080, trie.insertFlash);
        for (const mesh of trie.pickMeshes) {
          mesh.material.emissive.setHex(em);
        }
      }
    }

    for (const [, arc] of node.trieCouplings) {
      if (arc.glow > 0.01) {
        arc.glow *= 0.96;
        arc.mesh.material.opacity = Math.min(arc.coupling * 0.6 + arc.glow * 0.3, 0.7);
        arc.mesh.material.color.setHex(arc.glow > 0.4 ? 0xc09040 : 0xc08030);
      } else {
        arc.mesh.material.opacity = Math.max(arc.mesh.material.opacity - 0.003, Math.min(arc.coupling * 0.3, 0.25));
      }
    }

    const beam = node.beam;
    for (let ri = beam.rays.length - 1; ri >= 0; ri--) {
      const ray = beam.rays[ri];
      ray.t += 0.025;
      if (ray.t < 1.0) {
        ray.mesh.material.opacity = Math.sin(ray.t * Math.PI) * 0.7;
        ray.mesh.material.color.setHex(ray.t > 0.5 ? 0x4a80c0 : 0x3a6090);
      } else {
        node.group.remove(ray.mesh);
        ray.mesh.geometry.dispose();
        ray.mesh.material.dispose();
        beam.rays.splice(ri, 1);
      }
    }

    for (let hi = beam.hypotheses.length - 1; hi >= 0; hi--) {
      const hyp = beam.hypotheses[hi];
      hyp.angle += 0.02;
      hyp.fade -= 0.0008;
      const orbitR = 2.2;
      hyp.mesh.position.set(
        Math.cos(hyp.angle) * orbitR,
        1.8 + Math.sin(hyp.angle * 2) * 0.3,
        Math.sin(hyp.angle) * orbitR,
      );
      hyp.mesh.material.opacity = Math.max(0, hyp.score * hyp.fade * 0.8);
      if (hyp.fade <= 0) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
        beam.hypotheses.splice(hi, 1);
      }
    }

    for (let bi = beam.breakParticles.length - 1; bi >= 0; bi--) {
      const bp = beam.breakParticles[bi];
      bp.mesh.position.add(bp.velocity);
      bp.velocity.y -= 0.001;
      bp.life -= 0.02;
      bp.mesh.material.opacity = Math.max(0, bp.life);
      bp.mesh.rotation.x += 0.1;
      bp.mesh.rotation.z += 0.08;
      if (bp.life <= 0) {
        scene.remove(bp.mesh);
        bp.mesh.geometry.dispose();
        bp.mesh.material.dispose();
        beam.breakParticles.splice(bi, 1);
      }
    }

    if (beam.convergeRing) {
      const cr = beam.convergeRing;
      cr.scale += 0.04;
      cr.fade -= 0.006;
      cr.mesh.scale.setScalar(cr.scale);
      cr.mesh.material.opacity = Math.max(0, cr.fade * 0.9);
      if (cr.fade <= 0) {
        node.group.remove(cr.mesh);
        cr.mesh.geometry.dispose();
        cr.mesh.material.dispose();
        beam.convergeRing = null;
      }
    }
  }

  for (const [, edge] of state.edges) {
    if (edge.activity > 0) {
      edge.activity = Math.max(0, edge.activity - 0.02);
      edge.mesh.material.opacity = 0.15 + edge.activity * 0.5;
    } else {
      edge.mesh.material.opacity = Math.max(edge.mesh.material.opacity - 0.005, 0.1);
      edge.mesh.material.color.setHex(EDGE_COLORS.peer);
    }

    const nA = state.nodes.get(edge.from);
    const nB = state.nodes.get(edge.to);
    if (nA && nB) {
      const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
      mid.y += 2;
      const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
      const newGeo = new THREE.TubeGeometry(curve, 20, 0.04 + edge.activity * 0.06, 4, false);
      edge.mesh.geometry.dispose();
      edge.mesh.geometry = newGeo;
    }
  }

  for (const [, arc] of state.fieldArcs) {
    if (arc.glow > 0) {
      arc.glow = Math.max(0, arc.glow - 0.02);
      arc.mesh.material.opacity = Math.min(arc.coupling * 0.5 + arc.glow * 0.4, 0.6);
      arc.mesh.material.color.setHex(arc.glow > 0.3 ? 0xc09040 : 0xc08030);
    } else {
      arc.mesh.material.opacity = Math.max(arc.mesh.material.opacity - 0.003, Math.min(arc.coupling * 0.25, 0.2));
    }
    const nA = state.nodes.get(arc.from);
    const nB = state.nodes.get(arc.to);
    if (nA && nB) {
      const dist = nA.group.position.distanceTo(nB.group.position);
      const arcHeight = Math.max(1.5, dist * 0.25 + arc.coupling * 1.5);
      const mid = nA.group.position.clone().add(nB.group.position).multiplyScalar(0.5);
      mid.y += arcHeight;
      const curve = new THREE.QuadraticBezierCurve3(nA.group.position.clone(), mid, nB.group.position.clone());
      const newGeo = new THREE.TubeGeometry(curve, 24, 0.02 + arc.coupling * 0.03, 4, false);
      arc.mesh.geometry.dispose();
      arc.mesh.geometry = newGeo;
    }
  }

  for (let i = state.floaters.length - 1; i >= 0; i--) {
    const f = state.floaters[i];
    f.sprite.position.add(f.velocity);
    f.life -= f.decay;
    f.sprite.material.opacity = Math.max(0, f.life);
    if (f.life <= 0) {
      scene.remove(f.sprite);
      f.sprite.material.map?.dispose();
      f.sprite.material.dispose();
      state.floaters.splice(i, 1);
    }
  }

  for (let i = state.particles.length - 1; i >= 0; i--) {
    const p = state.particles[i];
    p.t += p.speed;
    if (p.t >= 1) {
      scene.remove(p.mesh);
      state.particles.splice(i, 1);
      continue;
    }
    p.mesh.position.lerpVectors(p.from, p.to, p.t);
    p.mesh.position.y += Math.sin(p.t * Math.PI) * 2;
    p.mesh.material.opacity = 1 - p.t * 0.7;
  }

  for (let i = state.edgeParticles.length - 1; i >= 0; i--) {
    const ep = state.edgeParticles[i];
    ep.t += ep.speed;

    if (ep.t >= 1) {
      scene.remove(ep.mesh);
      ep.trail.forEach((t) => {
        scene.remove(t);
      });
      state.edgeParticles.splice(i, 1);
      continue;
    }

    const nA = state.nodes.get(ep.from);
    const nB = state.nodes.get(ep.to);
    if (!nA || !nB) {
      scene.remove(ep.mesh);
      ep.trail.forEach((t) => {
        scene.remove(t);
      });
      state.edgeParticles.splice(i, 1);
      continue;
    }

    const posA = nA.group.position;
    const posB = nB.group.position;
    const mid = posA.clone().add(posB).multiplyScalar(0.5);
    mid.y += 2;

    const t1 = ep.t;
    const omt = 1 - t1;
    ep.mesh.position.set(
      omt * omt * posA.x + 2 * omt * t1 * mid.x + t1 * t1 * posB.x,
      omt * omt * posA.y + 2 * omt * t1 * mid.y + t1 * t1 * posB.y,
      omt * omt * posA.z + 2 * omt * t1 * mid.z + t1 * t1 * posB.z,
    );
    ep.mesh.material.opacity = 1 - ep.t * 0.5;

    for (let ti = 0; ti < ep.trail.length; ti++) {
      const tt = Math.max(0, t1 - (ti + 1) * 0.04);
      const omt2 = 1 - tt;
      ep.trail[ti].position.set(
        omt2 * omt2 * posA.x + 2 * omt2 * tt * mid.x + tt * tt * posB.x,
        omt2 * omt2 * posA.y + 2 * omt2 * tt * mid.y + tt * tt * posB.y,
        omt2 * omt2 * posA.z + 2 * omt2 * tt * mid.z + tt * tt * posB.z,
      );
      ep.trail[ti].material.opacity = (0.4 - ti * 0.1) * (1 - ep.t * 0.5);
    }
  }

  animatePipeline();

  statsRedrawTimer += dt;
  if (statsRedrawTimer > 1000) {
    statsRedrawTimer = 0;
    for (const [, node] of state.nodes) renderNodeStats(node);
  }

  updateTimeline();
  renderer.render(scene, camera);
}
