/* ═══════════════════════════════════════════════════════════
   value-viz.js — 3D Value structure visualization
   Shows the 128-word (8192-bit) Value as a detailed
   ring of bit-cells with color-coded regions.
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { valueGroup } from './scene.js';
import { SYS } from './architecture.js';

const TOTAL_BITS = 8192;

// Region boundaries (from primitive/value.go)
const REGIONS = [
  { name: 'DATA',        startBit: 0,    endBit: 3840, color: new THREE.Color(0x4090e0), cssClass: 'data' },
  { name: 'INSTRUCTION', startBit: 3840, endBit: 3844, color: new THREE.Color(0xffcc66), cssClass: 'instr' },
  { name: 'AFFINITY',    startBit: 4096, endBit: 4352, color: new THREE.Color(0x50d0e0), cssClass: 'affinity' },
  { name: 'PROGRAM',     startBit: 4352, endBit: 4608, color: new THREE.Color(0xa070e0), cssClass: 'program' },
  { name: 'LINK',        startBit: 4608, endBit: 4864, color: new THREE.Color(0x50c090), cssClass: 'link' },
  { name: 'GOSSIP',      startBit: 4864, endBit: 5120, color: new THREE.Color(0x50a0a0), cssClass: 'gossip' },
  { name: 'TTL',         startBit: 5120, endBit: 5128, color: new THREE.Color(0xe06050), cssClass: 'ttl' },
];

function getRegionForBit(bit) {
  for (const r of REGIONS) {
    if (bit >= r.startBit && bit < r.endBit) return r;
  }
  return null;
}

// ── Value Ring ─────────────────────────────────────────────
// We represent the Value as a torus-like ring of bit cells around the
// chamber zone. Each "cell" is a small cube on the ring surface.

const RING_RADIUS = 5.5;
const RING_Y = 3.0;
const CELL_SIZE = 0.06;
const DISPLAY_BITS = 512; // Show every 16th bit for performance
const BIT_STEP = Math.floor(TOTAL_BITS / DISPLAY_BITS);

let cellMeshes = [];
const regionLabels = [];
let valueRingGroup = null;

export function buildValueRing() {
  if (valueRingGroup) {
    valueGroup.remove(valueRingGroup);
  }

  valueRingGroup = new THREE.Group();

  const chamber = SYS.chamber;
  valueRingGroup.position.set(chamber.x, RING_Y, chamber.z);

  // Create ring of bit cells
  const cellGeo = new THREE.BoxGeometry(CELL_SIZE, CELL_SIZE * 2, CELL_SIZE);
  cellMeshes = [];

  for (let i = 0; i < DISPLAY_BITS; i++) {
    const actualBit = i * BIT_STEP;
    const region = getRegionForBit(actualBit);

    const angle = (i / DISPLAY_BITS) * Math.PI * 2;

    // Vary the ring radius slightly per region for visual distinction
    const regionOffset = region ? REGIONS.indexOf(region) * 0.12 : 0;
    const r = RING_RADIUS + regionOffset;

    const mat = new THREE.MeshBasicMaterial({
      color: region ? region.color : new THREE.Color(0x2040a0),
      transparent: true,
      opacity: 0.15,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });

    const cell = new THREE.Mesh(cellGeo, mat);
    cell.position.set(
      Math.cos(angle) * r,
      0,
      Math.sin(angle) * r,
    );
    cell.lookAt(0, 0, 0);

    cellMeshes.push({ mesh: cell, bit: actualBit, region, baseOpacity: 0.15 });
    valueRingGroup.add(cell);
  }

  // Region boundary lines (arcs)
  const ringOutlineGeo = new THREE.RingGeometry(RING_RADIUS - 0.15, RING_RADIUS + 0.15, 128, 1);
  const ringOutlineMat = new THREE.MeshBasicMaterial({
    color: 0x4080c0,
    transparent: true,
    opacity: 0.06,
    side: THREE.DoubleSide,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  const ringOutline = new THREE.Mesh(ringOutlineGeo, ringOutlineMat);
  ringOutline.rotation.x = -Math.PI / 2;
  valueRingGroup.add(ringOutline);

  // Region labels
  for (const region of REGIONS) {
    const midBit = (region.startBit + region.endBit) / 2;
    const midIdx = midBit / BIT_STEP;
    const angle = (midIdx / DISPLAY_BITS) * Math.PI * 2;
    const labelR = RING_RADIUS + 1.2 + REGIONS.indexOf(region) * 0.12;

    const div = document.createElement('div');
    div.className = `region-label ${region.cssClass}`;
    div.textContent = region.name;

    const lbl = new CSS2DObject(div);
    lbl.position.set(
      Math.cos(angle) * labelR,
      0.3,
      Math.sin(angle) * labelR,
    );
    valueRingGroup.add(lbl);
    regionLabels.push(lbl);
  }

  // Center label
  const centerDiv = document.createElement('div');
  centerDiv.className = 'region-label instr';
  centerDiv.textContent = 'VALUE · 8192 BITS';
  centerDiv.style.fontSize = 'calc(8px * var(--label-zoom, 1))';
  const centerLbl = new CSS2DObject(centerDiv);
  centerLbl.position.set(0, 0.5, 0);
  valueRingGroup.add(centerLbl);

  valueGroup.add(valueRingGroup);
}

// ── Update Value Display ───────────────────────────────────
// Accepts telemetry data and updates which bits are "lit"
// Map raw 1024-byte LE wire frame to sampled bit cells (no JSON, no object churn).
export function updateValueFromBinaryBuffer(buf) {
  if (!cellMeshes.length || !(buf instanceof ArrayBuffer) || buf.byteLength < 1024) return;
  const u8 = new Uint8Array(buf);

  for (const cell of cellMeshes) {
    const bit = cell.bit;
    const byteIdx = bit >>> 3;
    const bitInByte = bit & 7;
    const active = ((u8[byteIdx] >> bitInByte) & 1) === 1;

    cell.mesh.material.opacity = active ? 0.75 + Math.random() * 0.2 : 0.08;
    cell.mesh.scale.y = active ? 1.7 : 0.55;
  }
}

export function updateValueDisplay(data) {
  if (!cellMeshes.length) return;

  const dataPop = data.dataPop || 0;
  const operandPop = data.operandPop || 0;
  const affinityPop = data.affinityPop || 0;

  // Simulate active bits based on popcount data
  for (const cell of cellMeshes) {
    let active = false;
    let intensity = 0.15;

    if (cell.region) {
      const rName = cell.region.name;
      if (rName === 'DATA' && dataPop > 0) {
        // Probabilistic: light up a fraction proportional to popcount
        active = Math.random() < (dataPop / 3840) * 2;
        intensity = active ? 0.5 + Math.random() * 0.4 : 0.1;
      } else if (rName === 'INSTRUCTION') {
        active = true;
        intensity = 0.8;
      } else if (rName === 'AFFINITY' && affinityPop > 0) {
        active = Math.random() < (affinityPop / 64) * 1.5;
        intensity = active ? 0.4 + Math.random() * 0.3 : 0.08;
      } else if (rName === 'PROGRAM' && operandPop > 0) {
        active = Math.random() < (operandPop / 64) * 1.5;
        intensity = active ? 0.4 + Math.random() * 0.3 : 0.08;
      }
    }

    cell.mesh.material.opacity = intensity;
    cell.mesh.scale.y = active ? 1.5 + Math.random() : 0.6;
  }
}

// ── Slow Rotation ──────────────────────────────────────────
export function animateValueRing(time) {
  if (!valueRingGroup) return;
  valueRingGroup.rotation.y = time * 0.0001;

  // Gentle bob
  valueRingGroup.position.y = RING_Y + Math.sin(time * 0.0005) * 0.15;
}
