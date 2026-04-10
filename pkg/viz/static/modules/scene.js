import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { NODE_RADIUS } from './constants.js';

export const canvas = document.getElementById('canvas');
export const renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
renderer.setSize(window.innerWidth, window.innerHeight);
renderer.setClearColor(0x0a0e14);
renderer.toneMapping = THREE.NoToneMapping;

export const scene = new THREE.Scene();

export const camera = new THREE.PerspectiveCamera(50, window.innerWidth / window.innerHeight, 0.1, 500);
camera.position.set(0, 35, 55);

export const controls = new OrbitControls(camera, canvas);
controls.enableDamping = true;
controls.dampingFactor = 0.07;
controls.target.set(0, 2, 0);
controls.minDistance = 8;
controls.maxDistance = 150;
controls.maxPolarAngle = Math.PI * 0.85;

/*
Camera focus target — when set, animate() lerps towards it. Cleared on user drag.
*/
export const cameraFocus = { active: false, target: new THREE.Vector3(), lookAt: new THREE.Vector3(), distance: 20 };

controls.addEventListener('start', () => {
  cameraFocus.active = false;
});

/*
Blueprint-style lighting: cool directional + soft ambient, no harsh shadows.
*/
scene.add(new THREE.AmbientLight(0x1a2030, 2.0));
const mainLight = new THREE.DirectionalLight(0x4060a0, 0.6);
mainLight.position.set(20, 40, 20);
scene.add(mainLight);
const fillLight = new THREE.DirectionalLight(0x203050, 0.3);
fillLight.position.set(-15, 10, -15);
scene.add(fillLight);

/*
Blueprint ground grid — fine lines on XZ plane, evocative of technical drawings.
Major lines every 10 units, minor lines every 2 units.
*/
const gridSize = 120;
const gridDivisions = 60;
const grid = new THREE.GridHelper(gridSize, gridDivisions, 0x1a2840, 0x111828);
grid.material.transparent = true;
grid.material.opacity = 0.4;
grid.position.y = -0.5;
scene.add(grid);

const gridMajor = new THREE.GridHelper(gridSize, 12, 0x203858, 0x203858);
gridMajor.material.transparent = true;
gridMajor.material.opacity = 0.25;
gridMajor.position.y = -0.49;
scene.add(gridMajor);

/*
Faint horizon reference ring at NODE_RADIUS — shows where DHT hosts orbit.
*/
const orbitRingGeo = new THREE.RingGeometry(NODE_RADIUS - 0.04, NODE_RADIUS + 0.04, 96);
const orbitRingMat = new THREE.MeshBasicMaterial({
  color: 0x203858, transparent: true, opacity: 0.2, side: THREE.DoubleSide,
});
const orbitRing = new THREE.Mesh(orbitRingGeo, orbitRingMat);
orbitRing.rotation.x = -Math.PI / 2;
orbitRing.position.y = -0.48;
scene.add(orbitRing);

/*
Subtle fog to fade distant geometry and reinforce depth.
*/
scene.fog = new THREE.FogExp2(0x0a0e14, 0.006);
