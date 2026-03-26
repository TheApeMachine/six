/* ═══════════════════════════════════════════════════════════
   main.js — Entry point / orchestrator
   Bootstraps all modules and runs the animation loop.
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import {
  scene, camera, renderer, labelRenderer, controls,
  foldLayer, raycaster, mouseVec,
  updateLabelZoom, updateAmbientParticles,
  flyTo, updateFlyAnimation,
} from './scene.js';
import {
  SYS, zonePlanes,
  buildArchitecture,
  clearAllZoneLabels,
  setZoneHover, updateZonePulses,
} from './architecture.js';
import { buildValueRing, animateValueRing } from './value-viz.js';
import {
  updateDataStreams, clearDataStreams,
  buildFlowParticles, updateFlowParticles,
} from './particles.js';
import * as state from './state.js';
import {
  recordEvent, isReplayMode, getRecordingLength,
  enterReplayMode, enterLiveMode, replayTo,
  startPlayback, pausePlayback, stepForward,
  exportRecording, importRecording, initRecording,
} from './recording.js';
import { initWebSocket, connect, sendPrompt } from './websocket.js';
import { initEventHandler, handleEvent } from './event-handler.js';
import {
  initInspector, openInspector, closeInspector, refreshInspectorIfMatch,
  showEventDetail, closeEventDetail, isDetailOpen,
} from './inspector.js';

// ═══════════════════════════════════════════════════════════
// DOM REFERENCES
// ═══════════════════════════════════════════════════════════
const logBox = document.getElementById('log-container');
const statEvents = document.getElementById('stat-events');
const statIngested = document.getElementById('stat-ingested');
const statConn = document.getElementById('stat-conn');
const statReplay = document.getElementById('stat-replay');
const statLastOp = document.getElementById('stat-last-op');
const chunkText = document.getElementById('chunk-text');
const resultText = document.getElementById('result-text');
const densityFill = document.getElementById('density-fill');

const slider = document.getElementById('replay-slider');
const replayInfo = document.getElementById('replay-info');
const btnRec = document.getElementById('btn-rec');
const btnPlay = document.getElementById('btn-play');
const btnPause = document.getElementById('btn-pause');
const btnStep = document.getElementById('btn-step');
const btnExport = document.getElementById('btn-export');
const btnImport = document.getElementById('btn-import');
const importFile = document.getElementById('import-file');
const promptInput = document.getElementById('prompt-input');
const promptSend = document.getElementById('prompt-send');
const logSearch = document.getElementById('log-search');

const stageElements = {
  tokenize: document.getElementById('stage-tokenize'),
  insert:   document.getElementById('stage-insert'),
  lookup:   document.getElementById('stage-lookup'),
  fold:     document.getElementById('stage-fold'),
  decode:   document.getElementById('stage-decode'),
};
const stageInfoElements = {
  tokenize: document.getElementById('stage-tokenize-info'),
  insert:   document.getElementById('stage-insert-info'),
  lookup:   document.getElementById('stage-lookup-info'),
  fold:     document.getElementById('stage-fold-info'),
  decode:   document.getElementById('stage-decode-info'),
};

// ═══════════════════════════════════════════════════════════
// PIPELINE STAGE MANAGEMENT
// ═══════════════════════════════════════════════════════════
let clearPipelineTimer = null;

function activateStage(name, info) {
  for (const el of Object.values(stageElements)) {
    if (el.classList.contains('active')) {
      el.classList.remove('active');
      el.classList.add('done');
    }
  }
  const el = stageElements[name];
  if (el) { el.classList.remove('done'); el.classList.add('active'); }
  if (info && stageInfoElements[name]) stageInfoElements[name].textContent = info;

  if (clearPipelineTimer) clearTimeout(clearPipelineTimer);
  clearPipelineTimer = setTimeout(() => {
    Object.values(stageElements).forEach(el => { if (el) el.classList.remove('active', 'done'); });
    Object.values(stageInfoElements).forEach(el => { if (el) el.textContent = ''; });
  }, 3000);
}

// ═══════════════════════════════════════════════════════════
// SPARKLINE
// ═══════════════════════════════════════════════════════════
const sparkCanvas = document.getElementById('sparkline-canvas');
const sparkCtx = sparkCanvas.getContext('2d');

function pushSparkDensity(density, action) {
  state.sparkData.push({ density, action });
  if (state.sparkData.length > state.SPARK_MAX) state.sparkData.shift();
  drawSparkline();

  if (density > 0) {
    densityFill.style.height = `${Math.min(density * 100, 100) * 1.4}px`;
  }
}

function drawSparkline() {
  const w = sparkCanvas.width;
  const h = sparkCanvas.height;
  sparkCtx.clearRect(0, 0, w, h);

  if (state.sparkData.length < 2) return;

  sparkCtx.strokeStyle = 'rgba(64, 128, 192, 0.12)';
  sparkCtx.lineWidth = 1;
  sparkCtx.beginPath();
  for (const pct of [0.25, 0.5, 0.75]) {
    const gy = h - pct * h;
    sparkCtx.moveTo(0, gy);
    sparkCtx.lineTo(w, gy);
  }
  sparkCtx.stroke();

  const step = w / (state.SPARK_MAX - 1);
  sparkCtx.strokeStyle = 'rgba(140, 200, 255, 0.6)';
  sparkCtx.lineWidth = 1;
  sparkCtx.beginPath();
  for (let i = 0; i < state.sparkData.length; i++) {
    const x = i * step;
    const y = h - state.sparkData[i].density * h;
    if (i === 0) sparkCtx.moveTo(x, y); else sparkCtx.lineTo(x, y);
  }
  sparkCtx.stroke();

  const last = state.sparkData[state.sparkData.length - 1];
  sparkCtx.fillStyle = 'rgba(140, 200, 255, 0.5)';
  sparkCtx.font = '7px IBM Plex Mono';
  sparkCtx.fillText(`${(last.density * 257).toFixed(0)}/257`, w - 48, 10);
}

// ═══════════════════════════════════════════════════════════
// FOLD TREE (3D)
// ═══════════════════════════════════════════════════════════
const foldNodes = [];
const foldLevelCounts = {};
const MAX_FOLD_NODES = 30;
const foldLineMats = {
  0: new THREE.LineBasicMaterial({ color: 0x6090c0, transparent: true, opacity: 0.3 }),
  1: new THREE.LineBasicMaterial({ color: 0x5080b0, transparent: true, opacity: 0.22 }),
  deep: new THREE.LineBasicMaterial({ color: 0x4070a0, transparent: true, opacity: 0.15 }),
};

function addFoldNode(bin, level, density, text, childCount) {
  const sys = SYS.chamber;
  foldLevelCounts[level] = (foldLevelCounts[level] || 0) + 1;
  const indexInLevel = foldLevelCounts[level] - 1;

  const levelSpread = sys.w * 0.8;
  const maxPerLevel = Math.max(3, 6 - level);
  const slot = indexInLevel % maxPerLevel;
  const xOffset = (slot - (maxPerLevel - 1) / 2) * (levelSpread / maxPerLevel);

  const x = sys.x + xOffset;
  const y = sys.depth + 1.0 + level * 2.5;
  const z = sys.z + 0.5;

  const levelClass = level === 0 ? 'level-0' : level === 1 ? 'level-1' : level === 2 ? 'level-2' : 'level-deep';
  const div = document.createElement('div');
  div.className = `fold-label ${levelClass}`;

  const textSpan = document.createElement('span');
  textSpan.className = 'fold-text';
  textSpan.textContent = (text || '').trim() || '[empty]';
  div.appendChild(textSpan);

  const metaSpan = document.createElement('span');
  metaSpan.className = 'fold-meta';
  metaSpan.textContent = `L${level} bin=${bin} ${(density * 100).toFixed(0)}% ch=${childCount || 0}`;
  div.appendChild(metaSpan);

  const lbl = new CSS2DObject(div);
  lbl.position.set(x, y, z);
  foldLayer.add(lbl);

  const pos = new THREE.Vector3(x, y, z);
  const node = { lbl, pos, level, bin };

  if (level > 0 && foldNodes.length > 0) {
    const parentCandidates = foldNodes.filter(n => n.level === level - 1);
    const parent = parentCandidates.length > 0 ? parentCandidates[parentCandidates.length - 1] : foldNodes[foldNodes.length - 1];
    const mat = foldLineMats[level] || foldLineMats.deep;
    const lineGeo = new THREE.BufferGeometry().setFromPoints([parent.pos, pos]);
    const line = new THREE.Line(lineGeo, mat);
    foldLayer.add(line);
    node.line = line;
  }

  foldNodes.push(node);

  while (foldNodes.length > MAX_FOLD_NODES) {
    const old = foldNodes.shift();
    foldLayer.remove(old.lbl);
    if (old.line) { foldLayer.remove(old.line); old.line.geometry.dispose(); }
  }
}

function clearFoldNodes() {
  for (const n of foldNodes) {
    foldLayer.remove(n.lbl);
    if (n.line) { foldLayer.remove(n.line); n.line.geometry.dispose(); }
  }
  foldNodes.length = 0;
  for (const key of Object.keys(foldLevelCounts)) delete foldLevelCounts[key];
}

function resetFoldGraph() {
  clearFoldNodes();
  state.set('totalFoldLinks', 0);
  state.set('totalFolds', 0);
}

// ═══════════════════════════════════════════════════════════
// LOG
// ═══════════════════════════════════════════════════════════
function log(text, type = 'state', eventRef = null) {
  const div = document.createElement('div');
  div.className = `log-line log-${type}`;
  div.dataset.type = type;
  const t = new Date().toISOString().split('T')[1].slice(0, -1);
  div.textContent = `${t} ${text}`;

  if (eventRef) {
    div.addEventListener('click', () => showEventDetail(eventRef));
  }

  logBox.insertBefore(div, logBox.firstChild);
  while (logBox.children.length > 80) logBox.removeChild(logBox.lastChild);
  applyLogFilters();
}

let logFilterType = 'all';
const LOG_TYPE_MAP = {
  pipeline: ['pipeline', 'result', 'substrate'],
  ingest: ['ingest'],
  fold: ['fold'],
  compute: ['compute', 'kernel'],
};

function applyLogFilters() {
  const query = (logSearch.value || '').toLowerCase();
  for (const line of logBox.children) {
    const text = line.textContent.toLowerCase();
    const type = line.dataset.type || '';
    let matchFilter = logFilterType === 'all';
    if (!matchFilter && LOG_TYPE_MAP[logFilterType]) {
      matchFilter = LOG_TYPE_MAP[logFilterType].includes(type);
    }
    const matchSearch = !query || text.includes(query);
    line.classList.toggle('filtered-out', !(matchFilter && matchSearch));
  }
}

logSearch.addEventListener('input', applyLogFilters);
document.querySelectorAll('.log-filter-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    logFilterType = btn.dataset.filter;
    document.querySelectorAll('.log-filter-btn').forEach(b => {b.classList.toggle('active', b === btn)});
    applyLogFilters();
  });
});

// ═══════════════════════════════════════════════════════════
// GRAPH (SVG 2D — kept for graph events)
// ═══════════════════════════════════════════════════════════
const graphNodes = new Map();
const graphEdges = [];
const graphEdgeSet = new Set();
const graphEdgesG = document.getElementById('graph-edges');
const graphNodesG = document.getElementById('graph-nodes');

function graphAddNode(id, tokens, type) {
  if (graphNodes.has(id)) return;
  const w = 800, cols = Math.max(3, Math.ceil(Math.sqrt(graphNodes.size + 1) * 1.5));
  const existing = graphNodes.size;
  const col = existing % cols, row = Math.floor(existing / cols);
  const x = 80 + col * ((w - 160) / Math.max(cols - 1, 1));
  const y = 60 + row * 80;
  graphNodes.set(id, { id, tokens, type, x, y, vx: 0, vy: 0 });
  graphDirty = true;
}

function graphAddEdge(fromId, toId) {
  const key = `${fromId}:${toId}`;
  if (graphEdgeSet.has(key)) return;
  graphEdgeSet.add(key);
  graphEdges.push({ from: fromId, to: toId });
  graphDirty = true;
}

function graphClear() {
  graphNodes.clear();
  graphEdges.length = 0;
  graphEdgeSet.clear();
  graphDirty = false;
  graphEdgesG.innerHTML = '';
  graphNodesG.innerHTML = '';
}

// ═══════════════════════════════════════════════════════════
// RESET VISUALIZATION
// ═══════════════════════════════════════════════════════════
function resetVisualization() {
  graphClear();
  clearAllZoneLabels();
  clearFoldNodes();
  clearDataStreams();
  state.resetCounters();
  drawSparkline();

  statIngested.textContent = '0';
  chunkText.textContent = '—';
  resultText.style.display = 'none';
  if (statLastOp) statLastOp.textContent = '—';
  logBox.innerHTML = '';
}

// ═══════════════════════════════════════════════════════════
// HUD UPDATE
// ═══════════════════════════════════════════════════════════
function updateHud() {
  statEvents.textContent = getRecordingLength();
  statIngested.textContent = state.totalIngested;
}

// ═══════════════════════════════════════════════════════════
// INITIALIZE MODULES
// ═══════════════════════════════════════════════════════════
buildArchitecture();
buildValueRing();
buildFlowParticles();
initInspector();

initRecording(handleEvent, resetVisualization);
initEventHandler({
  updateHud,
  refreshInspector: refreshInspectorIfMatch,
  log,
  activateStage,
  pushSpark: pushSparkDensity,
  addFoldNode,
  resetFoldGraph,
  graphAddNode,
  graphAddEdge,
});

initWebSocket(
  // onEvent
  (ev) => {
    recordEvent(ev);
    if (!isReplayMode()) handleEvent(ev);
    // Update slider
    slider.max = getRecordingLength() - 1;
    replayInfo.textContent = `${getRecordingLength()} events`;
    statEvents.textContent = getRecordingLength();
  },
  // onConnect
  () => {
    statConn.textContent = 'connected';
    statConn.style.color = 'rgba(140, 255, 200, 0.75)';
    if (promptSend) promptSend.disabled = false;
    log('Connected to SIX telemetry', 'state');
  },
  // onDisconnect
  () => {
    statConn.textContent = 'offline';
    statConn.style.color = 'rgba(255, 140, 120, 0.7)';
    if (promptSend) promptSend.disabled = true;
    log('Disconnected', 'state');
  },
);

// ═══════════════════════════════════════════════════════════
// EVENT BINDINGS
// ═══════════════════════════════════════════════════════════
btnRec.addEventListener('click', () => {
  enterLiveMode();
  statReplay.textContent = 'live';
  statReplay.style.color = 'rgba(140, 255, 200, 0.75)';
  btnRec.classList.add('active');
});

btnPlay.addEventListener('click', () => {
  enterReplayMode();
  statReplay.textContent = 'replay';
  statReplay.style.color = 'rgba(255, 220, 140, 0.8)';
  btnRec.classList.remove('active');
  startPlayback((idx, len) => {
    slider.value = idx;
    replayInfo.textContent = `${idx + 1} / ${len}`;
  });
});

btnPause.addEventListener('click', pausePlayback);

btnStep.addEventListener('click', () => {
  if (!isReplayMode()) {
    enterReplayMode();
    statReplay.textContent = 'replay';
    statReplay.style.color = 'rgba(255, 220, 140, 0.8)';
    btnRec.classList.remove('active');
  }
  const idx = stepForward();
  slider.value = idx;
  replayInfo.textContent = `${idx + 1} / ${getRecordingLength()}`;
});

btnExport.addEventListener('click', exportRecording);
btnImport.addEventListener('click', () => importFile.click());
importFile.addEventListener('change', async (e) => {
  if (e.target.files.length) {
    const count = await importRecording(e.target.files[0]);
    if (count > 0) {
      slider.max = count - 1;
      replayInfo.textContent = `${count} events`;
      statEvents.textContent = count;
      log(`Loaded ${count} events`, 'state');
    }
  }
});

if (promptSend) {
  promptSend.addEventListener('click', () => {
    if (sendPrompt(promptInput.value)) {
      promptInput.value = '';
      log(`Sent prompt`, 'pipeline');
    }
  });
}
if (promptInput) {
  promptInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      if (sendPrompt(promptInput.value)) {
        promptInput.value = '';
        log(`Sent prompt`, 'pipeline');
      }
    }
  });
}

slider.addEventListener('input', () => {
  if (!isReplayMode()) {
    enterReplayMode();
    statReplay.textContent = 'replay';
    statReplay.style.color = 'rgba(255, 220, 140, 0.8)';
    btnRec.classList.remove('active');
  }
  pausePlayback();
  replayTo(parseInt(slider.value, 10));
  replayInfo.textContent = `${parseInt(slider.value, 10) + 1} / ${getRecordingLength()}`;
});

// ── Zone Click / Hover ──
let mouseDownPos = null;

renderer.domElement.addEventListener('pointerdown', (e) => {
  mouseDownPos = { x: e.clientX, y: e.clientY };
});

renderer.domElement.addEventListener('pointerup', (e) => {
  if (!mouseDownPos) return;
  const dx = e.clientX - mouseDownPos.x;
  const dy = e.clientY - mouseDownPos.y;
  if (Math.abs(dx) > 4 || Math.abs(dy) > 4) return;

  mouseVec.x = (e.clientX / innerWidth) * 2 - 1;
  mouseVec.y = -(e.clientY / innerHeight) * 2 + 1;
  raycaster.setFromCamera(mouseVec, camera);

  const planes = Object.values(zonePlanes);
  const hits = raycaster.intersectObjects(planes);
  if (hits.length > 0) {
    openInspector(hits[0].object.userData.sysKey);
  }
});

renderer.domElement.addEventListener('dblclick', (e) => {
  mouseVec.x = (e.clientX / innerWidth) * 2 - 1;
  mouseVec.y = -(e.clientY / innerHeight) * 2 + 1;
  raycaster.setFromCamera(mouseVec, camera);

  const planes = Object.values(zonePlanes);
  const hits = raycaster.intersectObjects(planes);
  if (hits.length > 0) {
    const sysKey = hits[0].object.userData.sysKey;
    const sys = SYS[sysKey];
    if (sys) {
      const camPos = new THREE.Vector3(
        sys.x + 10,
        sys.depth + 8,
        sys.z + 10,
      );
      flyTo(camPos, sys.center);
    }
  }
});

renderer.domElement.addEventListener('pointermove', (e) => {
  mouseVec.x = (e.clientX / innerWidth) * 2 - 1;
  mouseVec.y = -(e.clientY / innerHeight) * 2 + 1;
  raycaster.setFromCamera(mouseVec, camera);

  const planes = Object.values(zonePlanes);
  const hits = raycaster.intersectObjects(planes);

  if (hits.length > 0) {
    setZoneHover(hits[0].object.userData.sysKey);
    renderer.domElement.style.cursor = 'pointer';
  } else {
    setZoneHover(null);
    renderer.domElement.style.cursor = '';
  }
});

// ── Keyboard Shortcuts ──
document.addEventListener('keydown', (e) => {
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

  if (e.key === 'Escape') {
    if (isDetailOpen()) closeEventDetail();
    else if (state.inspectorOpen) closeInspector();
  }

  if (e.key === 'i' || e.key === 'I') {
    if (state.inspectorOpen) closeInspector();
  }

  const zoneKeys = { '1': 'machine', '2': 'dataset', '3': 'frame', '4': 'chamber', '5': 'kernel' };
  if (zoneKeys[e.key]) {
    openInspector(zoneKeys[e.key]);
    // Fly to zone
    const sys = SYS[zoneKeys[e.key]];
    if (sys) {
      flyTo(
        new THREE.Vector3(sys.x + 10, sys.depth + 8, sys.z + 10),
        sys.center,
      );
    }
  }

  // Home key to reset camera
  if (e.key === 'Home' || e.key === '0') {
    flyTo(
      new THREE.Vector3(0, 42, 38),
      new THREE.Vector3(0, 2, 0),
    );
  }
});

// ═══════════════════════════════════════════════════════════
// ANIMATION LOOP
// ═══════════════════════════════════════════════════════════
function animate(time) {
  requestAnimationFrame(animate);

  updateDataStreams();
  updateFlowParticles(time);
  updateLabelZoom();
  updateAmbientParticles(time);
  updateFlyAnimation();
  updateZonePulses();
  animateValueRing(time);

  controls.update();
  renderer.render(scene, camera);
  labelRenderer.render(scene, camera);
}

animate(0);
connect();

// Fade nav hint
setTimeout(() => {
  const hint = document.getElementById('nav-hint');
  if (hint) hint.style.opacity = '0';
}, 8000);
