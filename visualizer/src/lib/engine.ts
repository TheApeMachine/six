import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js';
import { decodeVizMessage, EK, type VizEvent } from './wire';
import type {
  NodeState, TrieState, EdgeState, BeamState, BeamHypothesis,
  ALUState, ALUOp, SubstrateState, PipelineStageState, ComputeState,
  FieldState, EigenmodeEntry, TrieGraphPayload, InspectorTarget,
} from './types';

const C = {
  bg: 0x0a0e1a,
  dataset: 0x448aff, machine: 0xff6e40, dht: 0xffab00, trie: 0xe6c930,
  algo: 0x76ff03, field: 0xba68c8, compiler: 0x4cc9f0, queue: 0xb388ff,
  cpu: 0x26c6da, cuda: 0x66bb6a, metal: 0xbdbdbd, errnie: 0xf44336,
  prim: 0xff80ab, network: 0x448aff, transport: 0x80deea, beam: 0xff6e40,
  gf257: 0x40c8a0, gf8191: 0xe89850, gf65537: 0xa868e8,
  beamActive: 0x76ff03, beamRejected: 0xf44336, beamConverge: 0xffab00,
};

const NODE_RADIUS = 14;
const TRIE_RING_R = 3.5;
const MAX_TRIES = 64;

export interface EngineCallbacks {
  onEvent: (event: VizEvent) => void;
  onInspect: (target: InspectorTarget | null) => void;
  onStats: (stats: EngineStats) => void;
  onLog: (log: string) => void;
  onBeamUpdate: (nodeId: string, beam: BeamState) => void;
  onALUUpdate: (alu: ALUState) => void;
  onConnectionChange: (connected: boolean) => void;
  onTimelineUpdate: (cursor: number, total: number) => void;
}

export interface EngineStats {
  nodeCount: number;
  trieCount: number;
  edgeCount: number;
  eventCount: number;
  droppedCount: number;
  fps: number;
  eventsPerSec: number;
}

interface SceneNode {
  id: string;
  group: THREE.Group;
  core: THREE.Mesh;
  face: THREE.Mesh;
  wire: THREE.LineSegments;
  pulse: THREE.Mesh;
  gfRing: GFRing;
  label: THREE.Sprite;
  data: NodeState;
  tries: SceneTrie[];
  edges: Set<string>;
  beam: InternalBeamState;
  beamRays: BeamRay[];
  beamHypMeshes: THREE.Mesh[];
}

interface SceneTrie {
  group: THREE.Group;
  rootMesh: THREE.Mesh;
  gfRing: GFRing;
  pickMeshes: THREE.Mesh[];
  graphGroup: THREE.Group | null;
  graphNodeByVid: Map<number, THREE.Mesh>;
  state: TrieState;
}

interface GFRing {
  group: THREE.Group;
  marker: THREE.Mesh;
  needle: THREE.Line;
  radius: number;
  phase: number;
  speed: number;
}

interface SceneEdge {
  from: string;
  to: string;
  line: THREE.Line;
  state: EdgeState;
  activity: number;
}

interface BeamRay {
  line: THREE.Line;
  mat: THREE.LineBasicMaterial;
  tipMesh: THREE.Mesh;
  tipMat: THREE.MeshBasicMaterial;
  targetPos: THREE.Vector3;
  active: boolean;
  score: number;
}

interface InternalBeamState {
  activeCount: number;
  rejectedCount: number;
  bestScore: number;
  lastSequence: string;
  lastCompose: number;
  hypotheses: BeamHypothesis[];
  converged: boolean;
}

interface PipelineStage3D {
  id: string;
  mesh: THREE.Mesh;
  edges: THREE.LineSegments;
  label: THREE.Sprite;
  subBoxes: THREE.Mesh[];
  pulseRing: THREE.Mesh;
  position: THREE.Vector3;
  color: number;
  metrics: PipelineStageState;
}

export function initEngine(container: HTMLDivElement, callbacks: EngineCallbacks) {
  const scene = new THREE.Scene();
  scene.fog = new THREE.FogExp2(C.bg, 0.004);

  const camera = new THREE.PerspectiveCamera(50, container.clientWidth / container.clientHeight, 0.1, 400);
  camera.position.set(0, 32, 44);

  const renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
  renderer.setSize(container.clientWidth, container.clientHeight);
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.setClearColor(C.bg);
  container.appendChild(renderer.domElement);

  const composer = new EffectComposer(renderer);
  composer.addPass(new RenderPass(scene, camera));
  const bloom = new UnrealBloomPass(
    new THREE.Vector2(container.clientWidth, container.clientHeight), 1.5, 0.4, 0.85
  );
  bloom.threshold = 0.15;
  bloom.strength = 0.4;
  bloom.radius = 0.5;
  composer.addPass(bloom);

  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.05;
  controls.maxDistance = 180;
  controls.minDistance = 8;
  controls.target.set(0, 4, 0);

  scene.add(new THREE.AmbientLight(0xffffff, 0.12));
  const grid = new THREE.GridHelper(80, 80, 0x151a2a, 0x0c1018);
  grid.position.y = -7;
  scene.add(grid);

  /* ── shared geometry / material factories ─────────────────────── */

  function makeMat(color: number, opacity = 0.06) {
    return new THREE.MeshBasicMaterial({ color, transparent: true, opacity, side: THREE.DoubleSide, depthWrite: false });
  }

  function edgeMat(color: number, opacity = 0.4) {
    return new THREE.LineBasicMaterial({ color, transparent: true, opacity });
  }

  function labelTexture(text: string, color: number, fontSize = 28): THREE.CanvasTexture {
    const cv = document.createElement('canvas');
    const ctx = cv.getContext('2d')!;
    cv.width = 512; cv.height = 64;
    ctx.font = `bold ${fontSize}px "Courier New", monospace`;
    ctx.fillStyle = '#' + color.toString(16).padStart(6, '0');
    ctx.fillText(text, 4, 42);
    const tex = new THREE.CanvasTexture(cv);
    tex.minFilter = THREE.LinearFilter;
    return tex;
  }

  function makeLabel(text: string, pos: THREE.Vector3, color = 0xffffff, size = 0.18): THREE.Sprite {
    const sp = new THREE.Sprite(new THREE.SpriteMaterial({ map: labelTexture(text, color), transparent: true, opacity: 0.8 }));
    sp.scale.set(size * text.length * 0.42, size * 0.75, 1);
    sp.position.copy(pos);
    return sp;
  }

  function makeBox(w: number, h: number, d: number, color: number, pos: THREE.Vector3, opacity = 0.06): { mesh: THREE.Mesh; edges: THREE.LineSegments } {
    const geo = new THREE.BoxGeometry(w, h, d);
    const mesh = new THREE.Mesh(geo, makeMat(color, opacity));
    mesh.position.copy(pos);
    const edges = new THREE.LineSegments(new THREE.EdgesGeometry(geo), edgeMat(color));
    edges.position.copy(pos);
    return { mesh, edges };
  }

  function makeGFRing(radius: number, ticks: number, pos: THREE.Vector3, color: number, initPhase = 0): GFRing {
    const group = new THREE.Group();
    group.position.copy(pos);

    const torusGeo = new THREE.TorusGeometry(radius, radius * 0.02, 8, 64);
    group.add(new THREE.Mesh(torusGeo, makeMat(color, 0.06)));
    const re = new THREE.LineSegments(new THREE.EdgesGeometry(torusGeo), edgeMat(color, 0.3));
    group.children[0].rotation.x = Math.PI / 2;
    re.rotation.x = Math.PI / 2;
    group.add(re);

    const dt = Math.min(ticks, 24);
    for (let i = 0; i < dt; i++) {
      const a = (i / dt) * Math.PI * 2;
      const pts = [
        new THREE.Vector3(Math.cos(a) * radius * 0.88, 0, Math.sin(a) * radius * 0.88),
        new THREE.Vector3(Math.cos(a) * radius, 0, Math.sin(a) * radius),
      ];
      group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(pts), edgeMat(color, i === 0 ? 0.6 : 0.15)));
    }

    const marker = new THREE.Mesh(
      new THREE.SphereGeometry(radius * 0.06, 8, 8),
      new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.9 })
    );
    marker.position.set(Math.cos(initPhase) * radius, 0, Math.sin(initPhase) * radius);
    group.add(marker);

    const needleArr = new Float32Array(6);
    needleArr[0] = 0;
    needleArr[1] = 0;
    needleArr[2] = 0;
    needleArr[3] = marker.position.x;
    needleArr[4] = marker.position.y;
    needleArr[5] = marker.position.z;
    const needleGeo = new THREE.BufferGeometry();
    needleGeo.setAttribute('position', new THREE.BufferAttribute(needleArr, 3));
    needleGeo.setDrawRange(0, 2);
    const needle = new THREE.Line(
      needleGeo,
      new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.5 })
    );
    group.add(needle);

    return { group, marker, needle, radius, phase: initPhase, speed: 0.1 };
  }

  function updateGFMarker(gf: GFRing) {
    gf.marker.position.set(Math.cos(gf.phase) * gf.radius, 0, Math.sin(gf.phase) * gf.radius);
    const posAttr = gf.needle.geometry.attributes.position as THREE.BufferAttribute;
    posAttr.setXYZ(1, gf.marker.position.x, gf.marker.position.y, gf.marker.position.z);
    posAttr.needsUpdate = true;
    gf.needle.geometry.computeBoundingSphere();
  }

  function curvedPipe(a: THREE.Vector3, b: THREE.Vector3, color: number, midY = 2, opacity = 0.18): THREE.Line {
    const mid = a.clone().add(b).multiplyScalar(0.5);
    mid.y += midY;
    return new THREE.Line(
      new THREE.BufferGeometry().setFromPoints(new THREE.QuadraticBezierCurve3(a, mid, b).getPoints(24)),
      new THREE.LineBasicMaterial({ color, transparent: true, opacity })
    );
  }

  function dashedPipe(a: THREE.Vector3, b: THREE.Vector3, color: number): THREE.Line {
    const mat = new THREE.LineDashedMaterial({ color, transparent: true, opacity: 0.12, dashSize: 0.3, gapSize: 0.2 });
    const line = new THREE.Line(new THREE.BufferGeometry().setFromPoints([a, b]), mat);
    line.computeLineDistances();
    return line;
  }

  /* ── state ────────────────────────────────────────────────────── */

  const nodes = new Map<string, SceneNode>();
  const edges = new Map<string, SceneEdge>();
  const timeline: VizEvent[] = [];
  let timelineCursor = 0;
  let eventCount = 0;
  let droppedCount = 0;
  let paused = false;
  let selectedTarget: InspectorTarget | null = null;

  const alu: ALUState = {
    totalDispatches: 0,
    substrates: {
      cpu: { inflight: 0, totalDispatches: 0, emaDurationNs: 0, lastDurationNs: 0 },
      cuda: { inflight: 0, totalDispatches: 0, emaDurationNs: 0, lastDurationNs: 0 },
      metal: { inflight: 0, totalDispatches: 0, emaDurationNs: 0, lastDurationNs: 0 },
    },
    recentOps: [],
  };

  const fieldState: FieldState = {
    globalPhase: 0,
    phaseConcentration: 0,
    eigenmodes: [],
  };

  const pipelineStages = new Map<string, PipelineStage3D>();

  /* ── static scene elements ────────────────────────────────────── */

  const dhtCenter = new THREE.Vector3(5, 2, 0);
  const dhtGroup = new THREE.Group();
  scene.add(dhtGroup);

  /* DHT ring torus */
  const dhtRingGeo = new THREE.TorusGeometry(NODE_RADIUS, 0.06, 8, 64);
  const dhtRingMesh = new THREE.Mesh(dhtRingGeo, makeMat(C.dht, 0.04));
  dhtRingMesh.rotation.x = Math.PI / 2;
  dhtRingMesh.position.copy(dhtCenter);
  const dhtRingEdge = new THREE.LineSegments(new THREE.EdgesGeometry(dhtRingGeo), edgeMat(C.dht, 0.2));
  dhtRingEdge.rotation.x = Math.PI / 2;
  dhtRingEdge.position.copy(dhtCenter);
  dhtGroup.add(dhtRingMesh, dhtRingEdge);

  const dhtLabel = makeLabel('kadabra.DHT', new THREE.Vector3(dhtCenter.x, dhtCenter.y + NODE_RADIUS * 0.5 + 2.5, dhtCenter.z), C.dht, 0.3);
  dhtGroup.add(dhtLabel);

  /* Global phase ring GF(65537) */
  const globalGF = makeGFRing(NODE_RADIUS + 2, 32, new THREE.Vector3(dhtCenter.x, dhtCenter.y + 5, dhtCenter.z), C.gf65537);
  globalGF.speed = 0.05;
  dhtGroup.add(globalGF.group);
  dhtGroup.add(makeLabel('GF(65537)', new THREE.Vector3(dhtCenter.x + NODE_RADIUS + 3, dhtCenter.y + 5, dhtCenter.z), C.gf65537, 0.14));

  /* ── data pipeline (left) ─────────────────────────────────────── */

  const staticGroup = new THREE.Group();
  scene.add(staticGroup);

  function addStatic(w: number, h: number, d: number, color: number, pos: THREE.Vector3, opacity = 0.06): THREE.Mesh {
    const { mesh, edges: edg } = makeBox(w, h, d, color, pos, opacity);
    staticGroup.add(mesh, edg);
    return mesh;
  }

  /* Dataset */
  const datasetMesh = addStatic(3, 2.5, 3, C.dataset, new THREE.Vector3(-18, 1.25, 0));
  datasetMesh.userData = { kind: 'pipeline', id: 'dataset' };
  staticGroup.add(makeLabel('Dataset', new THREE.Vector3(-18, 3, 0), C.dataset, 0.24));
  addStatic(1.2, 0.6, 1, C.dataset, new THREE.Vector3(-18.5, 0.5, -0.5), 0.1);
  staticGroup.add(makeLabel('HF', new THREE.Vector3(-18.5, 0.9, -0.5), 0x448aff, 0.1));
  addStatic(1.2, 0.6, 1, C.dataset, new THREE.Vector3(-17.3, 0.5, -0.5), 0.1);
  staticGroup.add(makeLabel('local', new THREE.Vector3(-17.3, 0.9, -0.5), 0x76ff03, 0.1));
  for (let i = 0; i < 6; i++) {
    staticGroup.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(-16.2 + i * 1.5, 1.25, 0),
        new THREE.Vector3(-16.2 + i * 1.5 + 1.2, 1.25, 0),
      ]),
      edgeMat(C.dataset, 0.1 + i * 0.03)
    ));
  }
  staticGroup.add(makeLabel('io.Copy stream', new THREE.Vector3(-13, 2.2, 0), C.dataset, 0.1));

  /* vm.Machine */
  const machineMesh = addStatic(5, 4, 4, C.machine, new THREE.Vector3(-8, 2, 0));
  machineMesh.userData = { kind: 'pipeline', id: 'machine' };
  staticGroup.add(makeLabel('vm.Machine', new THREE.Vector3(-8, 4.5, 0), C.machine, 0.28));

  const tokenizerRingGeo = new THREE.TorusGeometry(1.2, 0.08, 8, 48);
  const tokenizerRing = new THREE.Mesh(tokenizerRingGeo, makeMat(C.machine, 0.05));
  tokenizerRing.position.set(-8, 2, 0);
  tokenizerRing.rotation.x = Math.PI / 2;
  tokenizerRing.userData = { kind: 'pipeline', id: 'tokenizer' };
  staticGroup.add(tokenizerRing);
  staticGroup.add(new THREE.LineSegments(new THREE.EdgesGeometry(tokenizerRingGeo), edgeMat(C.machine, 0.25)));
  (staticGroup.children[staticGroup.children.length - 1] as THREE.Object3D).position.set(-8, 2, 0);
  (staticGroup.children[staticGroup.children.length - 1] as THREE.Object3D).rotation.x = Math.PI / 2;
  staticGroup.add(makeLabel('vm.Tokenizer', new THREE.Vector3(-8, 3.4, 0), C.machine, 0.11));

  /* Value pipeline boxes */
  for (let i = 0; i < 7; i++) {
    const x = -6 + i * 0.55;
    addStatic(0.4, 0.35, 0.35, C.prim, new THREE.Vector3(x, 2, 0), 0.25);
    if (i < 6) {
      staticGroup.add(new THREE.Line(
        new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(x + 0.2, 2, 0), new THREE.Vector3(x + 0.35, 2, 0)]),
        edgeMat(C.prim, 0.35)
      ));
    }
  }
  staticGroup.add(makeLabel('Value pipeline', new THREE.Vector3(-3.5, 2.6, 0), C.prim, 0.08));
  addStatic(2, 0.7, 1.5, C.prim, new THREE.Vector3(-8, 0.2, 2.2), 0.1);
  staticGroup.add(makeLabel('primitive.Value', new THREE.Vector3(-8, 0.7, 2.2), C.prim, 0.1));

  /* Machine → DHT pipe */
  staticGroup.add(curvedPipe(new THREE.Vector3(-5.5, 2, 0), new THREE.Vector3(dhtCenter.x - NODE_RADIUS, 2, 0), C.machine, 1.5));
  staticGroup.add(makeLabel('DrainPublishedValues', new THREE.Vector3(-1, 3.8, 0), C.machine, 0.09));

  /* ── compute pipeline (bottom) ────────────────────────────────── */

  const compY = -5;

  function addPipelineStage(id: string, label: string, color: number, pos: THREE.Vector3, w: number, h: number, d: number, subs: { name: string; offset: number }[]): PipelineStage3D {
    const { mesh, edges: edg } = makeBox(w, h, d, color, pos, 0.06);
    mesh.userData = { kind: 'pipeline', id };
    staticGroup.add(mesh, edg);
    const lbl = makeLabel(label, new THREE.Vector3(pos.x, pos.y + h / 2 + 0.4, pos.z), color, 0.14);
    staticGroup.add(lbl);

    const subBoxes: THREE.Mesh[] = [];
    subs.forEach((sub) => {
      const sb = addStatic(1, 0.45, 1, color, new THREE.Vector3(pos.x + sub.offset, pos.y - 0.5, pos.z), 0.15);
      subBoxes.push(sb);
      staticGroup.add(makeLabel(sub.name, new THREE.Vector3(pos.x + sub.offset, pos.y - 0.05, pos.z), color, 0.07));
    });

    const pulseGeo = new THREE.TorusGeometry(w / 2.5, 0.03, 8, 32);
    const pulseRing = new THREE.Mesh(pulseGeo, makeMat(color, 0));
    pulseRing.position.copy(pos);
    pulseRing.rotation.x = Math.PI / 2;
    staticGroup.add(pulseRing);

    const stage: PipelineStage3D = {
      id, mesh, edges: edg, label: lbl, subBoxes, pulseRing,
      position: pos.clone(), color,
      metrics: { id, totalEvents: 0, bytesProcessed: 0, inflight: 0, emaDurationMs: 0, recentOps: [] },
    };
    pipelineStages.set(id, stage);
    return stage;
  }

  addPipelineStage('compiler', 'programmer.Compiler', C.compiler, new THREE.Vector3(-4, compY, 0), 4, 2, 3, []);
  addPipelineStage('queue', 'pool.Queue', C.queue, new THREE.Vector3(1, compY, 0), 3.5, 2, 3, []);

  const spillGeo = new THREE.TorusGeometry(0.5, 0.035, 8, 32);
  const spillRing = new THREE.Mesh(spillGeo, makeMat(C.queue, 0.05));
  spillRing.position.set(1, compY - 0.3, 0);
  spillRing.rotation.x = Math.PI / 2;
  staticGroup.add(spillRing);
  staticGroup.add(new THREE.LineSegments(new THREE.EdgesGeometry(spillGeo), edgeMat(C.queue, 0.2)));
  (staticGroup.children[staticGroup.children.length - 1] as THREE.Object3D).position.set(1, compY - 0.3, 0);
  (staticGroup.children[staticGroup.children.length - 1] as THREE.Object3D).rotation.x = Math.PI / 2;
  staticGroup.add(makeLabel('spill ring', new THREE.Vector3(1, compY - 1.1, 0), C.queue, 0.07));

  addPipelineStage('cpu', 'cpu.Backend', C.cpu, new THREE.Vector3(6.5, compY, 0), 3, 2, 3, [
    { name: 'CSA', offset: -0.5 }, { name: 'WBlk', offset: 0.6 },
  ]);
  addPipelineStage('cuda', 'cuda.Backend', C.cuda, new THREE.Vector3(10.5, compY, 0), 2.5, 2, 3, [
    { name: 'kern', offset: 0 },
  ]);
  addPipelineStage('metal', 'metal.Backend', C.metal, new THREE.Vector3(13.5, compY, 0), 2.5, 2, 3, [
    { name: 'shdr', offset: 0 },
  ]);

  /* Observer + frames */
  addStatic(2, 0.7, 1.5, C.compiler, new THREE.Vector3(6.5, compY + 1.5, 2), 0.08);
  staticGroup.add(makeLabel('kernel.Observer', new THREE.Vector3(6.5, compY + 2, 2), C.compiler, 0.08));
  addStatic(2, 0.7, 1.5, C.compiler, new THREE.Vector3(9.5, compY + 1.5, 2), 0.08);
  staticGroup.add(makeLabel('frames', new THREE.Vector3(9.5, compY + 2, 2), C.compiler, 0.08));

  /* Compute pipes */
  staticGroup.add(new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(-2, compY, 0), new THREE.Vector3(-0.5, compY, 0)]),
    edgeMat(C.compiler, 0.25)
  ));
  staticGroup.add(new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(2.75, compY, 0), new THREE.Vector3(5, compY, 0)]),
    edgeMat(C.queue, 0.25)
  ));
  staticGroup.add(new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(-8, 0, 0), new THREE.Vector3(-4, compY + 1, 0)]),
    edgeMat(C.compiler, 0.12)
  ));
  staticGroup.add(new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(5, 0.6, 0), new THREE.Vector3(1, compY + 1, 0)]),
    edgeMat(C.queue, 0.12)
  ));

  /* ── network (right) ──────────────────────────────────────────── */

  const netMesh = addStatic(3, 2, 3, C.network, new THREE.Vector3(-16, compY + 1, 6));
  netMesh.userData = { kind: 'pipeline', id: 'network' };
  staticGroup.add(makeLabel('network.UniConn', new THREE.Vector3(-16, compY + 2.5, 6), C.network, 0.12));
  addStatic(1, 0.5, 0.8, C.network, new THREE.Vector3(-16.5, compY + 0.5, 5.5), 0.1);
  staticGroup.add(makeLabel('QUIC', new THREE.Vector3(-16.5, compY + 0.9, 5.5), C.network, 0.08));
  addStatic(1, 0.5, 0.8, C.network, new THREE.Vector3(-15.3, compY + 0.5, 5.5), 0.1);
  staticGroup.add(makeLabel('UDP', new THREE.Vector3(-15.3, compY + 0.9, 5.5), C.network, 0.08));

  addStatic(2.5, 1.2, 2, C.transport, new THREE.Vector3(-16, compY + 3.5, 6));
  staticGroup.add(makeLabel('transport.Pipeline', new THREE.Vector3(-16, compY + 4.5, 6), C.transport, 0.1));
  staticGroup.add(curvedPipe(new THREE.Vector3(-14.5, compY + 1, 6), new THREE.Vector3(-1.5, 2, 2), C.network, 3));

  /* errnie.Logger */
  addStatic(2.5, 1.5, 2, C.errnie, new THREE.Vector3(-16, compY + 1, -5));
  staticGroup.add(makeLabel('errnie.Logger', new THREE.Vector3(-16, compY + 2.5, -5), C.errnie, 0.12));
  staticGroup.add(dashedPipe(new THREE.Vector3(-16, compY + 1, -5), new THREE.Vector3(-8, 2, 0), C.errnie));
  staticGroup.add(dashedPipe(new THREE.Vector3(-16, compY + 1, -5), new THREE.Vector3(5, 2, 0), C.errnie));

  /* ── algo.Stack (top) ─────────────────────────────────────────── */

  const algoStackY = 14;
  addStatic(18, 1.8, 3.5, 0x444444, new THREE.Vector3(0, algoStackY, 0), 0.025);
  staticGroup.add(makeLabel('algo.Stack', new THREE.Vector3(0, algoStackY + 1.3, 0), 0xcccccc, 0.22));

  const algoComponents = [
    'classify.Classifier', 'beam.Search', 'train.Online', 'causal.Graph',
    'surprisal.Probability', 'cooccurrence.Matrix', 'episodic.Buffer', 'policy.Scores',
  ];
  const algoMeshes: THREE.Mesh[] = [];
  algoComponents.forEach((name, i) => {
    const x = -7 + i * 2;
    const m = addStatic(2, 0.6, 1, 0xcccccc, new THREE.Vector3(x, algoStackY - 0.2, 0), 0.08);
    m.userData = { kind: 'algorithm', id: name };
    algoMeshes.push(m);
    staticGroup.add(makeLabel(name, new THREE.Vector3(x, algoStackY + 0.15, 0), 0xcccccc, 0.07));
  });

  /* cmd box */
  addStatic(2.5, 0.7, 1.5, 0xffffff, new THREE.Vector3(0, algoStackY + 1.8, 0), 0.03);
  staticGroup.add(makeLabel('cmd', new THREE.Vector3(0, algoStackY + 2.4, 0), 0xffffff, 0.18));

  /* Stack → field pipe */
  staticGroup.add(curvedPipe(new THREE.Vector3(0, algoStackY - 0.9, 0), new THREE.Vector3(3, 12, 0), C.field, 1));
  staticGroup.add(curvedPipe(new THREE.Vector3(-6, algoStackY - 0.9, 0), new THREE.Vector3(-18, 2.5, 0), C.dataset, 2));

  /* ── phase dials ──────────────────────────────────────────────── */

  const dialY = 11;
  const dialDefs = [
    { name: 'Clifford', x: -3, color: 0xba68c8 },
    { name: 'PGA', x: 1, color: 0x80cbc4 },
    { name: 'Phase', x: 5, color: 0xffab40 },
    { name: 'Eigenmode', x: 9, color: 0x4fc3f7 },
  ];
  const dials: { group: THREE.Group; needle: THREE.Line; tip: THREE.Mesh; radius: number; phase: number; speed: number; needlePos: THREE.BufferAttribute }[] = [];

  dialDefs.forEach((d) => {
    const group = new THREE.Group();
    group.position.set(d.x, dialY, 0);
    const dR = 1.2;
    const disc = new THREE.Mesh(new THREE.CylinderGeometry(dR, dR, 0.02, 32), makeMat(d.color, 0.08));
    group.add(disc);
    group.add(new THREE.LineSegments(new THREE.EdgesGeometry(disc.geometry), edgeMat(d.color, 0.3)));

    for (let i = 0; i < 12; i++) {
      const a = (i / 12) * Math.PI * 2;
      group.add(new THREE.Line(
        new THREE.BufferGeometry().setFromPoints([
          new THREE.Vector3(Math.cos(a) * dR * 0.75, 0.02, Math.sin(a) * dR * 0.75),
          new THREE.Vector3(Math.cos(a) * dR * 0.95, 0.02, Math.sin(a) * dR * 0.95),
        ]),
        edgeMat(d.color, i % 3 === 0 ? 0.5 : 0.2)
      ));
    }

    const ph = Math.random() * Math.PI * 2;
    const tipX = Math.cos(ph) * dR * 0.85;
    const tipZ = Math.sin(ph) * dR * 0.85;
    const tipPos = new THREE.Vector3(tipX, 0.04, tipZ);
    const dialNeedleArr = new Float32Array(6);
    dialNeedleArr[0] = 0;
    dialNeedleArr[1] = 0.04;
    dialNeedleArr[2] = 0;
    dialNeedleArr[3] = tipX;
    dialNeedleArr[4] = 0.04;
    dialNeedleArr[5] = tipZ;
    const needleGeo = new THREE.BufferGeometry();
    const needlePos = new THREE.BufferAttribute(dialNeedleArr, 3);
    needleGeo.setAttribute('position', needlePos);
    needleGeo.setDrawRange(0, 2);
    const needle = new THREE.Line(
      needleGeo,
      new THREE.LineBasicMaterial({ color: d.color, transparent: true, opacity: 0.8 })
    );
    group.add(needle);
    const tip = new THREE.Mesh(
      new THREE.SphereGeometry(dR * 0.06, 6, 6),
      new THREE.MeshBasicMaterial({ color: d.color, transparent: true, opacity: 0.9 })
    );
    tip.position.copy(tipPos);
    group.add(tip);
    group.add(new THREE.Mesh(
      new THREE.SphereGeometry(dR * 0.04, 6, 6),
      new THREE.MeshBasicMaterial({ color: d.color, transparent: true, opacity: 0.6 })
    ));
    scene.add(group);
    staticGroup.add(makeLabel(d.name, new THREE.Vector3(d.x, dialY + 1.5, 0), d.color, 0.12));
    dials.push({ group, needle, tip, radius: dR, phase: ph, speed: 0.05 + Math.random() * 0.15, needlePos });
  });

  ['EMA', 'delta', 'shannon', '\u03C3-clamp'].forEach((n, i) => {
    addStatic(1.2, 0.4, 0.7, C.field, new THREE.Vector3(-3 + i * 1.8, dialY - 1.8, 1.5), 0.1);
    staticGroup.add(makeLabel(n, new THREE.Vector3(-3 + i * 1.8, dialY - 1.4, 1.5), C.field, 0.06));
  });

  /* Dial → DHT pipes */
  dialDefs.forEach((d) => {
    staticGroup.add(curvedPipe(new THREE.Vector3(d.x, dialY - 1.2, 0), new THREE.Vector3(dhtCenter.x, dhtCenter.y + 1.5, 0), d.color, -1, 0.08));
  });

  /* ── algorithm ring around DHT ────────────────────────────────── */

  const algoRingR = NODE_RADIUS + 3.5;
  const algoRingNames = [
    'attention', 'beam', 'causal', 'episodic', 'replay', 'policy',
    'infer', 'surprisal', 'train', 'classify', 'pattern', 'semantic',
  ];
  const algoRingMeshes: THREE.Mesh[] = [];
  algoRingNames.forEach((name, i) => {
    const a = (i / 12) * Math.PI * 2;
    const ax = dhtCenter.x + Math.cos(a) * algoRingR;
    const az = dhtCenter.z + Math.sin(a) * algoRingR;
    const m = addStatic(1.4, 0.55, 0.8, C.algo, new THREE.Vector3(ax, 5.5, az), 0.1);
    m.userData = { kind: 'algorithm', id: name };
    algoRingMeshes.push(m);
    staticGroup.add(makeLabel(name, new THREE.Vector3(ax, 5.95, az), C.algo, 0.08));
  });

  /* ── particles ────────────────────────────────────────────────── */

  const datasetParticles: THREE.Mesh[] = [];
  const pGeo = new THREE.SphereGeometry(0.05, 6, 6);
  for (let i = 0; i < 14; i++) {
    const pm = new THREE.Mesh(pGeo, new THREE.MeshBasicMaterial({ color: C.dataset, transparent: true, opacity: 0.5 }));
    pm.userData = { t: i / 14, sp: 0.1 + Math.random() * 0.05 };
    scene.add(pm);
    datasetParticles.push(pm);
  }

  const valueParticles: THREE.Mesh[] = [];
  const vGeo = new THREE.BoxGeometry(0.1, 0.1, 0.1);
  for (let i = 0; i < 10; i++) {
    const vm = new THREE.Mesh(vGeo, new THREE.MeshBasicMaterial({ color: C.machine, transparent: true, opacity: 0.4 }));
    vm.userData = { t: i / 10, sp: 0.07 + Math.random() * 0.03, ni: 0 };
    scene.add(vm);
    valueParticles.push(vm);
  }

  /* Flow particles for compute pipeline */
  const flowParticles: {
    mesh: THREE.Mesh;
    from: THREE.Vector3;
    to: THREE.Vector3;
    mid: THREE.Vector3;
    tmpPos: THREE.Vector3;
    t: number;
    speed: number;
    active: boolean;
  }[] = [];
  const flowGeo = new THREE.SphereGeometry(0.08, 6, 6);
  for (let i = 0; i < 20; i++) {
    const fm = new THREE.Mesh(flowGeo, new THREE.MeshBasicMaterial({ color: C.compiler, transparent: true, opacity: 0 }));
    scene.add(fm);
    flowParticles.push({
      mesh: fm,
      from: new THREE.Vector3(),
      to: new THREE.Vector3(),
      mid: new THREE.Vector3(),
      tmpPos: new THREE.Vector3(),
      t: 0,
      speed: 0,
      active: false,
    });
  }

  /* ── node management ──────────────────────────────────────────── */

  function repositionNodes() {
    const N = nodes.size;
    let idx = 0;
    for (const node of nodes.values()) {
      const angle = (idx / N) * Math.PI * 2 - Math.PI / 2;
      const nx = dhtCenter.x + Math.cos(angle) * NODE_RADIUS;
      const nz = dhtCenter.z + Math.sin(angle) * NODE_RADIUS;
      node.group.position.set(nx, dhtCenter.y, nz);
      idx++;
    }

    /* Reconnect algo ring dashed lines to nearest nodes */
    /* (These are rebuilt dynamically as nodes come/go) */
  }

  function ensureNode(id: string): SceneNode {
    let node = nodes.get(id);
    if (node) return node;

    const group = new THREE.Group();
    group.position.copy(dhtCenter);

    const coreGeo = new THREE.BoxGeometry(2.4, 2.8, 2.4);
    const core = new THREE.Mesh(coreGeo, makeMat(C.dht, 0.06));
    core.userData = { kind: 'node', id };
    group.add(core);

    const face = new THREE.Mesh(coreGeo, makeMat(C.dht, 0.03));
    group.add(face);

    const wire = new THREE.LineSegments(new THREE.EdgesGeometry(coreGeo), edgeMat(C.dht, 0.35));
    group.add(wire);

    /* Pulse ring */
    const pulseGeo = new THREE.TorusGeometry(1.8, 0.04, 8, 32);
    const pulse = new THREE.Mesh(pulseGeo, makeMat(C.dht, 0));
    pulse.rotation.x = Math.PI / 2;
    group.add(pulse);

    const gfRing = makeGFRing(1.8, 24, new THREE.Vector3(0, 0, 0), C.gf8191);
    gfRing.speed = 0.1 + Math.random() * 0.2;
    group.add(gfRing.group);

    const label = makeLabel(id.length > 16 ? id.substring(0, 16) : id, new THREE.Vector3(0, 1.9, 0), C.dht, 0.11);
    group.add(label);

    scene.add(group);

    node = {
      id, group, core, face, wire, pulse, gfRing, label,
      data: {
        id, label: id, insertCount: 0, predictCount: 0, gossipCount: 0, trieCount: 0,
        digest: { surprisal: 0, entropy: 0, growth: 0 },
        pressure: {}, latencies: {}, labelCounts: {}, recentSequences: [], eigenmode: -1,
      },
      tries: [],
      edges: new Set(),
      beam: { activeCount: 0, rejectedCount: 0, bestScore: 0, lastSequence: '', lastCompose: 0, hypotheses: [], converged: false },
      beamRays: [],
      beamHypMeshes: [],
    };
    nodes.set(id, node);
    repositionNodes();

    callbacks.onStats(getStats());
    return node;
  }

  function ensureTrie(nodeId: string, trieIdx: number): SceneTrie {
    const node = ensureNode(nodeId);
    while (node.tries.length <= trieIdx && node.tries.length < MAX_TRIES) {
      const ti = node.tries.length;
      const trieGroup = new THREE.Group();

      const count = node.tries.length + 1;
      const angle = (ti / Math.max(count, 1)) * Math.PI * 2;
      trieGroup.position.set(Math.cos(angle) * TRIE_RING_R, 0.8, Math.sin(angle) * TRIE_RING_R);

      /* Root sphere */
      const rootMesh = new THREE.Mesh(
        new THREE.SphereGeometry(0.06, 8, 8),
        new THREE.MeshBasicMaterial({ color: C.trie, transparent: true, opacity: 0.7 })
      );
      rootMesh.userData = { kind: 'trie', id: nodeId, trieIndex: ti };
      trieGroup.add(rootMesh);

      /* Mini trie shape (root + 2 branches + 4 leaves) */
      const spread = 0.18;
      const branches = [[-spread, -0.35, -0.1], [spread, -0.35, 0.1]];
      branches.forEach((bp) => {
        trieGroup.add(new THREE.Line(
          new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(0, 0, 0), new THREE.Vector3(bp[0], bp[1], bp[2])]),
          edgeMat(C.trie, 0.3)
        ));
        const bm = new THREE.Mesh(
          new THREE.SphereGeometry(0.04, 6, 6),
          new THREE.MeshBasicMaterial({ color: C.trie, transparent: true, opacity: 0.5 })
        );
        bm.position.set(bp[0], bp[1], bp[2]);
        trieGroup.add(bm);

        /* Leaves */
        [[-0.08, -0.2, -0.05], [0.08, -0.2, 0.05]].forEach((lp) => {
          trieGroup.add(new THREE.Line(
            new THREE.BufferGeometry().setFromPoints([
              new THREE.Vector3(bp[0], bp[1], bp[2]),
              new THREE.Vector3(bp[0] + lp[0], bp[1] + lp[1], bp[2] + lp[2]),
            ]),
            edgeMat(C.trie, 0.2)
          ));
          const lm = new THREE.Mesh(
            new THREE.SphereGeometry(0.025, 4, 4),
            new THREE.MeshBasicMaterial({ color: C.trie, transparent: true, opacity: 0.4 })
          );
          lm.position.set(bp[0] + lp[0], bp[1] + lp[1], bp[2] + lp[2]);
          trieGroup.add(lm);
        });
      });

      /* Trie GF(257) ring */
      const gfRing = makeGFRing(0.4, 16, new THREE.Vector3(0, -0.55, 0), C.gf257);
      gfRing.speed = 0.2 + Math.random() * 0.4;
      trieGroup.add(gfRing.group);

      node.group.add(trieGroup);

      const trieSt: SceneTrie = {
        group: trieGroup, rootMesh, gfRing,
        pickMeshes: [rootMesh],
        graphGroup: null, graphNodeByVid: new Map(),
        state: {
          index: ti, nodeId, insertFlash: 0, surprisal: 0, entropy: 0, growth: 0,
          decayMul: 1, learnMul: 1, aligned: false, graphPayload: null,
        },
      };
      node.tries.push(trieSt);

      /* Add beam rays from this trie */
      for (let b = 0; b < 2; b++) {
        const ba = Math.random() * Math.PI * 2;
        const bl = 2 + Math.random() * 2.5;
        const endX = Math.cos(ba) * bl;
        const endZ = Math.sin(ba) * bl;
        const endY = 0.5 + Math.random() * 1.5;

        const A = new THREE.Vector3(0, 0, 0);
        const M = new THREE.Vector3(endX / 2, endY / 2 + 0.5, endZ / 2);
        const B = new THREE.Vector3(endX, endY, endZ);

        const bMat = new THREE.LineBasicMaterial({ color: C.beam, transparent: true, opacity: 0 });
        const bLine = new THREE.Line(new THREE.BufferGeometry().setFromPoints(new THREE.QuadraticBezierCurve3(A, M, B).getPoints(16)), bMat);
        trieGroup.add(bLine);

        const tipMat = new THREE.MeshBasicMaterial({ color: C.beam, transparent: true, opacity: 0 });
        const tipMesh = new THREE.Mesh(new THREE.SphereGeometry(0.06, 6, 6), tipMat);
        tipMesh.position.copy(B);
        trieGroup.add(tipMesh);

        node.beamRays.push({ line: bLine, mat: bMat, tipMesh, tipMat, targetPos: B, active: false, score: 0 });
      }
    }

    /* Reposition tries around the ring */
    const count = node.tries.length;
    node.tries.forEach((t, i) => {
      const angle = (i / count) * Math.PI * 2;
      t.group.position.set(Math.cos(angle) * TRIE_RING_R, 0.8, Math.sin(angle) * TRIE_RING_R);
    });

    node.data.trieCount = node.tries.length;
    return node.tries[trieIdx];
  }

  function ensureEdge(from: string, to: string): SceneEdge {
    const eid = from < to ? `${from}|${to}` : `${to}|${from}`;
    let edge = edges.get(eid);
    if (edge) return edge;

    const nFrom = ensureNode(from);
    const nTo = ensureNode(to);

    const line = curvedPipe(
      nFrom.group.position,
      nTo.group.position,
      C.dht, 2, 0.1
    );
    scene.add(line);

    nFrom.edges.add(eid);
    nTo.edges.add(eid);

    edge = {
      from, to, line,
      state: { from, to, latencyMs: 0, gossipCount: 0, replicationCount: 0, activity: 0 },
      activity: 0,
    };
    edges.set(eid, edge);
    return edge;
  }

  /* ── beam search visualization helpers ────────────────────────── */

  function updateBeamVisuals(node: SceneNode) {
    const bm = node.beam;

    /* Color beam rays: active = green pulse, rejected = red fade, converged = gold */
    node.beamRays.forEach((ray, i) => {
      if (bm.converged) {
        ray.mat.color.setHex(C.beamConverge);
        ray.tipMat.color.setHex(C.beamConverge);
        ray.active = true;
        ray.score = bm.bestScore;
      } else if (i < bm.activeCount) {
        ray.mat.color.setHex(C.beamActive);
        ray.tipMat.color.setHex(C.beamActive);
        ray.active = true;
        ray.score = bm.hypotheses[i]?.score ?? 0;
      } else if (i < bm.activeCount + bm.rejectedCount) {
        ray.mat.color.setHex(C.beamRejected);
        ray.tipMat.color.setHex(C.beamRejected);
        ray.active = true;
        ray.score = -1;
      } else {
        ray.active = false;
      }
    });

    /* Orbiting hypothesis spheres */
    while (node.beamHypMeshes.length < bm.hypotheses.length) {
      const hm = new THREE.Mesh(
        new THREE.SphereGeometry(0.1, 8, 8),
        new THREE.MeshBasicMaterial({ color: C.beam, transparent: true, opacity: 0.6 })
      );
      node.group.add(hm);
      node.beamHypMeshes.push(hm);
    }
    while (node.beamHypMeshes.length > bm.hypotheses.length) {
      const hm = node.beamHypMeshes.pop()!;
      node.group.remove(hm);
      hm.geometry.dispose();
      (hm.material as THREE.Material).dispose();
    }
  }

  /* ── trie graph snapshot rendering ────────────────────────────── */

  function renderTrieGraph(trie: SceneTrie, payload: TrieGraphPayload) {
    /* Remove old graph */
    if (trie.graphGroup) {
      trie.group.remove(trie.graphGroup);
      trie.graphGroup.traverse((obj: THREE.Object3D) => {
        if ((obj as any).geometry) (obj as any).geometry.dispose();
        if ((obj as any).material) (obj as any).material.dispose();
      });
    }

    trie.graphGroup = new THREE.Group();
    trie.graphNodeByVid.clear();

    const vertices = payload.vertices;
    const edgeList = payload.edges;

    /* Layout: spread by depth and index */
    const byDepth = new Map<number, typeof vertices>();
    vertices.forEach((v) => {
      const arr = byDepth.get(v.depth) || [];
      arr.push(v);
      byDepth.set(v.depth, arr);
    });

    const vertexPos = new Map<number, THREE.Vector3>();
    byDepth.forEach((verts, depth) => {
      verts.forEach((v, i) => {
        const spread = 0.15;
        const x = (i - (verts.length - 1) / 2) * spread;
        const y = -depth * 0.2;
        const pos = new THREE.Vector3(x, y, 0);
        vertexPos.set(v.vid, pos);

        const size = Math.max(0.015, Math.min(0.04, v.visits * 0.002));
        const vMesh = new THREE.Mesh(
          new THREE.SphereGeometry(size, 6, 6),
          new THREE.MeshBasicMaterial({ color: C.trie, transparent: true, opacity: 0.6 + Math.min(v.visits * 0.05, 0.35) })
        );
        vMesh.position.copy(pos);
        vMesh.userData = {
          kind: 'trieVertex',
          kadabraNodeId: trie.state.nodeId,
          trieIdx: trie.state.index,
          graphVertexVid: v.vid,
        };
        trie.graphGroup!.add(vMesh);
        trie.graphNodeByVid.set(v.vid, vMesh);
        trie.pickMeshes.push(vMesh);
      });
    });

    /* Edges */
    edgeList.forEach((e) => {
      const fPos = vertexPos.get(e.from);
      const tPos = vertexPos.get(e.to);
      if (fPos && tPos) {
        trie.graphGroup!.add(new THREE.Line(
          new THREE.BufferGeometry().setFromPoints([fPos, tPos]),
          edgeMat(C.trie, 0.2)
        ));
      }
    });

    trie.graphGroup.position.set(0, -0.1, 0);
    trie.group.add(trie.graphGroup);
    trie.state.graphPayload = payload;
  }

  /* ── flow particle spawn ──────────────────────────────────────── */

  function spawnFlowParticle(from: THREE.Vector3, to: THREE.Vector3, color: number) {
    for (const fp of flowParticles) {
      if (!fp.active) {
        fp.from.copy(from);
        fp.to.copy(to);
        fp.mid.copy(from).add(to).multiplyScalar(0.5);
        fp.mid.y += 1.5;
        fp.t = 0;
        fp.speed = 0.8 + Math.random() * 0.4;
        fp.active = true;
        (fp.mesh.material as THREE.MeshBasicMaterial).color.setHex(color);
        return;
      }
    }
  }

  /* ── pulse a node ─────────────────────────────────────────────── */

  function pulseNode(node: SceneNode) {
    (node.pulse.material as THREE.MeshBasicMaterial).opacity = 0.4;
  }

  function pulsePipelineStage(stageId: string) {
    const stage = pipelineStages.get(stageId);
    if (stage) {
      (stage.pulseRing.material as THREE.MeshBasicMaterial).opacity = 0.5;
    }
  }

  /*
  disposeObjectTree releases GPU buffers for a subtree. Geometry and materials
  shared across meshes are disposed at most once per call via local sets.
  */
  function disposeObjectTree(root: THREE.Object3D) {
    const seenGeo = new Set<THREE.BufferGeometry>();
    const seenMat = new Set<THREE.Material>();
    const seenTex = new Set<THREE.Texture>();

    root.traverse((child) => {
      if (child instanceof THREE.Mesh || child instanceof THREE.Line || child instanceof THREE.LineSegments || child instanceof THREE.LineLoop) {
        const geo = child.geometry as THREE.BufferGeometry | undefined;

        if (geo && !seenGeo.has(geo)) {
          seenGeo.add(geo);
          geo.dispose();
        }

        const mat = child.material as THREE.Material | THREE.Material[];

        if (Array.isArray(mat)) {
          for (const m of mat) {
            if (m && !seenMat.has(m)) {
              seenMat.add(m);
              m.dispose();
            }
          }

          return;
        }

        if (mat && !seenMat.has(mat)) {
          seenMat.add(mat);
          mat.dispose();
        }

        return;
      }

      if (child instanceof THREE.Sprite) {
        const sm = child.material as THREE.SpriteMaterial;

        if (sm.map && !seenTex.has(sm.map)) {
          seenTex.add(sm.map);
          sm.map.dispose();
        }

        if (!seenMat.has(sm)) {
          seenMat.add(sm);
          sm.dispose();
        }
      }
    });
  }

  function disposeSceneNode(node: SceneNode) {
    scene.remove(node.group);
    disposeObjectTree(node.group);
  }

  /*
  Clears dynamic nodes/edges and simulation counters so timeline replay matches
  a cold apply of events[0..index).
  */
  function resetDynamicVisualization() {
    for (const node of nodes.values()) {
      disposeSceneNode(node);
    }

    nodes.clear();

    for (const edge of edges.values()) {
      scene.remove(edge.line);
      disposeObjectTree(edge.line);
    }

    edges.clear();

    alu.totalDispatches = 0;
    alu.recentOps.length = 0;

    for (const key of Object.keys(alu.substrates) as (keyof typeof alu.substrates)[]) {
      const substrate = alu.substrates[key];
      substrate.inflight = 0;
      substrate.totalDispatches = 0;
      substrate.emaDurationNs = 0;
      substrate.lastDurationNs = 0;
    }

    fieldState.globalPhase = 0;
    fieldState.phaseConcentration = 0;
    fieldState.eigenmodes = [];

    for (const stage of pipelineStages.values()) {
      stage.metrics.totalEvents = 0;
      stage.metrics.bytesProcessed = 0;
      stage.metrics.inflight = 0;
      stage.metrics.emaDurationMs = 0;
      stage.metrics.recentOps = [];
    }

    eventCount = 0;
    selectedTarget = null;
    deselectAll();

    flowParticles.forEach((fp) => {
      fp.active = false;
      (fp.mesh.material as THREE.MeshBasicMaterial).opacity = 0;
    });
  }

  /* ── event application ────────────────────────────────────────── */

  function applyEvent(ev: VizEvent) {
    eventCount++;
    const k = ev.kind;

    if (k === EK.NodeCreated) {
      ensureNode(ev.src);
    } else if (k === EK.NodeRemoved) {
      const node = nodes.get(ev.src);
      if (node) {
        disposeSceneNode(node);
        nodes.delete(ev.src);
        repositionNodes();
      }
    } else if (k === EK.PeerAdded) {
      ensureEdge(ev.src, ev.tgt);
    } else if (k === EK.PeerLatency) {
      const edge = ensureEdge(ev.src, ev.tgt);
      edge.state.latencyMs = ev.vals['latency_ms'] ?? 0;
      const nFrom = nodes.get(ev.src);
      const nTo = nodes.get(ev.tgt);
      if (nFrom) nFrom.data.latencies[ev.tgt] = edge.state.latencyMs;
      if (nTo) nTo.data.latencies[ev.src] = edge.state.latencyMs;
    } else if (k === EK.ValuePublished) {
      const node = ensureNode(ev.src);
      node.data.insertCount++;
      pulseNode(node);
    } else if (k === EK.ValueReplicated) {
      const edge = ensureEdge(ev.src, ev.tgt);
      edge.state.replicationCount++;
      edge.activity = 1;
    } else if (k === EK.GossipSent || k === EK.GossipReceived) {
      const node = ensureNode(ev.src);
      node.data.gossipCount++;
      if (ev.tgt) {
        const edge = ensureEdge(ev.src, ev.tgt);
        edge.state.gossipCount++;
        edge.activity = 1;
      }
    } else if (k === EK.FieldDigest) {
      const node = ensureNode(ev.src);
      node.data.digest.surprisal = ev.vals['surprisal'] ?? 0;
      node.data.digest.entropy = ev.vals['entropy'] ?? 0;
      node.data.digest.growth = ev.vals['growth'] ?? 0;
    } else if (k === EK.EigenmodeDetected) {
      fieldState.globalPhase = ev.vals['phase'] ?? 0;
      fieldState.phaseConcentration = ev.vals['concentration'] ?? 0;
    } else if (k === EK.FieldPressure) {
      const node = ensureNode(ev.src);
      node.data.pressure['decay_mul'] = ev.vals['decay_mul'] ?? 1;
      node.data.pressure['learn_mul'] = ev.vals['learn_mul'] ?? 1;
    } else if (k === EK.TrieInsert) {
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      const trie = ensureTrie(ev.src, trieIdx);
      trie.state.insertFlash = 1;
      const node = nodes.get(ev.src);
      if (node) {
        node.data.insertCount++;
        pulseNode(node);
      }
    } else if (k === EK.TrieDecay) {
      /* Visual: brief dim on trie */
    } else if (k === EK.TriePrune) {
      /* Visual: brief shrink */
    } else if (k === EK.TriePredict || k === EK.TrieClassify) {
      const node = ensureNode(ev.src);
      node.data.predictCount++;
      if (k === EK.TrieClassify && ev.meta['labels']) {
        try {
          const labels: Record<string, number> = JSON.parse(ev.meta['labels']);
          Object.entries(labels).forEach(([l, c]) => {
            node.data.labelCounts[l] = (node.data.labelCounts[l] ?? 0) + c;
          });
        } catch { /* ignore malformed */ }
      }
    } else if (k === EK.TrieExperience) {
      const node = ensureNode(ev.src);
      if (ev.lbl) {
        node.data.recentSequences.push(ev.lbl);
        if (node.data.recentSequences.length > 16) node.data.recentSequences.shift();
      }
    } else if (k === EK.TrieCoupling) {
      /* Visual: arc between two tries (could add later) */
    } else if (k === EK.TrieMode) {
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      const trie = ensureTrie(ev.src, trieIdx);
      trie.state.aligned = !!ev.vals['aligned'];
    } else if (k === EK.TriePressure) {
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      const trie = ensureTrie(ev.src, trieIdx);
      trie.state.decayMul = ev.vals['decay_mul'] ?? 1;
      trie.state.learnMul = ev.vals['learn_mul'] ?? 1;
    } else if (k === EK.TrieSignal) {
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      const trie = ensureTrie(ev.src, trieIdx);
      trie.state.surprisal = ev.vals['surprisal'] ?? 0;
      trie.state.entropy = ev.vals['entropy'] ?? 0;
      trie.state.growth = ev.vals['growth'] ?? 0;
    } else if (k === EK.BeamCollect) {
      const node = ensureNode(ev.src);
      pulseNode(node);
      node.beam.activeCount = ev.vals['count'] ?? 0;
    } else if (k === EK.BeamCompose) {
      const node = ensureNode(ev.src);
      node.beam.lastCompose = Date.now();
      node.beam.activeCount = ev.vals['selected'] ?? 0;
      node.beam.rejectedCount = ev.vals['rejected'] ?? 0;
      node.beam.bestScore = ev.vals['best_score'] ?? 0;
      node.beam.converged = false;

      /* Parse hypotheses from meta */
      if (ev.meta['hypotheses']) {
        try {
          node.beam.hypotheses = JSON.parse(ev.meta['hypotheses']);
        } catch { /* ignore */ }
      }
      updateBeamVisuals(node);
      callbacks.onBeamUpdate(ev.src, {
        activeCount: node.beam.activeCount,
        rejectedCount: node.beam.rejectedCount,
        bestScore: node.beam.bestScore,
        lastSequence: node.beam.lastSequence,
        hypotheses: node.beam.hypotheses,
        converged: false,
      });
    } else if (k === EK.BeamBreak) {
      const node = ensureNode(ev.src);
      /* Flash red on broken trie */
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      if (node.tries[trieIdx]) {
        (node.tries[trieIdx].rootMesh.material as THREE.MeshBasicMaterial).color.setHex(C.beamRejected);
        setTimeout(() => {
          if (node.tries[trieIdx]) {
            (node.tries[trieIdx].rootMesh.material as THREE.MeshBasicMaterial).color.setHex(C.trie);
          }
        }, 600);
      }
    } else if (k === EK.BeamConverge) {
      const node = ensureNode(ev.src);
      node.beam.converged = true;
      node.beam.lastSequence = ev.lbl || ev.meta['sequence'] || '';
      node.beam.bestScore = ev.vals['score'] ?? node.beam.bestScore;
      updateBeamVisuals(node);
      callbacks.onBeamUpdate(ev.src, {
        activeCount: node.beam.activeCount,
        rejectedCount: 0,
        bestScore: node.beam.bestScore,
        lastSequence: node.beam.lastSequence,
        hypotheses: node.beam.hypotheses,
        converged: true,
      });
    } else if (k === EK.TrieGraphSnapshot) {
      const trieIdx = ev.vals['trie_idx'] ?? 0;
      const trie = ensureTrie(ev.src, trieIdx);
      if (ev.meta['graph']) {
        try {
          const payload: TrieGraphPayload = JSON.parse(ev.meta['graph']);
          renderTrieGraph(trie, payload);
        } catch { /* ignore */ }
      }
    } else if (k === EK.CompilerCompile) {
      pulsePipelineStage('compiler');
      const stage = pipelineStages.get('compiler');
      if (stage) {
        stage.metrics.totalEvents++;
        stage.metrics.recentOps.push(ev.lbl || 'compile');
        if (stage.metrics.recentOps.length > 8) stage.metrics.recentOps.shift();
      }
      spawnFlowParticle(new THREE.Vector3(-4, compY, 0), new THREE.Vector3(1, compY, 0), C.compiler);
      callbacks.onLog(`<span style="color:#4cc9f0">compile</span> ${ev.lbl || ''}`);
    } else if (k === EK.ALUDispatch) {
      const substrate = ev.meta['substrate'] || 'cpu';
      const opcode = ev.vals['opcode'] ?? 0;
      const durationNs = ev.vals['duration_ns'] ?? 0;

      alu.totalDispatches++;
      const sub = alu.substrates[substrate] ?? { inflight: 0, totalDispatches: 0, emaDurationNs: 0, lastDurationNs: 0 };
      sub.totalDispatches++;
      sub.lastDurationNs = durationNs;
      sub.emaDurationNs = sub.emaDurationNs * 0.9 + durationNs * 0.1;
      sub.inflight = ev.vals['inflight'] ?? sub.inflight;
      alu.substrates[substrate] = sub;

      alu.recentOps.push({
        timestamp: ev.ts,
        substrate,
        opcode,
        durationNs,
        label: ev.lbl || `op 0x${opcode.toString(16)}`,
      });
      if (alu.recentOps.length > 32) alu.recentOps.shift();

      /* Pipeline pulse */
      pulsePipelineStage(substrate);
      const queueStage = pipelineStages.get('queue');
      if (queueStage) {
        spawnFlowParticle(new THREE.Vector3(1, compY, 0), pipelineStages.get(substrate)?.position ?? new THREE.Vector3(6.5, compY, 0), C.queue);
      }

      callbacks.onALUUpdate(alu);
      callbacks.onLog(`<span style="color:#26c6da">ALU</span> ${substrate} op=0x${opcode.toString(16)} <span style="color:#76ff03">${(durationNs / 1000).toFixed(1)}\u00B5s</span>`);
    } else if (k === EK.FinalizerRun) {
      const stage = pipelineStages.get('compiler');
      if (stage) {
        stage.metrics.totalEvents++;
        stage.metrics.recentOps.push('finalizer: ' + (ev.lbl || ''));
        if (stage.metrics.recentOps.length > 8) stage.metrics.recentOps.shift();
      }
    } else if (k === EK.DatasetRead) {
      pulsePipelineStage('dataset');
      const stage = pipelineStages.get('dataset');
      if (stage) {
        stage.metrics.totalEvents++;
        stage.metrics.bytesProcessed += ev.vals['bytes'] ?? 0;
      }
    } else if (k === EK.TokenizerChunk || k === EK.TokenizerEmit) {
      pulsePipelineStage('tokenizer');
    } else if (k === EK.QueueSubmit) {
      pulsePipelineStage('queue');
      const stage = pipelineStages.get('queue');
      if (stage) {
        stage.metrics.totalEvents++;
        stage.metrics.inflight = ev.vals['inflight'] ?? 0;
      }
    } else if (k === EK.PoolSchedule) {
      pulsePipelineStage('queue');
    } else if (k === EK.PoolComplete) {
      /* Could decrement inflight */
    } else if (k === EK.Prompt) {
      callbacks.onLog(`<span style="color:#ffab00">PROMPT</span> ${ev.lbl || ''}`);
    } else if (k === EK.PromptResult) {
      callbacks.onLog(`<span style="color:#76ff03">RESULT</span> ${ev.lbl || ''}`);
    }

    /* Forward to React */
    callbacks.onEvent(ev);
  }

  /* ── WebSocket connection ─────────────────────────────────────── */

  let ws: WebSocket | null = null;
  let wsReconnectTimer: number | null = null;

  function connectWS() {
    const vizHost = (import.meta as any).env?.VITE_VIZ_HOST || window.location.hostname || 'localhost';
    const vizPort = (import.meta as any).env?.VITE_VIZ_PORT || '6600';
    const wsUrl = `ws://${vizHost}:${vizPort}/ws`;
    try {
      ws = new WebSocket(wsUrl);
      ws.binaryType = 'arraybuffer';
    } catch {
      scheduleReconnect();
      return;
    }

    ws.onopen = () => {
      callbacks.onConnectionChange(true);
      callbacks.onLog('<span style="color:#76ff03">WS connected</span>');
    };

    ws.onmessage = (msg) => {
      if (paused) return;

      let data: Uint8Array;
      if (msg.data instanceof ArrayBuffer) {
        data = new Uint8Array(msg.data);
      } else {
        return;
      }

      const frame = decodeVizMessage(data);
      if (!frame) return;

      if (frame.frameType === 'event') {
        timeline.push(frame.event);
        timelineCursor = timeline.length;
        applyEvent(frame.event);
        callbacks.onTimelineUpdate(timelineCursor, timeline.length);
      } else if (frame.frameType === 'bootstrap') {
        for (const id of frame.nodes) {
          ensureNode(id);
        }
      } else if (frame.frameType === 'stats') {
        droppedCount = frame.dropped;
      } else if (frame.frameType === 'scrub') {
        frame.events.forEach((ev) => {
          timeline.push(ev);
          applyEvent(ev);
        });
        timelineCursor = timeline.length;
        callbacks.onTimelineUpdate(timelineCursor, timeline.length);
      } else if (frame.frameType === 'json') {
        try {
          const obj = JSON.parse(frame.text);
          if (obj.kind !== undefined) applyEvent(obj as VizEvent);
        } catch { /* ignore legacy JSON */ }
      }
    };

    ws.onclose = () => {
      callbacks.onConnectionChange(false);
      scheduleReconnect();
    };

    ws.onerror = () => {
      ws?.close();
    };
  }

  function scheduleReconnect() {
    if (wsReconnectTimer) return;
    wsReconnectTimer = window.setTimeout(() => {
      wsReconnectTimer = null;
      connectWS();
    }, 2000);
  }

  connectWS();

  /* ── stats ────────────────────────────────────────────────────── */

  let lastFrameTime = performance.now();
  let fps = 60;
  let eventsLastSec = 0;
  let eventsPerSec = 0;
  let lastSecTime = performance.now();

  function getStats(): EngineStats {
    let trieCount = 0;
    nodes.forEach((n) => { trieCount += n.tries.length; });
    return {
      nodeCount: nodes.size,
      trieCount,
      edgeCount: edges.size,
      eventCount,
      droppedCount,
      fps: Math.round(fps),
      eventsPerSec: Math.round(eventsPerSec),
    };
  }

  /* ── raycasting / click ───────────────────────────────────────── */

  const raycaster = new THREE.Raycaster();
  const mouse = new THREE.Vector2();

  function onClick(e: MouseEvent) {
    const rect = renderer.domElement.getBoundingClientRect();
    mouse.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
    mouse.y = -((e.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(mouse, camera);

    /* Collect all pickable meshes */
    const pickMeshes: THREE.Object3D[] = [];

    for (const node of nodes.values()) {
      pickMeshes.push(node.core, node.face);
      for (const trie of node.tries) {
        pickMeshes.push(...trie.pickMeshes);
      }
    }

    /* Pipeline stages */
    for (const stage of pipelineStages.values()) {
      pickMeshes.push(stage.mesh);
    }

    /* Static clickables (dataset, machine, network, algo ring/stack) */
    pickMeshes.push(datasetMesh, machineMesh, tokenizerRing, netMesh);
    pickMeshes.push(...algoRingMeshes, ...algoMeshes);

    const hits = raycaster.intersectObjects(pickMeshes);
    if (hits.length > 0) {
      const hit = hits[0].object;
      const ud = hit.userData;

      if (ud.kind === 'trieVertex') {
        selectedTarget = { kind: 'trie', id: ud.kadabraNodeId, trieIndex: ud.trieIdx, vertexVid: ud.graphVertexVid };

        /* Highlight parent node */
        const node = nodes.get(ud.kadabraNodeId);
        if (node) {
          (node.wire.material as THREE.Material).opacity = 0.5;
          focusOn(node.group.position, 14);
        }
      } else if (ud.kind === 'trie') {
        selectedTarget = { kind: 'trie', id: ud.id, trieIndex: ud.trieIndex };
        const node = nodes.get(ud.id);
        if (node) {
          (node.wire.material as THREE.Material).opacity = 0.5;
          focusOn(node.group.position, 14);
        }
      } else if (ud.kind === 'node') {
        selectedTarget = { kind: 'node', id: ud.id };
        const node = nodes.get(ud.id);
        if (node) {
          (node.wire.material as THREE.Material).opacity = 0.5;
          focusOn(node.group.position, 16);
        }
      } else if (ud.kind === 'pipeline') {
        selectedTarget = { kind: 'pipeline', id: ud.id };
        const stage = pipelineStages.get(ud.id);
        if (stage) focusOn(stage.position, 12);
        /* Also handle dataset/machine/tokenizer/network */
        if (ud.id === 'dataset') focusOn(new THREE.Vector3(-18, 1.25, 0), 12);
        if (ud.id === 'machine') focusOn(new THREE.Vector3(-8, 2, 0), 14);
        if (ud.id === 'tokenizer') focusOn(new THREE.Vector3(-8, 2, 0), 14);
        if (ud.id === 'network') focusOn(new THREE.Vector3(-16, compY + 1, 6), 12);
      } else if (ud.kind === 'algorithm') {
        selectedTarget = { kind: 'algorithm', id: ud.id };
      } else {
        selectedTarget = null;
      }

      callbacks.onInspect(selectedTarget);
      return;
    }

    /* Deselect */
    deselectAll();
    selectedTarget = null;
    callbacks.onInspect(null);
  }

  function deselectAll() {
    for (const node of nodes.values()) {
      (node.wire.material as THREE.Material).opacity = 0.35;
    }
  }

  let cameraTarget: { pos: THREE.Vector3; lookAt: THREE.Vector3; active: boolean } = {
    pos: new THREE.Vector3(), lookAt: new THREE.Vector3(), active: false,
  };

  function focusOn(worldPos: THREE.Vector3, distance = 16) {
    cameraTarget.active = true;
    cameraTarget.lookAt.copy(worldPos);
    const dir = camera.position.clone().sub(worldPos).normalize();
    cameraTarget.pos.copy(worldPos).addScaledVector(dir, distance);
    cameraTarget.pos.y = Math.max(cameraTarget.pos.y, worldPos.y + 4);
  }

  renderer.domElement.addEventListener('click', onClick);

  /* ── animation loop ───────────────────────────────────────────── */

  let animationId: number;

  function animate() {
    animationId = requestAnimationFrame(animate);
    const now = performance.now();
    const dt = (now - lastFrameTime) / 1000;
    lastFrameTime = now;
    fps = fps * 0.95 + (1 / Math.max(dt, 0.001)) * 0.05;

    /* Events/sec counter */
    eventsLastSec++;
    if (now - lastSecTime > 1000) {
      eventsPerSec = eventsLastSec;
      eventsLastSec = 0;
      lastSecTime = now;
      callbacks.onStats(getStats());
    }

    const time = now * 0.001;

    /* Camera smooth transition */
    if (cameraTarget.active) {
      camera.position.lerp(cameraTarget.pos, 0.04);
      controls.target.lerp(cameraTarget.lookAt, 0.04);
      if (camera.position.distanceTo(cameraTarget.pos) < 0.5) {
        cameraTarget.active = false;
      }
    }

    /* Dataset particles */
    datasetParticles.forEach((p) => {
      p.userData.t = (p.userData.t + p.userData.sp * dt) % 1;
      const t = p.userData.t;
      p.position.set(-18 + t * 10, 1.25 + Math.sin(t * Math.PI) * 0.25, Math.sin(t * 6) * 0.1);
      (p.material as THREE.Material).opacity = 0.25 + Math.sin(t * Math.PI) * 0.35;
    });

    /* Value particles flowing to DHT nodes */
    const nodeArr = Array.from(nodes.values());
    valueParticles.forEach((v) => {
      v.userData.t = (v.userData.t + v.userData.sp * dt) % 1;
      const t = v.userData.t;
      const ni = v.userData.ni % Math.max(nodeArr.length, 1);
      const target = nodeArr[ni]?.group.position ?? dhtCenter;
      v.position.set(
        -5.5 + t * (target.x + 5.5),
        2 + Math.sin(t * Math.PI) * 1.8,
        t * target.z
      );
      (v.material as THREE.Material).opacity = 0.15 + Math.sin(t * Math.PI) * 0.35;
      if (t > 0.99) v.userData.ni = Math.floor(Math.random() * Math.max(nodeArr.length, 1));
    });

    /* Flow particles */
    flowParticles.forEach((fp) => {
      if (!fp.active) return;
      fp.t += fp.speed * dt;
      if (fp.t >= 1) {
        fp.active = false;
        (fp.mesh.material as THREE.MeshBasicMaterial).opacity = 0;
        return;
      }
      const t = fp.t;
      const u = 1 - t;
      const uu = u * u;
      const tt = t * t;
      fp.tmpPos.copy(fp.from).multiplyScalar(uu);
      fp.tmpPos.addScaledVector(fp.mid, 2 * u * t);
      fp.tmpPos.addScaledVector(fp.to, tt);
      fp.mesh.position.copy(fp.tmpPos);
      (fp.mesh.material as THREE.MeshBasicMaterial).opacity = 0.6 * Math.sin(fp.t * Math.PI);
    });

    /* GF ring rotations */
    globalGF.phase += globalGF.speed * dt;
    updateGFMarker(globalGF);

    for (const node of nodes.values()) {
      node.gfRing.phase += node.gfRing.speed * dt;
      updateGFMarker(node.gfRing);

      /* Pulse decay */
      const pulseOp = (node.pulse.material as THREE.MeshBasicMaterial).opacity;
      if (pulseOp > 0) {
        (node.pulse.material as THREE.MeshBasicMaterial).opacity = Math.max(0, pulseOp - dt * 0.8);
        node.pulse.scale.setScalar(1 + (0.4 - pulseOp) * 2);
      }

      /* Trie GF rings + insert flash */
      node.tries.forEach((trie) => {
        trie.gfRing.phase += trie.gfRing.speed * dt;
        updateGFMarker(trie.gfRing);

        if (trie.state.insertFlash > 0) {
          trie.state.insertFlash = Math.max(0, trie.state.insertFlash - dt * 2);
          (trie.rootMesh.material as THREE.MeshBasicMaterial).opacity = 0.7 + trie.state.insertFlash * 0.3;
        }
      });

      /* Beam ray animation */
      node.beamRays.forEach((ray) => {
        if (ray.active) {
          const pulse = Math.sin(time * 1.5 + Math.random() * 0.01);
          ray.mat.opacity = Math.max(0, pulse * 0.3);
          ray.tipMat.opacity = Math.max(0, pulse * 0.5);
        } else {
          ray.mat.opacity = Math.max(0, ray.mat.opacity - dt * 2);
          ray.tipMat.opacity = Math.max(0, ray.tipMat.opacity - dt * 2);
        }
      });

      /* Beam hypothesis orbiting */
      node.beamHypMeshes.forEach((hm, i) => {
        const angle = time * 0.5 + (i / Math.max(node.beamHypMeshes.length, 1)) * Math.PI * 2;
        const orbitR = 2.5;
        hm.position.set(Math.cos(angle) * orbitR, 1.5 + Math.sin(angle * 2) * 0.3, Math.sin(angle) * orbitR);

        const hyp = node.beam.hypotheses[i];
        if (hyp) {
          const score01 = Math.max(0, Math.min(1, (hyp.score + 15) / 15));
          (hm.material as THREE.MeshBasicMaterial).color.lerpColors(
            new THREE.Color(C.beamRejected), new THREE.Color(C.beamActive), score01
          );
        }
      });
    }

    /* Pipeline stage pulse decay */
    for (const stage of pipelineStages.values()) {
      const op = (stage.pulseRing.material as THREE.MeshBasicMaterial).opacity;
      if (op > 0) {
        (stage.pulseRing.material as THREE.MeshBasicMaterial).opacity = Math.max(0, op - dt * 1.2);
        stage.pulseRing.scale.setScalar(1 + (0.5 - op) * 1.5);
      }
    }

    /* Phase dials */
    dials.forEach((d) => {
      d.phase += d.speed * dt;
      const tipX = Math.cos(d.phase) * d.radius * 0.85;
      const tipZ = Math.sin(d.phase) * d.radius * 0.85;
      d.tip.position.set(tipX, 0.04, tipZ);
      d.needlePos.setXYZ(1, tipX, 0.04, tipZ);
      d.needlePos.needsUpdate = true;
      d.needle.geometry.computeBoundingSphere();
    });

    /* Edge activity decay */
    for (const edge of edges.values()) {
      if (edge.activity > 0) {
        edge.activity = Math.max(0, edge.activity - dt * 0.5);
        (edge.line.material as THREE.LineBasicMaterial).opacity = 0.1 + edge.activity * 0.4;
      }
    }

    controls.update();
    composer.render();
  }

  /* ── resize ───────────────────────────────────────────────────── */

  function onResize() {
    camera.aspect = container.clientWidth / container.clientHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(container.clientWidth, container.clientHeight);
    composer.setSize(container.clientWidth, container.clientHeight);
  }
  window.addEventListener('resize', onResize);

  animate();

  /* ── public API ───────────────────────────────────────────────── */

  return {
    destroy() {
      cancelAnimationFrame(animationId);
      window.removeEventListener('resize', onResize);
      renderer.domElement.removeEventListener('click', onClick);
      controls.dispose();

      ws?.close();
      ws = null;

      if (wsReconnectTimer !== null) {
        clearTimeout(wsReconnectTimer);
        wsReconnectTimer = null;
      }

      for (const node of nodes.values()) {
        scene.remove(node.group);
        disposeObjectTree(node.group);
      }

      nodes.clear();

      for (const edge of edges.values()) {
        scene.remove(edge.line);
        disposeObjectTree(edge.line);
      }

      edges.clear();

      for (const particleMesh of datasetParticles) {
        scene.remove(particleMesh);
        disposeObjectTree(particleMesh);
      }

      for (const particleMesh of valueParticles) {
        scene.remove(particleMesh);
        disposeObjectTree(particleMesh);
      }

      for (const fp of flowParticles) {
        scene.remove(fp.mesh);
        disposeObjectTree(fp.mesh);
      }

      disposeObjectTree(scene);

      renderer.dispose();
      composer.dispose();
      renderer.forceContextLoss();
      container.removeChild(renderer.domElement);
    },

    closeInspector() {
      deselectAll();
      selectedTarget = null;
    },

    togglePause() {
      paused = !paused;
      return paused;
    },

    isPaused() { return paused; },

    scrubTo(index: number) {
      if (index < 0 || index > timeline.length) return;
      resetDynamicVisualization();
      for (let idx = 0; idx < index; idx++) {
        applyEvent(timeline[idx]);
      }
      timelineCursor = index;
      callbacks.onTimelineUpdate(timelineCursor, timeline.length);
      callbacks.onStats(getStats());
    },

    stepForward() {
      if (timelineCursor < timeline.length) {
        applyEvent(timeline[timelineCursor]);
        timelineCursor++;
        callbacks.onTimelineUpdate(timelineCursor, timeline.length);
      }
    },

    stepBackward() {
      /* Can't un-apply events easily; just report cursor */
      if (timelineCursor > 0) {
        timelineCursor--;
        callbacks.onTimelineUpdate(timelineCursor, timeline.length);
      }
    },

    sendPrompt(text: string) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'prompt', text }));
      }
    },

    getNodeState(id: string): NodeState | null {
      return nodes.get(id)?.data ?? null;
    },

    getBeamState(id: string): BeamState | null {
      const node = nodes.get(id);
      if (!node) return null;
      return {
        activeCount: node.beam.activeCount,
        rejectedCount: node.beam.rejectedCount,
        bestScore: node.beam.bestScore,
        lastSequence: node.beam.lastSequence,
        hypotheses: node.beam.hypotheses,
        converged: node.beam.converged,
      };
    },

    getTrieState(nodeId: string, trieIdx: number): TrieState | null {
      const node = nodes.get(nodeId);
      return node?.tries[trieIdx]?.state ?? null;
    },

    getALUState(): ALUState { return alu; },

    getPipelineState(id: string): PipelineStageState | null {
      return pipelineStages.get(id)?.metrics ?? null;
    },

    getFieldState(): FieldState { return fieldState; },

    getStats,
  };
}
