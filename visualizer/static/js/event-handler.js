/* ═══════════════════════════════════════════════════════════
   event-handler.js — Routes WebSocket events to visualization
   ═══════════════════════════════════════════════════════════ */
import * as state from './state.js';
import { spawnDataStream, activateFlowParticles } from './particles.js';
import {
  addZoneLabel, clearZoneLabels, pulseZone,
  advanceStreamPtr, spawnFoldEffect,
  setUniConnTransport, setUniConnState, spawnUniConnFrame, addUniConnPeer,
} from './architecture.js';
import { updateValueDisplay, updateValueFromWireFrame } from './value-viz.js';
import { pulseSystemNode, pulseSystemBackendSelection } from './system-viz.js';

let hudUpdateFn = null;
let inspectorRefreshFn = null;
let logFn = null;
let activateStageFn = null;
let pushSparkFn = null;
let addFoldNodeFn = null;
let resetFoldGraphFn = null;
let graphAddNodeFn = null;
let graphAddEdgeFn = null;
let graphAddValueNodeFn = null;
let updateValueHudFn = null;

// NOP-heavy UB slot telemetry would drown the log; still log every slot change and periodic samples.
let ubLogLastTs = 0;
let ubLogLastSlot = -2;

function universalBitwiseTelemetryLooksLikeNop(message, instruction) {
  const msg = String(message || '');
  const ins = String(instruction || '');

  return msg.includes('NOP')
    || msg.includes('kernel skips')
    || ins === ''
    || ins === '0'
    || /^nop\b/i.test(ins);
}

function shouldLogUniversalBitwiseSlot(ev, slot, instruction, message) {
  if (!universalBitwiseTelemetryLooksLikeNop(message, instruction)) {
    return true;
  }

  const ts = Number(ev._ts);
  const t = Number.isFinite(ts) ? ts : Date.now();

  if (slot !== ubLogLastSlot) {
    ubLogLastSlot = slot;
    ubLogLastTs = t;

    return true;
  }

  if (t - ubLogLastTs >= 480) {
    ubLogLastTs = t;

    return true;
  }

  return false;
}

export function initEventHandler(opts) {
  hudUpdateFn = opts.updateHud;
  inspectorRefreshFn = opts.refreshInspector;
  logFn = opts.log;
  activateStageFn = opts.activateStage;
  pushSparkFn = opts.pushSpark;
  addFoldNodeFn = opts.addFoldNode;
  resetFoldGraphFn = opts.resetFoldGraph;
  graphAddNodeFn = opts.graphAddNode;
  graphAddEdgeFn = opts.graphAddEdge;
  graphAddValueNodeFn = opts.graphAddValueNode;
  updateValueHudFn = opts.updateValueHud;
}

function log(text, type, ev) {
  if (logFn) logFn(text, type, ev);
}

function activateStage(name, info) {
  if (activateStageFn) activateStageFn(name, info);
}

function refreshInspector(sysKey) {
  if (inspectorRefreshFn) inspectorRefreshFn(sysKey);
}

function pushSpark(density, action) {
  if (pushSparkFn) pushSparkFn(density, action);
}

/**
 * Shared visualization path: stream, particles, zone label, pulse(s), log, inspector.
 */
function visualizeFlow(opts) {
  const {
    from, to, streamText, streamClass, duration,
    labelZone, labelText,
    pulseZones,
    logText, logType, ev,
    inspectorKey,
    skipFlowParticles = false,
    skipDataStream = false,
    skipRefreshInspector = false,
    skipLog = false,
  } = opts;

  if (!skipDataStream) {
    spawnDataStream(from, to, streamText, streamClass, duration);
  }
  if (!skipFlowParticles) {
    activateFlowParticles(from, to);
  }
  if (labelZone != null && labelText != null) {
    addZoneLabel(labelZone, labelText);
  }
  const zones = pulseZones === undefined || pulseZones === null
    ? []
    : (Array.isArray(pulseZones) ? pulseZones : [pulseZones]);
  for (const z of zones) {
    if (z) pulseZone(z);
  }
  if (!skipLog && logText != null) {
    log(logText, logType, ev);
  }
  if (!skipRefreshInspector && inspectorKey) {
    refreshInspector(inspectorKey);
  }
}

export function handleEvent(ev) {
  const d = ev.data || {};
  state.accumulateEvent(ev);

  // ── Tokenizer Value ──
  if (ev.component === 'Tokenizer' && ev.action === 'Value') {
    const stg = d.stage || '';
    const edgeCount = d.edgeCount || 0;
    const text = d.chunkText || '';

    if (stg === 'ingest-tokenize') {
      state.inc('totalIngested');
      advanceStreamPtr(); // Stream write advances ptr
      spawnUniConnFrame(null, 'in'); // Data arrived via network transport
      activateStage('tokenize', `${edgeCount} edges`);
      // Real flow: Dataset → Machine.Write → Tokenizer.Write → NewValue
      visualizeFlow({
        from: 'dataset',
        to: 'machine',
        streamText: text || 'raw bytes',
        streamClass: 'stream-ingest',
        duration: 800,
        labelZone: 'machine',
        labelText: 'raw bytes → Write',
        pulseZones: ['dataset', 'machine'],
        logText: `Ingest: "${text.slice(0, 50)}"`,
        logType: 'ingest',
        ev,
        skipRefreshInspector: true,
      });
      // Machine.Write → Tokenizer.Write (raw → NewValue)
      setTimeout(() => {
        spawnDataStream('machine', 'stream', text || 'NewValue', 'stream-ingest', 900);
        addZoneLabel('stream', text || 'NewValue');
        pulseZone('stream');
      }, 400);
    }

    if (stg === 'tokenize') {
      state.inc('totalQueries');
      activateStage('tokenize', `${edgeCount} edges`);
      visualizeFlow({
        from: 'frame',
        to: 'machine',
        streamText: text || 'tokens',
        streamClass: 'stream-token',
        duration: 1000,
        labelZone: 'frame',
        labelText: text,
        pulseZones: ['frame'],
        logText: `Query: "${text.slice(0, 50)}"`,
        logType: 'tokenize',
        ev,
        skipRefreshInspector: true,
      });
    }

    if (d.bin !== undefined) {
      state.inc('totalTokenNodes');
      state.tokenBinCounts[d.bin & 0xFF]++;
      state.inc('tokenBinMax', state.tokenBinCounts[d.bin & 0xFF]);
    }

    refreshInspector('frame');
  }

  // ── LSM Insert (SpatialIndex insert + Kademlia k-bucket routing) ──
  else if (ev.component === 'LSM' && ev.action === 'Insert') {
    const stg = d.stage || '';
    const edges = d.edges || d.edgeCount || 0;
    const entryCount = d.entryCount || 0;

    if (stg === 'kademlia-route') {
      // Kademlia routing: Value placed in k-bucket → per-bucket LSM
      if (d.bin !== undefined) state.forestKeyBins[d.bin & 0xFF]++;
      addZoneLabel('controlplane', `bucket #${d.bin} (${entryCount} frames)`);
      addZoneLabel('lsm', `bucket #${d.bin}`);
      pulseZone('controlplane');
      pulseZone('lsm');
      log(`Kademlia route: bucket=${d.bin} key=${d.nodeId}`, 'ingest', ev);
      refreshInspector('controlplane');
    } else {
      // SpatialIndex insert: token→posting in per-bucket LSM
      const insertText = d.chunkText || `${edges} tokens → ${entryCount} frames`;
      state.inc('totalEdges', edges);
      activateStage('insert', `${edges} stored`);
      visualizeFlow({
        from: 'controlplane',
        to: 'lsm',
        streamText: insertText,
        streamClass: 'stream-store',
        duration: 1400,
        labelZone: 'lsm',
        labelText: insertText,
        pulseZones: ['controlplane', 'lsm'],
        logText: `LSM insert: ${insertText}`,
        logType: 'ingest',
        ev,
        inspectorKey: 'controlplane',
      });
      state.inc('totalFlowEdges');
    }
  }

  // ── SpatialIndex Lookup (per-bucket index in control plane) ──
  else if (ev.component === 'SpatialIndex' && ev.action === 'Lookup') {
    const pathCount = d.pathCount || d.paths || 0;
    state.inc('totalPaths', pathCount);
    activateStage('lookup', `${pathCount} paths`);
    visualizeFlow({
      from: 'controlplane',
      to: 'exec',
      streamText: `${pathCount} paths found`,
      streamClass: 'stream-lookup',
      duration: 1000,
      labelZone: 'controlplane',
      labelText: `${pathCount} paths`,
      pulseZones: ['controlplane', 'exec'],
      logText: `Lookup: ${pathCount} paths`,
      logType: 'lookup',
      ev,
      inspectorKey: 'controlplane',
      skipFlowParticles: true,
    });
  }

  // ── Graph Evaluate ──
  else if (ev.component === 'Graph' && ev.action === 'Evaluate') {
    const pathCount = d.pathCount || 0;
    activateStage('fold', `${pathCount} results`);
    spawnDataStream('exec', 'pool', `evaluate ${pathCount} paths`, 'stream-fold', 1200);
    activateFlowParticles('exec', 'pool');
    log(`Fold complete: ${pathCount} result paths`, 'fold', ev);
    refreshInspector('exec');
  }

  // ── Graph Fold ──
  else if (ev.component === 'Graph' && ev.action === 'Fold') {
    state.inc('totalFolds');
    state.inc('totalFoldLinks');
    const foldText = d.chunkText || '';
    if (addFoldNodeFn) addFoldNodeFn(d.bin || 0, d.level || 0, d.density || 0, foldText, d.childCount || 0);
    // Show fold: incoming Value written into receiver's ALU (UniversalBitwise)
    spawnFoldEffect(foldText, d.partnerText || '', d.firmware || '');

    state.foldHistory.push({
      level: d.level || 0, bin: d.bin || 0, density: d.density || 0,
      text: foldText, children: d.childCount || 0, ts: ev._ts,
    });
    while (state.foldHistory.length > 200) state.foldHistory.shift();

    visualizeFlow({
      from: 'exec',
      to: 'pool',
      streamText: foldText || `L${d.level} fold`,
      streamClass: 'stream-fold',
      duration: 1400,
      labelZone: 'exec',
      labelText: foldText || `L${d.level} fold`,
      pulseZones: ['exec'],
      logText: `Fold L${d.level} bin=${d.bin} children=${d.childCount} "${foldText.slice(0, 40)}"`,
      logType: 'fold',
      ev,
      inspectorKey: 'exec',
    });
  }

  // ── Machine Pipeline ──
  else if (ev.component === 'Machine' && ev.action === 'Pipeline') {
    handleMachineEvent(d, ev);
  }

  // ── Value Frame ──
  else if (ev.component === 'Value' && ev.action === 'Frame') {
    handleValueEvent(d, ev);
  }

  // ── Sequencer Boundary ──
  else if (ev.component === 'Sequencer' && ev.action === 'Boundary') {
    const densityPct = ((d.density || 0) * 100).toFixed(0);
    spawnDataStream('frame', 'machine', `chunk #${d.chunks} ${densityPct}%`, 'stream-token', 800);
    pulseZone('frame');
    log(`Boundary #${d.chunks} density=${((d.density || 0) * 100).toFixed(1)}%`, 'tokenize');
  }

  // ── Program Execute ──
  else if (ev.component === 'Program' && ev.action === 'Execute') {
    handleProgramExecute(d, ev);
  }

  // ── Graph FoldSpan ──
  else if (ev.component === 'Graph' && ev.action === 'FoldSpan') {
    const level = d.level || 0;
    const spanText = d.chunkText || '';
    const spanLabel = spanText
      ? `"${spanText.slice(0, 20)}" L${level} [${d.left}:${d.right}]`
      : `L${level} [${d.left}:${d.right}] ×${d.spanSize || 0}`;
    visualizeFlow({
      from: 'exec',
      to: 'pool',
      streamText: spanText || 'span',
      streamClass: 'stream-fold',
      duration: 1000,
      labelZone: 'exec',
      labelText: spanLabel,
      pulseZones: [],
      logText: `FoldSpan L${level}: [${d.left}:${d.right}] "${spanText.slice(0, 40)}"`,
      logType: 'fold',
      ev,
      skipFlowParticles: true,
      skipDataStream: level !== 0,
      skipRefreshInspector: true,
    });
  }

  // ── Pool ──
  else if (ev.component === 'Pool') {
    handlePoolEvent(ev, d);
  }

  // ── Kernel ──
  else if (ev.component === 'Kernel') {
    handleKernelEvent(ev, d);
  }

  // ── Chaos ──
  else if (ev.component === 'Chaos') {
    const density = d.density || 0;
    pushSpark(density, ev.action || '');
    log(`${ev.action}: [${(d.activeBits || []).length} bits, ${(density * 100).toFixed(1)}%]`, 'fold');
  }

  // ── UniConn ──
  else if (ev.component === 'UniConn') {
    handleUniConnEvent(ev, d);
  }

  // ── Substrate ──
  else if (ev.component === 'Substrate') {
    handleSubstrateEvent(ev, d);
  }

  // ── Backend (graph nodes/edges, Queue, UniversalBitwise telemetry) ──
  else if (ev.component === 'Backend') {
    if (ev.action === 'Queue') {
      const qs = d.queueSize || 0;
      activateStage('insert', `queue=${qs}`);
      spawnDataStream('machine', 'emitter', `queue ${qs}`, 'stream-token', 800);
      activateFlowParticles('machine', 'emitter');
      addZoneLabel('emitter', `queued (${qs})`);
      pulseZone('emitter');
      log(`Backend queue: size=${qs}`, 'pipeline', ev);
      refreshInspector('emitter');
    } else if (ev.action === 'UniversalBitwise') {
      handleUniversalBitwiseTelemetry(ev, d);
    } else {
      pulseSystemNode('exec', d.nodeType || ev.action || 'backend');
      const nodeType = String(d.nodeType || '').toLowerCase();
      if (nodeType === 'cuda' || nodeType === 'metal' || nodeType === 'cpu') {
        pulseSystemNode(nodeType, d.nodeTokens || d.nodeId || ev.action || nodeType);
      }
      if (ev.action === 'AddNode' && graphAddNodeFn) {
        graphAddNodeFn(d.nodeId, d.nodeTokens || '', d.nodeType || 'raw');
        log(`Node #${d.nodeId} [${d.nodeTokens}] (${d.nodeType})`, 'fold', ev);
      }
      if (ev.action === 'AddEdge' && graphAddEdgeFn) {
        graphAddEdgeFn(d.fromId, d.toId);
        log(`Edge ${d.fromId} → ${d.toId}`, 'fold', ev);
      }
    }
  }

  // Update HUD after processing
  if (hudUpdateFn) hudUpdateFn();
}

function handleValueEvent(d, ev) {
  const snapshot = d.value || d.snapshot || d;
  if (!snapshot) return;

  if (snapshot._ts == null) {
    snapshot._ts = ev._ts || Date.now();
  }

  // Map Go telemetry field names to visualizer snapshot names
  if (!snapshot.valueId && snapshot.nodeId) snapshot.valueId = snapshot.nodeId;
  if (!snapshot.prevId && snapshot.fromId) snapshot.prevId = snapshot.fromId;
  if (!snapshot.nextId && snapshot.toId) snapshot.nextId = snapshot.toId;
  if (!snapshot.tokenPreview && snapshot.chunkText) snapshot.tokenPreview = snapshot.chunkText;

  state.inc('totalValueFrames');
  state.rememberValueSnapshot(snapshot);
  state.set('lastValueSummary', snapshot.summary || `Value ${snapshot.valueId || '?'}`);

  if (updateValueHudFn) updateValueHudFn(snapshot);

  if (snapshot.wire) {
    updateValueFromWireFrame(snapshot.wire);
  }
  pulseSystemNode('emitter', snapshot.summary || snapshot.tokenPreview || snapshot.tokenText || 'value');
  pulseSystemNode('machine', snapshot.summary || snapshot.tokenPreview || snapshot.tokenText || 'value');

  if (snapshot.tokenPreview || snapshot.tokenText) {
    addZoneLabel('stream', snapshot.tokenPreview || snapshot.tokenText);
    pulseZone('stream');
  }
  if (snapshot.summary) {
    addZoneLabel('emitter', snapshot.summary);
    pulseZone('emitter');
  }

  spawnDataStream('stream', 'machine', snapshot.tokenPreview || snapshot.tokenText || 'value', 'stream-token', 900);
  activateFlowParticles('stream', 'machine');
  spawnDataStream('machine', 'emitter', snapshot.summary || snapshot.tokenPreview || snapshot.tokenText || 'value', 'stream-token', 880);
  activateFlowParticles('machine', 'emitter');
  spawnDataStream('emitter', 'exec', snapshot.summary || snapshot.tokenPreview || 'value', 'stream-fold', 1100);
  activateFlowParticles('emitter', 'exec');

  // Continue the pipeline: exec → pool → machine (full loop)
  const shortLabel = (snapshot.tokenPreview || snapshot.tokenText || 'value').slice(0, 20);
  setTimeout(() => {
    addZoneLabel('exec', shortLabel);
    pulseZone('backend');
    pulseZone('exec');

    // Backend dispatches to a compute substrate (CPU by default)
    spawnDataStream('exec', 'cpu', 'compute', 'stream-compute', 700);
    activateFlowParticles('exec', 'cpu');
    pulseZone('cpu');
  }, 500);
  setTimeout(() => {
    spawnDataStream('exec', 'pool', 'result', 'stream-compute', 900);
    activateFlowParticles('exec', 'pool');
    addZoneLabel('pool', 'result');
    pulseZone('pool');
  }, 1000);
  setTimeout(() => {
    spawnDataStream('pool', 'machine', 'emitted', 'stream-result', 800);
    activateFlowParticles('pool', 'machine');
    pulseZone('machine');
  }, 1600);

  if (graphAddValueNodeFn) {
    graphAddValueNodeFn(snapshot);
  }

  if (snapshot.summary) {
    log(`Value: ${snapshot.summary}`, 'pipeline', ev);
  }

  refreshInspector(snapshot.valueId);
  refreshInspector('machine');
  refreshInspector('stream');
  refreshInspector('emitter');
  refreshInspector('exec');
}

function handleMachineEvent(d, ev) {
  const stg = d.stage || '';

  if (stg === 'prompt-start') {
    if (resetFoldGraphFn) resetFoldGraphFn();
    clearZoneLabels('emitter');
    const promptText = d.message || d.msg || '';
    pulseSystemNode('machine', promptText || 'prompt');

    spawnDataStream('machine', 'stream', promptText, 'stream-prompt', 800);
    activateFlowParticles('machine', 'stream');
    addZoneLabel('stream', promptText);
    pulseZone('stream');
    setTimeout(() => {
      spawnDataStream('stream', 'machine', promptText, 'stream-prompt', 800);
      activateFlowParticles('stream', 'machine');
      addZoneLabel('machine', promptText.slice(0, 40));
      pulseZone('machine');
    }, 400);
    setTimeout(() => {
      spawnDataStream('machine', 'emitter', promptText, 'stream-prompt', 800);
      activateFlowParticles('machine', 'emitter');
      addZoneLabel('emitter', promptText);
      pulseZone('emitter');
    }, 800);
    setTimeout(() => {
      spawnDataStream('emitter', 'exec', promptText, 'stream-prompt', 800);
      activateFlowParticles('emitter', 'exec');
      addZoneLabel('exec', promptText);
      pulseZone('backend');
      pulseZone('exec');
    }, 1200);

    addZoneLabel('machine', promptText);
    pulseZone('machine');

    state.promptHistory.push({ prompt: promptText, result: null, error: null, ts: ev._ts, stage: 'pending' });
    while (state.promptHistory.length > 50) state.promptHistory.shift();
    log(`Prompt: "${promptText.slice(0, 50)}"`, 'pipeline', ev);
  }

  if (stg === 'prompt-complete') {
    activateStage('decode', '✓');
    const resultContent = (d.resultText || '').slice(0, 200);
    pulseSystemNode('machine', resultContent || 'complete');
    spawnDataStream('exec', 'pool', resultContent || 'result', 'stream-result', 1000);
    activateFlowParticles('exec', 'pool');
    spawnDataStream('pool', 'machine', resultContent || 'result', 'stream-result', 1000);
    activateFlowParticles('pool', 'machine');
    addZoneLabel('machine', resultContent.slice(0, 30));
    addZoneLabel('pool', resultContent.slice(0, 30));
    pulseZone('machine');
    pulseZone('pool');

    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.result = resultContent; lastPrompt.stage = 'complete'; }
    log(`Result: "${resultContent.slice(0, 60)}"`, 'result', ev);
  }

  if (stg === 'prompt-empty') {
    activateStage('decode', '∅');
    pulseSystemNode('machine', 'empty');
    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.result = '∅ empty'; lastPrompt.stage = 'empty'; }
    log(`Empty result`, 'state', ev);
  }

  if (stg === 'prompt-error') {
    pulseSystemNode('machine', d.message || 'error');
    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.error = d.message; lastPrompt.stage = 'error'; }
    log(`Prompt error: ${d.message}`, 'state', ev);
  }

  refreshInspector('machine');
}

function handleProgramExecute(d, ev) {
  const stg = d.stage || '';

  if (stg === 'start') {
    spawnDataStream('machine', 'stream', `${d.candidateCount} candidates`, 'stream-prompt', 1000);
    activateFlowParticles('machine', 'stream');
    addZoneLabel('stream', `${d.candidateCount} candidates`);
    pulseZone('stream');
    addZoneLabel('machine', `Execute: ${d.candidateCount} candidates`);
    pulseZone('machine');

    state.executeHistory.push({
      stage: 'start', candidates: d.candidateCount,
      preResidue: d.preResidue, steps: [], ts: ev._ts,
    });
    while (state.executeHistory.length > 30) state.executeHistory.shift();
    log(`Execute start: residue=${d.preResidue} candidates=${d.candidateCount}`, 'pipeline', ev);
  }

  if (stg === 'step') {
    const step = d.step || 0;
    const pre = d.preResidue || 0;
    const post = d.postResidue || 0;
    const adv = d.advanced;

    const stepLabel = `step ${step}: ${pre}→${post}`;
    spawnDataStream('stream', 'machine', stepLabel, adv ? 'stream-result' : 'stream-fold', 700);
    activateFlowParticles('stream', 'machine');
    spawnDataStream('machine', 'emitter', stepLabel, adv ? 'stream-result' : 'stream-fold', 680);
    activateFlowParticles('machine', 'emitter');
    addZoneLabel('emitter', stepLabel);
    pulseZone('emitter');

    const lastExec = state.executeHistory[state.executeHistory.length - 1];
    if (lastExec) lastExec.steps.push({ step, pre, post, advanced: adv, stable: d.stable });
    log(`Execute step ${step}: ${pre}→${post}`, 'pipeline', ev);
  }

  if (stg === 'complete') {
    const outcome = d.outcome || '';
    if (outcome === 'stable') {
      spawnDataStream('pool', 'machine', `stable (step ${d.step})`, 'stream-result', 1000);
      activateFlowParticles('pool', 'machine');
      addZoneLabel('pool', `stable (step ${d.step})`);
      pulseZone('pool');
    }

    const lastExec = state.executeHistory[state.executeHistory.length - 1];
    if (lastExec) { lastExec.outcome = outcome; lastExec.finalStep = d.step; lastExec.postResidue = d.postResidue; }
    addZoneLabel('machine', `Execute: ${outcome} (step ${d.step})`);
    pulseZone('machine');
    log(`Execute ${outcome}: step=${d.step} residue=${d.postResidue}`, 'pipeline', ev);
  }

  refreshInspector('machine');
}

function handlePoolEvent(ev, d) {
  if (ev.action === 'Schedule') {
    state.inc('totalJobsScheduled');
    pulseSystemNode('pool', d.jobId || d.taskType || 'schedule');
    spawnDataStream('exec', 'pool', d.jobId || 'job', 'stream-compute', 800);
    activateFlowParticles('exec', 'pool');
    addZoneLabel('pool', `schedule: ${d.jobId || '?'}`);
    pulseZone('pool');
    log(`Schedule: ${d.jobId} type=${d.taskType} queue=${d.queueSize}`, 'pool', ev);
  }

  if (ev.action === 'Dispatch') {
    pulseSystemNode('pool', d.taskType || d.jobId || 'dispatch');
    spawnDataStream('exec', 'pool', d.taskType || 'task', 'stream-compute', 600);
    addZoneLabel('pool', `dispatch: ${d.taskType || '?'}`);
    log(`Dispatch: ${d.jobId} type=${d.taskType}`, 'pool', ev);
  }

  if (ev.action === 'JobDone') {
    state.inc('totalJobsDone');
    pulseSystemNode('pool', `done ${d.durationMs}ms`);
    spawnDataStream('pool', 'machine', `done ${d.durationMs}ms`, 'stream-result', 800);
    activateFlowParticles('pool', 'machine');
    addZoneLabel('pool', `done: ${d.durationMs}ms`);
    pulseZone('pool');

    state.poolJobHistory.push({ id: d.jobId, type: d.taskType, duration: d.durationMs, success: true, ts: ev._ts });
    while (state.poolJobHistory.length > 100) state.poolJobHistory.shift();
    state.poolLatencyHistory.push(d.durationMs || 0);
    while (state.poolLatencyHistory.length > 200) state.poolLatencyHistory.shift();
    log(`JobDone: ${d.jobId} ${d.durationMs}ms`, 'pool', ev);
  }

  if (ev.action === 'JobFail') {
    state.inc('totalJobsFailed');
    pulseSystemNode('pool', d.message || 'failed');
    addZoneLabel('pool', `FAIL: ${(d.message || '').slice(0, 24)}`);
    state.poolJobHistory.push({ id: d.jobId, type: d.taskType, duration: d.durationMs, success: false, error: d.message, ts: ev._ts });
    while (state.poolJobHistory.length > 100) state.poolJobHistory.shift();
    log(`JobFail: ${d.jobId} "${(d.message || '').slice(0, 40)}"`, 'pool', ev);
  }

  if (ev.action === 'Drop') {
    pulseSystemNode('pool', d.message || 'dropped');
    addZoneLabel('pool', `DROP: ${d.jobId || '?'}`);
    log(`Drop: ${d.jobId} — ${d.message}`, 'pool', ev);
  }

  if (ev.action === 'Metrics') {
    state.set('poolWorkerCount', d.workerCount || 0);
    state.set('poolIdleWorkers', d.idleWorkers || 0);
    state.set('poolQueueSize', d.queueSize || 0);
    state.set('poolSuccessRate', d.successRate || 0);
    state.set('poolAvgLatencyMs', d.avgLatencyMs || 0);
    state.set('poolP95LatencyMs', d.p95LatencyMs || 0);
    state.set('poolP99LatencyMs', d.p99LatencyMs || 0);
    state.set('poolFailureCount', d.failureCount || 0);
  }

  if (ev.action === 'Scale') {
    const dir = d.scaleDirection || '?';
    const cnt = d.scaleCount || 0;
    const total = d.workerCount || 0;
    pulseSystemNode('pool', `scale ${dir} ${cnt}`);
    addZoneLabel('pool', `scale ${dir}: ${cnt} → ${total}`);
    state.poolScaleHistory.push({ direction: dir, count: cnt, total, ts: ev._ts });
    while (state.poolScaleHistory.length > 50) state.poolScaleHistory.shift();
    log(`Scale ${dir}: ${cnt} workers (total=${total})`, 'pool', ev);
  }

  refreshInspector('pool');
}

function handleUniversalBitwiseTelemetry(ev, d) {
  if (d.stage !== 'slot') {
    return;
  }

  const slot = Number(d.lgpSlot);
  const total = Number(d.lgpSlotsTotal);
  const slotKey = Number.isFinite(slot) ? slot : -1;

  if (Number.isFinite(slot)) {
    state.set('lastUbSlot', slot);
  }

  if (Number.isFinite(total) && total > 0) {
    state.set('lastUbSlotsTotal', total);
  }

  const slotShown = Number.isFinite(slot) ? `${slot + 1}` : '?';
  const label = Number.isFinite(total) && total > 0
    ? `UB ${slotShown}/${total} ${d.instruction || ''}`.trim()
    : (d.message || 'UB').slice(0, 40);

  const detail = String(d.message || '').slice(0, 400);

  state.set('activityUbShort', label);
  state.set('activityUbDetail', detail);

  activateStage('fold', label);
  pulseSystemNode('exec', d.instruction || label);
  addZoneLabel('exec', label);
  pulseZone('backend');
  pulseZone('exec');
  spawnDataStream('exec', 'cpu', label, 'stream-compute', 420);
  activateFlowParticles('exec', 'cpu');

  if (detail && shouldLogUniversalBitwiseSlot(ev, slotKey, d.instruction, d.message)) {
    log(detail, 'backend', ev);
  }

  refreshInspector('exec');
}

function handleKernelEvent(ev, d) {
  if (ev.action === 'Route') {
    const selectedHardware = pulseSystemBackendSelection(d.bestIndex, `route #${d.bestIndex}`);
    d.selectedHardware = selectedHardware;
    addZoneLabel('exec', `backend #${d.bestIndex} avail=${d.avail}`);
    spawnDataStream('exec', 'pool', `backend #${d.bestIndex}`, 'stream-kernel', 800);
    activateFlowParticles('exec', 'pool');
    pulseZone('backend');
    pulseZone('exec');
    log(`Route: backend #${d.bestIndex} avail=${d.avail}`, 'backend', ev);
  }

  if (ev.action === 'PeerAdd') {
    pulseSystemNode('exec', d.nodeAddr || 'peer');
    addZoneLabel('exec', `peer: ${d.nodeAddr}`);
    // Wire into UniConn viz — peer nodes appear on network layer
    const addr = d.nodeAddr || '';
    if (addr.includes(':')) {
      // Network address → QUIC or UDP peer
      const transport = addr.startsWith('/') ? 'ipc' : (addr.includes('239.') ? 'udp' : 'quic');
      addUniConnPeer(addr, transport);
      setUniConnState(transport, 'connected');
      spawnUniConnFrame(transport, 'in');
    }
    log(`PeerAdd: ${d.nodeAddr} (${d.nodeCount} nodes)`, 'backend', ev);
  }

  if (ev.action === 'WriteError') {
    pulseSystemNode('exec', d.message || 'write error');
    addZoneLabel('exec', `ERR: ${(d.message || '').slice(0, 20)}`);
    log(`Kernel error: ${d.message}`, 'backend', ev);
  }

  refreshInspector('exec');
}

function handleSubstrateEvent(ev, d) {
  if (ev.action === 'Run') {
    if (d.stage === 'start') {
      if (resetFoldGraphFn) resetFoldGraphFn();
      clearZoneLabels('emitter');
      clearZoneLabels('machine');
      pulseSystemNode('machine', d.message || 'substrate start');
      log(`Substrate: ${d.message}`, 'substrate', ev);
      refreshInspector('machine');
    }

    if (d.stage === 'batch-start') {
      const frames = d.ubFrameCount || 0;
      activateStage('insert', `batch ${frames} frames`);
      spawnDataStream('emitter', 'exec', `batch ${frames}`, 'stream-compute', 900);
      activateFlowParticles('emitter', 'exec');
      addZoneLabel('exec', `batch ${frames} frames`);
      pulseZone('backend');
      pulseZone('exec');
      log(`Batch start: ${frames} frames`, 'substrate', ev);
      refreshInspector('exec');
    }

    if (d.stage === 'batch-complete') {
      const frames = d.ubFrameCount || 0;
      spawnDataStream('exec', 'pool', `done ${frames}`, 'stream-compute', 800);
      activateFlowParticles('exec', 'pool');
      addZoneLabel('pool', `batch done ${frames}`);
      pulseZone('pool');
      log(`Batch complete: ${frames} frames`, 'substrate', ev);
      refreshInspector('pool');
    }

    if (d.stage === 'complete') {
      activateStage('decode', 'ok');
      pulseSystemNode('machine', d.message || 'substrate complete');
      log(`Substrate: ${d.message}`, 'substrate', ev);
      refreshInspector('machine');
    }

    if (d.stage === 'dataset-error') {
      log(`Dataset error: ${d.message}`, 'state', ev);
    }
  }

  if (ev.action === 'Step') {
    const st = d.stage || '';

    if (st === 'frame') {
      state.inc('substrateFrames');
      state.set('totalIngested', state.substrateFrames);
      // Each frame write advances the stream's rotation pointer
      advanceStreamPtr();
      pulseSystemNode('stream', d.chunkText || `frame ${d.frameIndex}`);
      activateStage('tokenize', `row ${d.frameIndex}`);
      spawnDataStream('machine', 'stream', (d.chunkText || 'frame').slice(0, 28), 'stream-ingest', 1200);
      activateFlowParticles('machine', 'stream');
      addZoneLabel('stream', `frame ${d.frameIndex}`);
      pulseZone('stream');
      log(`Frame ${d.frameIndex}: ${d.message}`, 'substrate', ev);
      refreshInspector('stream');
    }

    if (st === 'chamber-before') {
      activateStage('insert', 'absorb');
      pulseSystemNode('emitter', d.instruction || 'queue before');
      spawnDataStream('stream', 'machine', 'frame', 'stream-token', 900);
      activateFlowParticles('stream', 'machine');
      spawnDataStream('machine', 'emitter', d.instruction || 'queue', 'stream-token', 880);
      activateFlowParticles('machine', 'emitter');
      addZoneLabel('emitter', `before · ${(d.instruction || '').slice(0, 12)}`);
      pulseZone('emitter');
      log(`Queue (before): ${d.message}`, 'substrate', ev);
      refreshInspector('emitter');
    }

    if (st === 'chamber-after') {
      activateStage('insert', `merged · ${d.instruction || '?'}`);
      pulseSystemNode('exec', d.message || 'backend after');
      // Fold: incoming written into receiver, ALU executes
      spawnFoldEffect(d.chunkText || '', d.instruction || '', d.firmware || '');
      spawnDataStream('emitter', 'exec', (d.message || '').slice(0, 24), 'stream-fold', 1000);
      activateFlowParticles('emitter', 'exec');
      addZoneLabel('exec', (d.message || '').slice(0, 28));
      pulseZone('backend');
      pulseZone('exec');
      // Continue flow through pool and back to machine (full pipeline loop)
      setTimeout(() => {
        spawnDataStream('exec', 'pool', 'fold result', 'stream-compute', 900);
        activateFlowParticles('exec', 'pool');
        addZoneLabel('pool', 'fold result');
        pulseZone('pool');
      }, 400);
      setTimeout(() => {
        spawnDataStream('pool', 'machine', 'emitted', 'stream-result', 800);
        activateFlowParticles('pool', 'machine');
        pulseZone('machine');
      }, 900);
      log(`Backend (after): ${d.message}`, 'substrate', ev);
      refreshInspector('exec');
    }

    if (st === 'kernel') {
      activateStage('lookup', d.instruction || 'cpu');
      pulseSystemNode('exec', d.instruction || 'cpu');
      spawnDataStream('exec', 'pool', d.instruction || 'ALU', 'stream-compute', 850);
      activateFlowParticles('exec', 'pool');
      addZoneLabel('exec', (d.instruction || 'op').slice(0, 20));
      pulseZone('backend');
      pulseZone('exec');
      log(`Backend: ${d.message}`, 'substrate', ev);
      refreshInspector('exec');

      // Update Value visualization
      updateValueDisplay({
        dataPop: d.dataPop,
        operandPop: d.operandPop,
        affinityPop: d.affinityPop,
        density: d.density,
      });
    }

    if (st === 'state') {
      const dens = d.density || 0;
      pushSpark(dens, 'accum');
      activateStage('fold', `${d.instruction || ''} · p=${d.accumPop ?? '?'}`);
      pulseSystemNode('exec', d.instruction || 'cpu');
      spawnDataStream('exec', 'pool', d.instruction || 'cpu', 'stream-compute', 900);
      spawnDataStream('pool', 'machine', 'out', 'stream-result', 800);
      activateFlowParticles('exec', 'pool');
      activateFlowParticles('pool', 'machine');
      addZoneLabel('pool', `${d.instruction || 'op'} acc=${d.accumPop ?? '?'}`);
      pulseZone('pool');
      log(`State: ${d.message}`, 'substrate', ev);
      refreshInspector('pool');

      // Update Value visualization
      updateValueDisplay({
        dataPop: d.dataPop,
        operandPop: d.operandPop,
        affinityPop: d.affinityPop,
        density: dens,
      });
    }

    // Hardware substrate execution (cpu, cuda, metal)
    if (st === 'cpu' || st === 'cuda' || st === 'metal') {
      const frames = d.ubFrameCount || 0;
      const dur = d.durationMs || 0;
      pulseSystemNode(st, `${frames}f ${dur}ms`);
      spawnDataStream('exec', st, `${st} ${frames}f`, 'stream-compute', 700);
      activateFlowParticles('exec', st);
      addZoneLabel(st, `${frames} frames ${dur}ms`);
      pulseZone(st);
      log(`${st.toUpperCase()}: ${frames} frames in ${dur}ms`, 'substrate', ev);
      refreshInspector(st);
    }
  }
}

// ── UniConn Network Events ─────────────────────────────────
// Handles events from the UniConn transport layer.
// Event format: { component: "UniConn", action: "...", data: {...} }
//
// Actions:
//   Transport — transport type selected (data.transport: "ipc"|"udp"|"quic")
//   Connect   — connection established (data.transport, data.addr)
//   Disconnect — connection lost (data.transport, data.addr)
//   Read      — frame read through transport (data.transport, data.bytes)
//   Write     — frame written through transport (data.transport, data.bytes)
//   Error     — transport error (data.transport, data.message)
//   GateSwitch — Gate switched from primary to secondary (data.from, data.to)
function handleUniConnEvent(ev, d) {
  const transport = d.transport || d.connType || '';

  if (ev.action === 'Transport') {
    setUniConnTransport(transport);
    log(`UniConn: transport=${transport}`, 'pipeline', ev);
  }

  if (ev.action === 'Connect') {
    setUniConnState(transport, 'connected');
    setUniConnTransport(transport);
    if (d.addr) addUniConnPeer(d.addr, transport);
    log(`UniConn: ${transport} connected → ${d.addr || 'local'}`, 'pipeline', ev);
  }

  if (ev.action === 'Disconnect') {
    setUniConnState(transport, 'idle');
    log(`UniConn: ${transport} disconnected`, 'pipeline', ev);
  }

  if (ev.action === 'Listen') {
    setUniConnState(transport, 'listening');
    log(`UniConn: ${transport} listening on ${d.addr || '?'}`, 'pipeline', ev);
  }

  if (ev.action === 'Read' || ev.action === 'Write') {
    const dir = ev.action === 'Read' ? 'in' : 'out';
    spawnUniConnFrame(transport, dir);
  }

  if (ev.action === 'Error') {
    setUniConnState(transport, 'error');
    log(`UniConn ERROR: ${transport} ${d.message || ''}`, 'pipeline', ev);
  }

  if (ev.action === 'GateSwitch') {
    const from = d.from || '';
    const to = d.to || '';
    setUniConnTransport(to);
    setUniConnState(from, 'idle');
    setUniConnState(to, 'connected');
    log(`UniConn Gate: ${from} → ${to}`, 'pipeline', ev);
  }
}
