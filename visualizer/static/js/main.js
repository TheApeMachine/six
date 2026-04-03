/* ═══════════════════════════════════════════════════════════
   main.js — Entry point / orchestrator
   Bootstraps all modules and runs the animation loop.
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import {
  scene, camera, renderer, labelRenderer, controls,
  foldLayer, raycaster, mouseVec,
  updateLabelZoom,
  flyTo, updateFlyAnimation,
  GRAPH_STRIP_H,
} from './scene.js';
import {
  SYS, zonePlanes,
  buildArchitecture,
  clearAllZoneLabels,
  setZoneHover, updateZonePulses,
  animateStreamRing, advanceStreamPtr,
  spawnFoldEffect, updateFoldEffects,
  animateUniConn,
  setStreamRegionCount, rebuildStreamRing,
} from './architecture.js';
import {
  buildValueRing,
  animateValueRing,
  setValueLayout,
  decodeValueFrame,
  getSubstrateRuntime,
} from './value-viz.js';
import { buildSystemOrbit, animateSystemOrbit, setSystemTopology } from './system-viz.js';
import {
  updateDataStreams, clearDataStreams,
  buildFlowParticles, updateFlowParticles,
} from './particles.js';
import * as state from './state.js';
import {
  recording,
  recordEvent, isReplayMode, getRecordingLength,
  enterReplayMode, enterLiveMode, replayTo,
  startPlayback, pausePlayback, stepForward,
  exportRecording, importRecording, initRecording,
} from './recording.js';
import { initWebSocket, connect, sendPrompt, sendIngest } from './websocket.js';
import { initEventHandler, handleEvent } from './event-handler.js';
import {
  initInspector, openInspector, closeInspector, refreshInspectorIfMatch,
  openValueInspector, showEventDetail, closeEventDetail, isDetailOpen,
} from './inspector.js';
import {
  graphAddNode as graph2dAddNode,
  graphAddEdge as graph2dAddEdge,
  graphAddValueNode as graph2dAddValueNode,
  graphClear as graph2dClear,
  fitGraph as graph2dFit,
} from './graph2d.js';

// ═══════════════════════════════════════════════════════════
// DOM REFERENCES
// ═══════════════════════════════════════════════════════════
function getRequired(id) {
  const el = document.getElementById(id);
  if (!el) throw new Error(`Six viz: missing required DOM element #${id}`);
  return el;
}

const logBox = getRequired('log-container');
const statEvents = getRequired('stat-events');
const statIngested = getRequired('stat-ingested');
const statConn = getRequired('stat-conn');
const statReplay = getRequired('stat-replay');
const statLastOp = getRequired('stat-last-op');
const chunkText = getRequired('chunk-text');
const resultText = getRequired('result-text');
const densityFill = getRequired('density-fill');

const slider = getRequired('replay-slider');
const replayInfo = getRequired('replay-info');
const btnRec = getRequired('btn-rec');
const btnPlay = getRequired('btn-play');
const btnPause = getRequired('btn-pause');
const btnStep = getRequired('btn-step');
const btnExport = getRequired('btn-export');
const btnImport = getRequired('btn-import');
const importFile = getRequired('import-file');
const inputBar = getRequired('input-bar');
const ingestPanel = getRequired('ingest-panel');
const ingestInput = getRequired('ingest-input');
const ingestSend = getRequired('ingest-send');
const ingestClose = getRequired('ingest-close');
const ingestClear = getRequired('ingest-clear');
const ingestToggle = getRequired('ingest-toggle');
const ingestStatus = getRequired('ingest-status');
const ingestHint = getRequired('ingest-hint');
const promptInput = getRequired('prompt-input');
const promptSend = getRequired('prompt-send');
const logSearch = getRequired('log-search');

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
const sparkCanvas = getRequired('sparkline-canvas');
const sparkCtx = sparkCanvas.getContext('2d');

let _sparkDirty = false;
let _sparkRAF = 0;

function pushSparkDensity(density, action) {
  state.sparkData.push({ density, action });
  if (state.sparkData.length > state.SPARK_MAX) state.sparkData.shift();

  // Coalesce rapid-fire spark updates into a single repaint
  if (!_sparkDirty) {
    _sparkDirty = true;
    _sparkRAF = requestAnimationFrame(() => {
      _sparkDirty = false;
      drawSparkline();
    });
  }

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
  const sys = SYS.backend || SYS.machine;
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
    if (old.line) {
      foldLayer.remove(old.line);
      old.line.geometry.dispose();
      const m = old.line.material;
      if (m) (Array.isArray(m) ? m : [m]).forEach((mat) => { mat.dispose(); });
    }
  }
}

function clearFoldNodes() {
  for (const n of foldNodes) {
    foldLayer.remove(n.lbl);
    if (n.line) {
      foldLayer.remove(n.line);
      n.line.geometry.dispose();
      const m = n.line.material;
      if (m) (Array.isArray(m) ? m : [m]).forEach((mat) => { mat.dispose(); });
    }
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
let _logFilterDirty = false;

const logDivPool = [];
function getLogDiv() {
  if (logDivPool.length > 0) {
    const div = logDivPool.pop();
    div.onclick = null;
    div.className = '';
    div.classList.remove('filtered-out');
    return div;
  }
  return document.createElement('div');
}

function releaseLogDiv(div) {
  logDivPool.push(div);
}

function log(text, type = 'state', eventRef = null) {
  const div = getLogDiv();
  div.className = `log-line log-${type}`;
  div.dataset.type = type;
  const t = new Date().toISOString().split('T')[1].slice(0, -1);
  div.textContent = `${t} ${text}`;

  if (eventRef) {
    div.onclick = () => showEventDetail(eventRef);
  }

  // Apply filter to the new line only (avoids re-scanning all children)
  if (logFilterType !== 'all') {
    const matchFilter = LOG_TYPE_MAP[logFilterType]
      ? LOG_TYPE_MAP[logFilterType].includes(type)
      : false;
    const query = (logSearch.value || '').toLowerCase();
    const matchSearch = !query || `${t} ${text}`.toLowerCase().includes(query);
    if (!(matchFilter && matchSearch)) div.classList.add('filtered-out');
  } else {
    const query = (logSearch.value || '').toLowerCase();
    if (query && !`${t} ${text}`.toLowerCase().includes(query)) {
      div.classList.add('filtered-out');
    }
  }

  logBox.insertBefore(div, logBox.firstChild);
  while (logBox.children.length > 80) {
    const old = logBox.lastChild;
    logBox.removeChild(old);
    releaseLogDiv(old);
  }
}

let logFilterType = 'all';
const LOG_TYPE_MAP = {
  pipeline: ['pipeline', 'result', 'substrate'],
  ingest: ['ingest'],
  fold: ['fold'],
  compute: ['compute', 'kernel', 'backend', 'pool'],
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
// GRAPH — delegates to graph2d.js (2D SVG strip)
// ═══════════════════════════════════════════════════════════
// Wire click handler so graph nodes open the value inspector
window._graphNodeClick = (id) => openValueInspector(id);

// Thin wrappers that delegate to graph2d module
function graphAddNode(id, tokens, type, extra = {}) {
  return graph2dAddNode(id, tokens, type, extra);
}

function graphAddEdge(fromId, toId, kind = 'link') {
  graph2dAddEdge(fromId, toId, kind);
}

function graphClear() {
  graph2dClear();
}

function graphAddValueNode(snapshot) {
  return graph2dAddValueNode(snapshot);
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
  _prevUbActivitySig = '';
}

// ═══════════════════════════════════════════════════════════
// HUD UPDATE
// ═══════════════════════════════════════════════════════════
function updateValueHud(snapshot) {
  if (!snapshot) return;
  _prevUbActivitySig = '';
  chunkText.textContent = snapshot.tokenPreview || snapshot.tokenText || '—';
  resultText.style.display = 'block';
  resultText.textContent = snapshot.summary || snapshot.tokenPreview || 'Value snapshot';
  if (statLastOp) statLastOp.textContent = snapshot.summary || '—';
}

let _prevHudEvents = -1;
let _prevHudIngested = -1;
let _prevHudOp = '';
let _prevUbActivitySig = '';

function updateHud() {
  const evts = getRecordingLength();
  if (evts !== _prevHudEvents) {
    _prevHudEvents = evts;
    statEvents.textContent = evts;
  }
  if (state.totalIngested !== _prevHudIngested) {
    _prevHudIngested = state.totalIngested;
    statIngested.textContent = state.totalIngested;
  }
  const op = state.lastValueSummary || '—';
  if (statLastOp && op !== _prevHudOp) {
    _prevHudOp = op;
    statLastOp.textContent = op;
  }

  const ubSig = `${state.activityUbShort}\n${state.activityUbDetail}`;
  if (state.totalValueFrames === 0 && ubSig.trim() && ubSig !== _prevUbActivitySig) {
    _prevUbActivitySig = ubSig;
    chunkText.textContent = state.activityUbShort || '—';
    resultText.style.display = 'block';
    resultText.textContent = state.activityUbDetail
      || 'UniversalBitwise slot telemetry (executeBatch). Instruction words are not LSM keys.';
  }
}

function formatSubstrateHud(sub) {
  if (!sub || typeof sub !== 'object') {
    return '—';
  }
  const lines = [
    [
      'substrate=unified',
      `batchSize=${sub.batchSize}`,
      `batchWindow=${sub.batchWindow || '—'}`,
      `evoWindow=${sub.evolutionBatchWindow || '—'}`,
      `sleepTick=${sub.substrateSleepIdle || '—'}`,
    ].filter(Boolean).join(' · '),
    sub.execSummary || '',
    sub.ingressSummary || '',
    sub.programModel || '',
    sub.ubTelemetryNote || '',
  ].filter((line) => line && String(line).trim());

  return lines.length ? lines.join('\n') : '—';
}

const hudSubstrateEl = document.getElementById('hud-substrate');

function applySubstrateHud() {
  if (!hudSubstrateEl) return;
  hudSubstrateEl.textContent = formatSubstrateHud(getSubstrateRuntime());
}

async function loadValueLayout() {
  try {
    const res = await fetch('/api/layout', { cache: 'no-store' });
    if (!res.ok) return;
    const layout = await res.json();
    setValueLayout(layout);
    applySubstrateHud();
  } catch (err) {
    console.warn('Six viz layout fetch failed:', err);
  }
}

async function loadSystemTopology() {
  try {
    const res = await fetch('/api/system', { cache: 'no-store' });
    if (!res.ok) return;
    const topology = await res.json();
    setSystemTopology(topology);
    if (topology.streamRegions != null) {
      setStreamRegionCount(topology.streamRegions);
      rebuildStreamRing();
    }
  } catch (err) {
    console.warn('Six viz system topology fetch failed:', err);
  }
}

// ═══════════════════════════════════════════════════════════
// INITIALIZE MODULES
// ═══════════════════════════════════════════════════════════
buildArchitecture();
buildFlowParticles();
initInspector();
Promise.all([loadValueLayout(), loadSystemTopology()]).finally(() => {
  buildValueRing();
  buildSystemOrbit();
});

initRecording(handleEvent, resetVisualization);
initEventHandler({
  updateHud,
  refreshInspector: refreshInspectorIfMatch,
  updateValueHud,
  log,
  activateStage,
  pushSpark: pushSparkDensity,
  addFoldNode,
  resetFoldGraph,
  graphAddNode,
  graphAddEdge,
  graphAddValueNode,
});

initWebSocket(
  // onEvent
  (ev) => {
    if (ev._binary && ev.buffer) {
      const snapshot = decodeValueFrame(ev.buffer);
      if (!snapshot) return;
      ev = {
        component: 'Value',
        action: 'Frame',
        data: {
          stage: 'wire',
          message: snapshot.summary,
          chunkText: snapshot.tokenPreview || snapshot.tokenText || '',
          summary: snapshot.summary,
          value: snapshot,
        },
      };
      recordEvent(ev);
      if (!isReplayMode() && !state.animationPaused) handleEvent(ev);
    } else {
      recordEvent(ev);
      if (!isReplayMode() && !state.animationPaused) handleEvent(ev);
    }
    // Update slider
    slider.max = getRecordingLength() - 1;
    replayInfo.textContent = `${getRecordingLength()} events`;
    statEvents.textContent = getRecordingLength();
  },
  // onConnect
  () => {
    statConn.textContent = 'connected';
    statConn.style.color = 'rgba(140, 255, 200, 0.75)';
    ingestSend.disabled = false;
    promptSend.disabled = false;
    inputBar.classList.remove('hidden');
    log('Connected to SIX telemetry', 'state');
  },
  // onDisconnect
  () => {
    statConn.textContent = 'offline';
    statConn.style.color = 'rgba(255, 140, 120, 0.7)';
    ingestSend.disabled = true;
    promptSend.disabled = true;
    inputBar.classList.add('hidden');
    ingestPanel.classList.remove('open');
    ingestToggle.classList.remove('active');
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

let pausedAtRecIdx = 0;

btnPause.addEventListener('click', () => {
  pausePlayback();
  const wasPaused = state.animationPaused;
  state.set('animationPaused', !wasPaused);
  btnPause.classList.toggle('active', !wasPaused);

  if (!wasPaused) {
    // Pausing: remember current recording index
    pausedAtRecIdx = getRecordingLength();
  } else {
    // Unpausing: replay events that arrived while paused
    const rec = recording;
    for (let i = pausedAtRecIdx; i < rec.length; i++) {
      handleEvent(rec[i]);
    }
  }
});

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
  if (!e.target.files.length) return;
  try {
    const count = await importRecording(e.target.files[0]);
    slider.max = Math.max(0, count - 1);
    replayInfo.textContent = `${count} events`;
    statEvents.textContent = count;
    log(count > 0 ? `Loaded ${count} events` : 'Imported empty recording', 'state');
  } catch (err) {
    console.error(err);
    log(`Import failed: ${err && err.message ? err.message : err}`, 'state');
  }
});

// ── Ingest slide-out panel ──
ingestToggle.addEventListener('click', () => {
  const opening = !ingestPanel.classList.contains('open');
  ingestPanel.classList.toggle('open', opening);
  ingestToggle.classList.toggle('active', opening);
  if (opening) ingestInput.focus();
});
ingestClose.addEventListener('click', () => {
  ingestPanel.classList.remove('open');
  ingestToggle.classList.remove('active');
});
ingestClear.addEventListener('click', () => {
  ingestInput.value = '';
  ingestStatus.textContent = '';
  ingestStatus.className = 'ingest-panel-status';
  ingestInput.focus();
});

function doIngest() {
  const text = ingestInput.value;
  if (sendIngest(text)) {
    const lines = text.split('\n').filter(l => l.trim()).length;
    const chars = text.length;
    ingestInput.value = '';
    ingestStatus.textContent = `Sent ${lines} line${lines !== 1 ? 's' : ''}, ${chars} chars`;
    ingestStatus.className = 'ingest-panel-status success';
    log(`Loaded data (${chars} chars)`, 'ingest');
    setTimeout(() => {
      ingestStatus.textContent = '';
      ingestStatus.className = 'ingest-panel-status';
    }, 4000);
  } else {
    ingestStatus.textContent = 'Not connected or empty';
    ingestStatus.className = 'ingest-panel-status error';
  }
}

ingestSend.addEventListener('click', doIngest);
ingestInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    doIngest();
  }
});
ingestInput.addEventListener('input', () => {
  ingestSend.disabled = !ingestInput.value.trim();
});

// ── Prompt input ──
function doPrompt() {
  if (sendPrompt(promptInput.value)) {
    promptInput.value = '';
    log(`Sent prompt`, 'pipeline');
  }
}
promptSend.addEventListener('click', doPrompt);
promptInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    doPrompt();
  }
});

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

// Helper: map clientX/Y to normalized device coords relative to the 3D canvas
function canvasNDC(e) {
  const canvasH = innerHeight - GRAPH_STRIP_H;
  return {
    x: (e.clientX / innerWidth) * 2 - 1,
    y: -((e.clientY - GRAPH_STRIP_H) / canvasH) * 2 + 1,
  };
}

renderer.domElement.addEventListener('pointerdown', (e) => {
  mouseDownPos = { x: e.clientX, y: e.clientY };
});

renderer.domElement.addEventListener('pointerup', (e) => {
  if (!mouseDownPos) return;
  const dx = e.clientX - mouseDownPos.x;
  const dy = e.clientY - mouseDownPos.y;
  if (Math.abs(dx) > 4 || Math.abs(dy) > 4) return;

  const ndc = canvasNDC(e);
  mouseVec.x = ndc.x;
  mouseVec.y = ndc.y;
  raycaster.setFromCamera(mouseVec, camera);

  const planes = Object.values(zonePlanes);
  const hits = raycaster.intersectObjects(planes);
  if (hits.length > 0) {
    openInspector(hits[0].object.userData.sysKey);
  }
});

renderer.domElement.addEventListener('dblclick', (e) => {
  const ndc = canvasNDC(e);
  mouseVec.x = ndc.x;
  mouseVec.y = ndc.y;
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
  const ndc = canvasNDC(e);
  mouseVec.x = ndc.x;
  mouseVec.y = ndc.y;
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

  const zoneKeys = {
    '1': 'dataset',
    '2': 'machine',
    '3': 'stream',
    '4': 'controlplane',
    '5': 'emitter',
    '6': 'exec',
    '7': 'pool',
    '8': 'cuda',
    '9': 'cpu',
  };
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
      new THREE.Vector3(4, 48, 50),
      new THREE.Vector3(4, 2, 6),
    );
  }
});

// ═══════════════════════════════════════════════════════════
// ANIMATION LOOP
// ═══════════════════════════════════════════════════════════
function animate(time) {
  requestAnimationFrame(animate);

  const paused = state.animationPaused;

  if (!paused) {
    updateDataStreams();
    updateFlowParticles(time);
  }
  updateLabelZoom();
  updateFlyAnimation();
  updateZonePulses();
  if (!paused) {
    animateValueRing(time);
    animateSystemOrbit(time);
    animateStreamRing(time, false);
    animateUniConn(time, false);
  }
  updateFoldEffects();

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
