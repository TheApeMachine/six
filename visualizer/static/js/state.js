/* ═══════════════════════════════════════════════════════════
   state.js — Shared application state
   ═══════════════════════════════════════════════════════════ */

// Counters
export let totalIngested = 0;
export let totalEdges = 0;
export let totalQueries = 0;
export let totalPaths = 0;
export let totalFolds = 0;
export let totalTokenNodes = 0;
export let totalFlowEdges = 0;
export let totalFoldLinks = 0;
export let substrateFrames = 0;
export let totalValueFrames = 0;
export let totalJobsDone = 0;
export let totalJobsFailed = 0;
export let totalJobsScheduled = 0;

// Pool state
export let poolWorkerCount = 0;
export let poolIdleWorkers = 0;
export let poolQueueSize = 0;
export let poolSuccessRate = 1.0;
export let poolAvgLatencyMs = 0;
export let poolP95LatencyMs = 0;
export let poolP99LatencyMs = 0;
export let poolFailureCount = 0;
export const poolJobHistory = [];
export const poolScaleHistory = [];
export const poolLatencyHistory = [];

// Event accumulation
export const eventsByComponent = new Map();
export const MAX_COMPONENT_EVENTS = 300;
export const allEvents = [];

// Animation pause
export let animationPaused = false;

// Inspector
export let inspectorOpen = false;
export let inspectorMode = 'zone';
export let inspectorKey = null;
export let inspectorTab = 'feed';
export let lastValueSummary = '—';

// Histories
export const forestKeyBins = new Uint32Array(256);
export const tokenBinCounts = new Uint32Array(256);
export let tokenBinMax = 1;
export const promptHistory = [];
export const foldHistory = [];
export const executeHistory = [];
export const valueSnapshots = new Map();

const VALUE_HISTORY_CAP = 240;
const _valueHistoryRing = new Array(VALUE_HISTORY_CAP);
let _valueHistoryStart = 0;
let _valueHistoryCount = 0;

function clearValueHistoryBuffer() {
  _valueHistoryStart = 0;
  _valueHistoryCount = 0;
}

/** Ring buffer of recent Value snapshots (capacity 240). */
export const valueHistory = {
  get length() {
    return _valueHistoryCount;
  },
  *[Symbol.iterator]() {
    for (let i = 0; i < _valueHistoryCount; i++) {
      yield _valueHistoryRing[(_valueHistoryStart + i) % VALUE_HISTORY_CAP];
    }
  },
};

// Sparkline data
export const sparkData = [];
export const SPARK_MAX = 200;

// Subsystem config
export const ZONE_COMPONENTS = {
  machine: ['Machine', 'Program', 'Substrate', 'UniConn'],
  stream:  ['Tokenizer', 'Sequencer', 'DMT', 'LSM'],
  emitter: ['Value'],
  backend: ['Kernel', 'Backend', 'Graph', 'SpatialIndex', 'Substrate'],
  pool:    ['Pool'],
  cuda:    ['Kernel', 'Backend'],
  metal:   ['Kernel', 'Backend'],
  cpu:     ['Kernel', 'Backend'],
};

export const ZONE_DESCRIPTIONS = {
  machine: 'Orchestrates prompts, ingest, and value circulation',
  stream:  'Frames and tokens moving through transport',
  emitter: 'Captures framed Value snapshots and preserves wire content',
  backend: 'Routes work to CPU, CUDA, and Metal substrates',
  pool:    'Schedules jobs across worker goroutines',
  cuda:    'CUDA device routes selected by the backend',
  metal:   'Metal device routes selected by the backend',
  cpu:     'CPU fallback routes selected by the backend',
};

// Mutator helpers (since ES modules export bindings by reference for let)
export function inc(name, amount = 1) {
  switch (name) {
    case 'totalIngested': totalIngested += amount; return totalIngested;
    case 'totalEdges': totalEdges += amount; return totalEdges;
    case 'totalQueries': totalQueries += amount; return totalQueries;
    case 'totalPaths': totalPaths += amount; return totalPaths;
    case 'totalFolds': totalFolds += amount; return totalFolds;
    case 'totalTokenNodes': totalTokenNodes += amount; return totalTokenNodes;
    case 'totalFlowEdges': totalFlowEdges += amount; return totalFlowEdges;
    case 'totalFoldLinks': totalFoldLinks += amount; return totalFoldLinks;
    case 'totalValueFrames': totalValueFrames += amount; return totalValueFrames;
    case 'totalJobsDone': totalJobsDone += amount; return totalJobsDone;
    case 'totalJobsFailed': totalJobsFailed += amount; return totalJobsFailed;
    case 'totalJobsScheduled': totalJobsScheduled += amount; return totalJobsScheduled;
    case 'substrateFrames': substrateFrames += amount; return substrateFrames;
    case 'tokenBinMax': tokenBinMax = Math.max(tokenBinMax, amount); return tokenBinMax;
    default:
      throw new Error(`Unknown counter name: ${name}`);
  }
}

export function set(name, value) {
  switch (name) {
    case 'totalIngested': totalIngested = value; break;
    case 'substrateFrames': substrateFrames = value; break;
    case 'totalValueFrames': totalValueFrames = value; break;
    case 'animationPaused': animationPaused = value; break;
    case 'inspectorOpen': inspectorOpen = value; break;
    case 'inspectorMode': inspectorMode = value; break;
    case 'inspectorKey': inspectorKey = value; break;
    case 'inspectorTab': inspectorTab = value; break;
    case 'lastValueSummary': lastValueSummary = value; break;
    case 'poolWorkerCount': poolWorkerCount = value; break;
    case 'poolIdleWorkers': poolIdleWorkers = value; break;
    case 'poolQueueSize': poolQueueSize = value; break;
    case 'poolSuccessRate': poolSuccessRate = value; break;
    case 'poolAvgLatencyMs': poolAvgLatencyMs = value; break;
    case 'poolP95LatencyMs': poolP95LatencyMs = value; break;
    case 'poolP99LatencyMs': poolP99LatencyMs = value; break;
    case 'poolFailureCount': poolFailureCount = value; break;
    case 'tokenBinMax': tokenBinMax = value; break;
    case 'totalFoldLinks': totalFoldLinks = value; break;
    case 'totalFolds': totalFolds = value; break;
    default:
      console.warn('[state.set] unrecognized key:', name, value);
  }
}

export function resetCounters() {
  totalIngested = 0;
  totalEdges = 0;
  totalQueries = 0;
  totalPaths = 0;
  totalFolds = 0;
  totalTokenNodes = 0;
  totalFlowEdges = 0;
  totalFoldLinks = 0;
  substrateFrames = 0;
  totalValueFrames = 0;
  totalJobsDone = 0;
  totalJobsFailed = 0;
  totalJobsScheduled = 0;
  poolWorkerCount = 0;
  poolIdleWorkers = 0;
  poolQueueSize = 0;
  poolSuccessRate = 1.0;
  poolAvgLatencyMs = 0;
  poolP95LatencyMs = 0;
  poolP99LatencyMs = 0;
  poolFailureCount = 0;
  poolJobHistory.length = 0;
  poolScaleHistory.length = 0;
  poolLatencyHistory.length = 0;
  tokenBinMax = 1;
  sparkData.length = 0;
  forestKeyBins.fill(0);
  tokenBinCounts.fill(0);
  eventsByComponent.clear();
  promptHistory.length = 0;
  foldHistory.length = 0;
  executeHistory.length = 0;
  clearValueHistoryBuffer();
  valueSnapshots.clear();
  allEvents.length = 0;
  inspectorMode = 'zone';
  inspectorKey = null;
  lastValueSummary = '—';
}

const MAX_ALL_EVENTS = 2000;

export function accumulateEvent(ev) {
  const key = ev.component || 'unknown';
  if (!eventsByComponent.has(key)) {
    eventsByComponent.set(key, []);
  }
  const arr = eventsByComponent.get(key);
  arr.push(ev);
  const overComp = arr.length - MAX_COMPONENT_EVENTS;
  if (overComp > 0) arr.splice(0, overComp);
  allEvents.push(ev);
  const overAll = allEvents.length - MAX_ALL_EVENTS;
  if (overAll > 0) allEvents.splice(0, overAll);
}

export function rememberValueSnapshot(snapshot) {
  if (!snapshot || !snapshot.valueId) return;
  valueSnapshots.set(snapshot.valueId, snapshot);
  const idx = (_valueHistoryStart + _valueHistoryCount) % VALUE_HISTORY_CAP;
  if (_valueHistoryCount < VALUE_HISTORY_CAP) {
    _valueHistoryRing[idx] = snapshot;
    _valueHistoryCount++;
  } else {
    _valueHistoryRing[idx] = snapshot;
    _valueHistoryStart = (_valueHistoryStart + 1) % VALUE_HISTORY_CAP;
  }
}
