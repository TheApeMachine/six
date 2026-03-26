/* ═══════════════════════════════════════════════════════════
   scene.js — Three.js scene, camera, renderer, controls,
              lighting, grid, and atmospheric effects
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { CSS2DRenderer } from 'three/addons/renderers/CSS2DRenderer.js';

export const scene = new THREE.Scene();
scene.fog = new THREE.FogExp2(0x060e1e, 0.012);

export const camera = new THREE.PerspectiveCamera(55, innerWidth / innerHeight, 0.1, 800);
camera.position.set(0, 42, 38);

// ── WebGL Renderer ─────────────────────────────────────────
export const renderer = new THREE.WebGLRenderer({
  antialias: true,
  canvas: document.getElementById('three-canvas') || undefined,
  alpha: false,
  powerPreference: 'high-performance',
});
renderer.setSize(innerWidth, innerHeight);
renderer.setPixelRatio(Math.min(devicePixelRatio, 1.5));
renderer.setClearColor(0x060e1e);
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.2;

if (!document.getElementById('three-canvas')) {
  renderer.domElement.id = 'three-canvas';
  document.body.insertBefore(renderer.domElement, document.body.firstChild);
}

// ── CSS2D Label Renderer ───────────────────────────────────
export const labelRenderer = new CSS2DRenderer();
labelRenderer.setSize(innerWidth, innerHeight);
labelRenderer.domElement.style.position = 'absolute';
labelRenderer.domElement.style.top = '0';
labelRenderer.domElement.style.pointerEvents = 'none';
labelRenderer.domElement.style.zIndex = '1';
document.body.appendChild(labelRenderer.domElement);

// ── Controls ───────────────────────────────────────────────
export const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.dampingFactor = 0.06;
controls.target.set(0, 2, 0);
controls.minDistance = 5;
controls.maxDistance = 200;
controls.maxPolarAngle = Math.PI * 0.85;

// ── Lighting ───────────────────────────────────────────────
const ambientLight = new THREE.AmbientLight(0x1a3060, 1.0);
scene.add(ambientLight);

const dirLight = new THREE.DirectionalLight(0x4a80c0, 0.5);
dirLight.position.set(15, 40, 20);
scene.add(dirLight);

const rimLight = new THREE.DirectionalLight(0x2244aa, 0.2);
rimLight.position.set(-10, 20, -15);
scene.add(rimLight);

// Subtle point lights at subsystem zones (will add more in architecture.js)
const centerGlow = new THREE.PointLight(0xffcc66, 0.15, 30, 2);
centerGlow.position.set(0, 3, 0);
scene.add(centerGlow);

// ── Grid ───────────────────────────────────────────────────
const gridHelper = new THREE.GridHelper(80, 80, 0x152540, 0x0c1830);
gridHelper.material.transparent = true;
gridHelper.material.opacity = 0.2;
scene.add(gridHelper);

const gridMajor = new THREE.GridHelper(80, 16, 0x1e3860, 0x1e3860);
gridMajor.material.transparent = true;
gridMajor.material.opacity = 0.1;
gridMajor.position.y = 0.01;
scene.add(gridMajor);

// ── Axis Lines ─────────────────────────────────────────────
const axisMat = new THREE.LineBasicMaterial({
  color: 0x3060a0,
  transparent: true,
  opacity: 0.12,
});
for (const [a, b] of [[[-40, 0, 0], [40, 0, 0]], [[0, 0, -30], [0, 0, 30]]]) {
  const g = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(...a),
    new THREE.Vector3(...b),
  ]);
  scene.add(new THREE.Line(g, axisMat));
}

// ── Ambient Particles ──────────────────────────────────────
const particleCount = 800;
const particleGeo = new THREE.BufferGeometry();
const positions = new Float32Array(particleCount * 3);
const particleSizes = new Float32Array(particleCount);

for (let i = 0; i < particleCount; i++) {
  positions[i * 3 + 0] = (Math.random() - 0.5) * 120;
  positions[i * 3 + 1] = Math.random() * 40;
  positions[i * 3 + 2] = (Math.random() - 0.5) * 120;
  particleSizes[i] = Math.random() * 1.5 + 0.3;
}

particleGeo.setAttribute('position', new THREE.BufferAttribute(positions, 3));
particleGeo.setAttribute('size', new THREE.BufferAttribute(particleSizes, 1));

const particleMat = new THREE.PointsMaterial({
  color: 0x4080c0,
  size: 0.08,
  transparent: true,
  opacity: 0.15,
  sizeAttenuation: true,
  blending: THREE.AdditiveBlending,
  depthWrite: false,
});

const ambientParticles = new THREE.Points(particleGeo, particleMat);
scene.add(ambientParticles);

// ── Groups ─────────────────────────────────────────────────
export const zoneGroup = new THREE.Group();
scene.add(zoneGroup);

export const foldLayer = new THREE.Group();
scene.add(foldLayer);

export const flowLayer = new THREE.Group();
scene.add(flowLayer);

export const valueGroup = new THREE.Group();
scene.add(valueGroup);

// ── Raycasting ─────────────────────────────────────────────
export const raycaster = new THREE.Raycaster();
export const mouseVec = new THREE.Vector2();

// ── Label Zoom ─────────────────────────────────────────────
const ZOOM_REF_DIST = 40;
let prevZoomScale = 1;

export function updateLabelZoom() {
  const dist = camera.position.distanceTo(controls.target);
  const scale = Math.min(Math.max(ZOOM_REF_DIST / dist, 0.8), 4.0);
  const rounded = Math.round(scale * 20) / 20;
  if (rounded !== prevZoomScale) {
    prevZoomScale = rounded;
    labelRenderer.domElement.style.setProperty('--label-zoom', rounded.toFixed(2));
  }
}

// ── Ambient Particle Animation ─────────────────────────────
export function updateAmbientParticles(time) {
  const pos = ambientParticles.geometry.attributes.position.array;
  for (let i = 0; i < particleCount; i++) {
    pos[i * 3 + 1] += Math.sin(time * 0.001 + i * 0.3) * 0.002;
    if (pos[i * 3 + 1] > 40) pos[i * 3 + 1] = 0;
  }
  ambientParticles.geometry.attributes.position.needsUpdate = true;
}

// ── Camera Fly-To ──────────────────────────────────────────
let flyTarget = null;
let flyStart = null;
let flyStartTime = 0;
const FLY_DURATION = 1200;

export function flyTo(targetPos, lookAtPos) {
  flyStart = {
    pos: camera.position.clone(),
    target: controls.target.clone(),
  };
  flyTarget = {
    pos: targetPos.clone(),
    target: lookAtPos.clone(),
  };
  flyStartTime = Date.now();
}

export function updateFlyAnimation() {
  if (!flyTarget) return;
  const elapsed = Date.now() - flyStartTime;
  const t = Math.min(elapsed / FLY_DURATION, 1);
  const ease = t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;

  camera.position.lerpVectors(flyStart.pos, flyTarget.pos, ease);
  controls.target.lerpVectors(flyStart.target, flyTarget.target, ease);

  if (t >= 1) {
    flyTarget = null;
    flyStart = null;
  }
}

// ── Resize ─────────────────────────────────────────────────
window.addEventListener('resize', () => {
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(innerWidth, innerHeight);
  labelRenderer.setSize(innerWidth, innerHeight);
});
