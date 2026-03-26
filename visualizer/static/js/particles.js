/* ═══════════════════════════════════════════════════════════
   particles.js — Particle data streams between subsystems
   Uses Three.js Points for GPU-accelerated flow particles
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { flowLayer } from './scene.js';
import { SYS, CONNS } from './architecture.js';

// ── Data Stream Labels (CSS2D text flowing between zones) ────
const dataStreams = [];
const MAX_STREAMS = 60;

export function spawnDataStream(fromKey, toKey, text, streamClass = '', duration = 2800) {
  const from = SYS[fromKey], to = SYS[toKey];
  if (!from || !to) return;

  const displayText = (text || '').trim().slice(0, 32) || '·';
  const pathKey = `${fromKey}>${toKey}`;

  // Rate-limit streams on same path
  const existing = dataStreams.filter(s => s.pathKey === pathKey);
  if (existing.length > 0) {
    const newest = existing[existing.length - 1];
    const age = (Date.now() - newest.t0) / newest.dur;
    if (age < 0.12) return;
  }

  const div = document.createElement('div');
  div.className = `data-stream-label ${streamClass}`;
  div.textContent = displayText;

  const lbl = new CSS2DObject(div);

  // Use arc path from CONNS if available
  const conn = CONNS.find(c => c.from === fromKey && c.to === toKey);
  const startPos = from.center.clone();
  const endPos = to.center.clone();

  if (conn.curve) {
    const curveStart = conn.curve.getPointAt(0);
    lbl.position.copy(curveStart);
  } else {
    startPos.y += 1.5;
    endPos.y += 1.5;
    lbl.position.copy(startPos);
  }

  flowLayer.add(lbl);

  dataStreams.push({
    lbl,
    div,
    pathKey,
    start: startPos,
    end: endPos,
    curve: conn ? conn.curve : null,
    t0: Date.now(),
    dur: duration,
  });

  while (dataStreams.length > MAX_STREAMS) {
    const old = dataStreams.shift();
    flowLayer.remove(old.lbl);
  }
}

export function updateDataStreams() {
  const now = Date.now();

  for (let i = dataStreams.length - 1; i >= 0; i--) {
    const s = dataStreams[i];
    const t = (now - s.t0) / s.dur;

    if (t >= 1) {
      flowLayer.remove(s.lbl);
      dataStreams.splice(i, 1);
      continue;
    }

    if (s.curve) {
      // Follow the Bézier curve
      const pt = s.curve.getPointAt(t);
      s.lbl.position.copy(pt);
    } else {
      s.lbl.position.lerpVectors(s.start, s.end, t);
    }

    // Fade in and out
    s.div.style.opacity = t < 0.1
      ? (t / 0.1).toFixed(2)
      : t > 0.85
        ? ((1 - t) / 0.15).toFixed(2)
        : '1';
  }
}

export function clearDataStreams() {
  for (const s of dataStreams) {
    flowLayer.remove(s.lbl);
  }
  dataStreams.length = 0;
}

// ── GPU Particle Streams ───────────────────────────────────
// Continuous flow of tiny dot-particles along connection paths.
const particleSystems = [];
const PARTICLES_PER_PATH = 30;

export function buildFlowParticles() {
  for (const conn of CONNS) {
    if (!conn.curve) continue;

    const positions = new Float32Array(PARTICLES_PER_PATH * 3);
    const offsets = new Float32Array(PARTICLES_PER_PATH);

    for (let i = 0; i < PARTICLES_PER_PATH; i++) {
      offsets[i] = i / PARTICLES_PER_PATH;
    }

    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.BufferAttribute(positions, 3));

    const fromSys = SYS[conn.from];
    const mat = new THREE.PointsMaterial({
      color: fromSys ? fromSys.color : 0x4080c0,
      size: 0.12,
      transparent: true,
      opacity: 0.25,
      sizeAttenuation: true,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });

    const points = new THREE.Points(geo, mat);
    flowLayer.add(points);

    particleSystems.push({
      points,
      geo,
      positions,
      offsets,
      curve: conn.curve,
      speed: 0.08 + Math.random() * 0.04,
      active: false,
    });
  }
}

export function activateFlowParticles(fromKey, toKey) {
  const connIdx = CONNS.findIndex(c => c.from === fromKey && c.to === toKey);
  if (connIdx >= 0 && particleSystems[connIdx]) {
    particleSystems[connIdx].active = true;
    particleSystems[connIdx].fadeTimer = Date.now();
  }
}

export function updateFlowParticles(time) {
  const now = Date.now();

  for (const ps of particleSystems) {
    if (!ps.active) continue;

    // Auto-deactivate after 3 seconds of no new activation
    if (now - ps.fadeTimer > 3000) {
      ps.active = false;
      ps.points.material.opacity = 0;
      continue;
    }

    ps.points.material.opacity = 0.3;

    for (let i = 0; i < PARTICLES_PER_PATH; i++) {
      const t = (ps.offsets[i] + time * 0.001 * ps.speed) % 1;
      const pt = ps.curve.getPointAt(t);

      ps.positions[i * 3 + 0] = pt.x;
      ps.positions[i * 3 + 1] = pt.y;
      ps.positions[i * 3 + 2] = pt.z;
    }

    ps.geo.attributes.position.needsUpdate = true;
  }
}
