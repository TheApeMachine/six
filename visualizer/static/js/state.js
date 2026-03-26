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

// Inspector
export let inspectorOpen = false;
export let inspectorSysKey = null;
export let inspectorTab = 'feed';

// Histories
export const forestKeyBins = new Uint32Array(256);
export const tokenBinCounts = new Uint32Array(256);
export let tokenBinMax = 1;
export const promptHistory = [];
export const foldHistory = [];
export const executeHistory = [];

// Sparkline data
export const sparkData = [];
export const SPARK_MAX = 200;

// Subsystem config
export const ZONE_COMPONENTS = {
  machine: ['Machine', 'Program', 'Substrate'],
  dataset: ['Dataset', 'Substrate'],
  frame:   ['Tokenizer', 'Sequencer', 'Substrate'],
  chamber: ['Graph', 'SpatialIndex', 'Substrate'],
  kernel:  ['Kernel', 'Pool', 'Substrate'],
};

export const ZONE_DESCRIPTIONS = {
  machine: 'Orchestrates io between dataset, Value chamber, and CPU kernel',
  dataset: 'Byte source (e.g. Hugging Face) — chunked into 1024-byte frames',
  frame:   'Each frame is a primitive.Value on the wire',
  chamber: 'Holds merged Value state — registers + instruction nibble',
  kernel:  'cpu.Backend — motor (Accumulate) + truth-table ALU on operand pressure',
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
    case 'totalJobsDone': totalJobsDone += amount; return totalJobsDone;
    case 'totalJobsFailed': totalJobsFailed += amount; return totalJobsFailed;
    case 'totalJobsScheduled': totalJobsScheduled += amount; return totalJobsScheduled;
    case 'substrateFrames': substrateFrames += amount; return substrateFrames;
    case 'tokenBinMax': tokenBinMax = Math.max(tokenBinMax, amount); return tokenBinMax;
  }
}

export function set(name, value) {
  switch (name) {
    case 'totalIngested': totalIngested = value; break;
    case 'substrateFrames': substrateFrames = value; break;
    case 'inspectorOpen': inspectorOpen = value; break;
    case 'inspectorSysKey': inspectorSysKey = value; break;
    case 'inspectorTab': inspectorTab = value; break;
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
}

export function accumulateEvent(ev) {
  const key = ev.component || 'unknown';
  if (!eventsByComponent.has(key)) {
    eventsByComponent.set(key, []);
  }
  const arr = eventsByComponent.get(key);
  arr.push(ev);
  while (arr.length > MAX_COMPONENT_EVENTS) arr.shift();
  allEvents.push(ev);
  while (allEvents.length > 2000) allEvents.shift();
}
