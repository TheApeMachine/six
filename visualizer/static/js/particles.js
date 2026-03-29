/* ═══════════════════════════════════════════════════════════
   particles.js — Particle data streams between subsystems
   Uses Three.js Points for GPU-accelerated flow particles
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { flowLayer } from './scene.js';
import { SYS, CONNS, resolveZoneKey } from './architecture.js';
import { pulseSystemNode } from './system-viz.js';

// ── Data Stream Labels (CSS2D text flowing between zones) ────
const activeDataStreams = [];
const streamPool = [];
const MAX_STREAMS = 60;

function getStreamLabel() {
  if (streamPool.length > 0) {
    const pooled = streamPool.pop();
    pooled.div.style.display = 'block';
    pooled.lbl.visible = true;
    return pooled;
  }
  const div = document.createElement('div');
  div.className = 'data-stream-label';
  const lbl = new CSS2DObject(div);
  flowLayer.add(lbl);
  return { div, lbl };
}

function releaseStreamLabel(s) {
  s.div.style.display = 'none';
  s.lbl.visible = false;
  streamPool.push({ div: s.div, lbl: s.lbl });
}

export function spawnDataStream(fromKey, toKey, text, streamClass = '', duration = 2800) {
  const from = SYS[resolveZoneKey(fromKey)], to = SYS[resolveZoneKey(toKey)];
  if (!from || !to) return;

  const displayText = (text || '').trim().slice(0, 32) || '·';
  const pathKey = `${resolveZoneKey(fromKey)}>${resolveZoneKey(toKey)}`;

  // Rate-limit streams on same path
  const existing = activeDataStreams.filter(s => s.pathKey === pathKey);
  if (existing.length > 0) {
    const newest = existing[existing.length - 1];
    const age = (Date.now() - newest.t0) / newest.dur;
    if (age < 0.12) return;
  }

  const { div, lbl } = getStreamLabel();
  div.className = `data-stream-label ${streamClass}`;
  div.textContent = displayText;
  pulseSystemNode(resolveZoneKey(fromKey), displayText);

  // Use arc path from CONNS if available
  const conn = CONNS.find(c => c.from === resolveZoneKey(fromKey) && c.to === resolveZoneKey(toKey));
  const startPos = from.center.clone();
  const endPos = to.center.clone();

  if (conn && conn.curve) {
    const curveStart = conn.curve.getPointAt(0);
    lbl.position.copy(curveStart);
  } else {
    startPos.y += 1.5;
    endPos.y += 1.5;
    lbl.position.copy(startPos);
  }

  activeDataStreams.push({
    lbl,
    div,
    pathKey,
    start: startPos,
    end: endPos,
    curve: conn && conn.curve ? conn.curve : null,
    t0: Date.now(),
    dur: duration,
  });

  while (activeDataStreams.length > MAX_STREAMS) {
    const old = activeDataStreams.shift();
    releaseStreamLabel(old);
  }
}

export function updateDataStreams() {
  if (activeDataStreams.length === 0) return; // Fast exit when idle
  const now = Date.now();

  for (let i = activeDataStreams.length - 1; i >= 0; i--) {
    const s = activeDataStreams[i];
    const t = (now - s.t0) / s.dur;

    if (t >= 1) {
      releaseStreamLabel(s);
      activeDataStreams.splice(i, 1);
      continue;
    }

    if (s.curve) {
      // Follow the Bézier curve
      const pt = s.curve.getPointAt(t);
      s.lbl.position.copy(pt);
    } else {
      s.lbl.position.lerpVectors(s.start, s.end, t);
    }

    // Fade in and out — avoid toFixed string allocation in hot path
    const opacity = t < 0.1 ? t * 10 : t > 0.85 ? (1 - t) * 6.667 : 1;
    s.div.style.opacity = opacity;
  }
}

export function clearDataStreams() {
  for (const s of activeDataStreams) {
    releaseStreamLabel(s);
  }
  activeDataStreams.length = 0;
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

    const connKey = `${conn.from}->${conn.to}`;
    particleSystems.push({
      connKey,
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
  const connKey = `${resolveZoneKey(fromKey)}->${resolveZoneKey(toKey)}`;
  const ps = particleSystems.find(p => p.connKey === connKey);
  if (ps) {
    if (!ps.active) _activeFlowCount++;
    ps.active = true;
    ps.fadeTimer = Date.now();
  }
}

let _activeFlowCount = 0;

export function updateFlowParticles(time) {
  if (_activeFlowCount === 0) return; // Fast exit when idle

  const now = Date.now();
  let stillActive = 0;

  for (const ps of particleSystems) {
    if (!ps.active) continue;

    // Auto-deactivate after 3 seconds of no new activation
    if (now - ps.fadeTimer > 3000) {
      ps.active = false;
      ps.points.material.opacity = 0;
      continue;
    }

    stillActive++;
    ps.points.material.opacity = 0.3;

    const timeOffset = time * 0.001 * ps.speed;
    for (let i = 0; i < PARTICLES_PER_PATH; i++) {
      const t = (ps.offsets[i] + timeOffset) % 1;
      const pt = ps.curve.getPointAt(t);

      ps.positions[i * 3 + 0] = pt.x;
      ps.positions[i * 3 + 1] = pt.y;
      ps.positions[i * 3 + 2] = pt.z;
    }

    ps.geo.attributes.position.needsUpdate = true;
  }

  _activeFlowCount = stillActive;
}
