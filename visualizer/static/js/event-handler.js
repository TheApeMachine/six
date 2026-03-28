/* ═══════════════════════════════════════════════════════════
   event-handler.js — Routes WebSocket events to visualization
   ═══════════════════════════════════════════════════════════ */
import * as state from './state.js';
import { spawnDataStream, activateFlowParticles } from './particles.js';
import {
  addZoneLabel, clearZoneLabels, pulseZone,
  advanceStreamPtr, spawnFoldEffect,
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

export function handleEvent(ev) {
  const d = ev.data || {};
  state.accumulateEvent(ev);

  // ── DMT Insert ──
  if (ev.component === 'DMT' && ev.action === 'Insert') {
    if (d.bin !== undefined) state.forestKeyBins[d.bin & 0xFF]++;
    addZoneLabel('frame', `entry #${d.entryCount || '?'}`);
    pulseZone('frame');
    refreshInspector('frame');
  }

  // ── Tokenizer Value ──
  if (ev.component === 'Tokenizer' && ev.action === 'Value') {
    const stg = d.stage || '';
    const edgeCount = d.edgeCount || 0;
    const text = d.chunkText || '';

    if (stg === 'ingest-tokenize') {
      state.inc('totalIngested');
      advanceStreamPtr(); // Stream write advances ptr
      activateStage('tokenize', `${edgeCount} edges`);
      spawnDataStream('dataset', 'frame', text || 'raw bytes', 'stream-ingest', 1200);
      activateFlowParticles('dataset', 'frame');
      addZoneLabel('frame', text);
      pulseZone('dataset');
      pulseZone('frame');
      log(`Ingest: "${text.slice(0, 50)}"`, 'ingest', ev);
    }

    if (stg === 'tokenize') {
      state.inc('totalQueries');
      activateStage('tokenize', `${edgeCount} edges`);
      spawnDataStream('frame', 'machine', text || 'tokens', 'stream-token', 1000);
      activateFlowParticles('frame', 'machine');
      addZoneLabel('frame', text);
      pulseZone('frame');
      log(`Query: "${text.slice(0, 50)}"`, 'tokenize', ev);
    }

    if (d.bin !== undefined) {
      state.inc('totalTokenNodes');
      state.tokenBinCounts[d.bin & 0xFF]++;
      state.inc('tokenBinMax', state.tokenBinCounts[d.bin & 0xFF]);
    }

    refreshInspector('frame');
  }

  // ── LSM Insert ──
  else if (ev.component === 'LSM' && ev.action === 'Insert') {
    const edges = d.edges || d.edgeCount || 0;
    const insertText = d.chunkText || `${edges} edge`;
    state.inc('totalEdges', edges);
    activateStage('insert', `${edges} stored`);
    spawnDataStream('machine', 'stream', insertText, 'stream-store', 1400);
    activateFlowParticles('machine', 'stream');
    state.inc('totalFlowEdges');
    addZoneLabel('stream', insertText);
    pulseZone('stream');
    log(`Radix insert: ${insertText}`, 'ingest', ev);
    refreshInspector('stream');
  }

  // ── SpatialIndex Lookup ──
  else if (ev.component === 'SpatialIndex' && ev.action === 'Lookup') {
    const pathCount = d.pathCount || d.paths || 0;
    state.inc('totalPaths', pathCount);
    activateStage('lookup', `${pathCount} paths`);
    spawnDataStream('backend', 'pool', `${pathCount} paths found`, 'stream-lookup', 1000);
    addZoneLabel('backend', `${pathCount} paths`);
    pulseZone('backend');
    log(`Lookup: ${pathCount} paths`, 'lookup', ev);
    refreshInspector('backend');
  }

  // ── Graph Evaluate ──
  else if (ev.component === 'Graph' && ev.action === 'Evaluate') {
    const pathCount = d.pathCount || 0;
    activateStage('fold', `${pathCount} results`);
    spawnDataStream('backend', 'pool', `evaluate ${pathCount} paths`, 'stream-fold', 1200);
    log(`Fold complete: ${pathCount} result paths`, 'fold', ev);
    refreshInspector('backend');
  }

  // ── Graph Fold ──
  else if (ev.component === 'Graph' && ev.action === 'Fold') {
    state.inc('totalFolds');
    state.inc('totalFoldLinks');
    const foldText = d.chunkText || '';
    if (addFoldNodeFn) addFoldNodeFn(d.bin || 0, d.level || 0, d.density || 0, foldText, d.childCount || 0);
    // Show fold: incoming Value written into receiver's ALU (UniversalBitwise)
    spawnFoldEffect(foldText, d.partnerText || '', d.firmware || '');
    spawnDataStream('backend', 'pool', foldText || `L${d.level} fold`, 'stream-fold', 1400);
    activateFlowParticles('backend', 'pool');
    addZoneLabel('backend', foldText || `L${d.level} fold`);

    state.foldHistory.push({
      level: d.level || 0, bin: d.bin || 0, density: d.density || 0,
      text: foldText, children: d.childCount || 0, ts: ev._ts,
    });
    while (state.foldHistory.length > 200) state.foldHistory.shift();
    pulseZone('backend');
    log(`Fold L${d.level} bin=${d.bin} children=${d.childCount} "${foldText.slice(0, 40)}"`, 'fold', ev);
    refreshInspector('backend');
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
    addZoneLabel('backend', spanLabel);
    if (level === 0) {
      spawnDataStream('backend', 'pool', spanText || `span`, 'stream-fold', 1000);
    }
    log(`FoldSpan L${level}: [${d.left}:${d.right}] "${spanText.slice(0, 40)}"`, 'fold');
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

  // ── Substrate ──
  else if (ev.component === 'Substrate') {
    handleSubstrateEvent(ev, d);
  }

  // ── Backend (graph nodes/edges) ──
  else if (ev.component === 'Backend') {
    pulseSystemNode('backend', d.nodeType || ev.action || 'backend');
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

  // Update HUD after processing
  if (hudUpdateFn) hudUpdateFn();
}

function handleValueEvent(d, ev) {
  const snapshot = d.value || d.snapshot || d;
  if (!snapshot) return;

  if (snapshot._ts == null) {
    snapshot._ts = ev._ts || Date.now();
  }

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

  spawnDataStream('stream', 'emitter', snapshot.tokenPreview || snapshot.tokenText || 'value', 'stream-token', 900);
  activateFlowParticles('stream', 'emitter');
  spawnDataStream('emitter', 'backend', snapshot.summary || snapshot.tokenPreview || 'value', 'stream-fold', 1100);
  activateFlowParticles('emitter', 'backend');

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
  refreshInspector('backend');
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
      spawnDataStream('stream', 'emitter', promptText, 'stream-prompt', 800);
      activateFlowParticles('stream', 'emitter');
      addZoneLabel('emitter', promptText);
      pulseZone('emitter');
    }, 400);
    setTimeout(() => {
      spawnDataStream('emitter', 'backend', promptText, 'stream-prompt', 800);
      activateFlowParticles('emitter', 'backend');
      addZoneLabel('backend', promptText);
      pulseZone('backend');
    }, 800);

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
    spawnDataStream('backend', 'pool', resultContent || 'result', 'stream-result', 1000);
    activateFlowParticles('backend', 'pool');
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
    spawnDataStream('stream', 'emitter', stepLabel, adv ? 'stream-result' : 'stream-fold', 700);
    activateFlowParticles('stream', 'emitter');
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
    spawnDataStream('backend', 'pool', d.jobId || 'job', 'stream-compute', 800);
    activateFlowParticles('backend', 'pool');
    addZoneLabel('pool', `schedule: ${d.jobId || '?'}`);
    pulseZone('pool');
    log(`Schedule: ${d.jobId} type=${d.taskType} queue=${d.queueSize}`, 'pool', ev);
  }

  if (ev.action === 'Dispatch') {
    pulseSystemNode('pool', d.taskType || d.jobId || 'dispatch');
    spawnDataStream('backend', 'pool', d.taskType || 'task', 'stream-compute', 600);
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

function handleKernelEvent(ev, d) {
  if (ev.action === 'Route') {
    const selectedHardware = pulseSystemBackendSelection(d.bestIndex, `route #${d.bestIndex}`);
    d.selectedHardware = selectedHardware;
    addZoneLabel('backend', `backend #${d.bestIndex} avail=${d.avail}`);
    spawnDataStream('backend', 'pool', `backend #${d.bestIndex}`, 'stream-kernel', 800);
    activateFlowParticles('backend', 'pool');
    pulseZone('backend');
    log(`Route: backend #${d.bestIndex} avail=${d.avail}`, 'backend', ev);
  }

  if (ev.action === 'PeerAdd') {
    pulseSystemNode('backend', d.nodeAddr || 'peer');
    addZoneLabel('backend', `peer: ${d.nodeAddr}`);
    log(`PeerAdd: ${d.nodeAddr} (${d.nodeCount} nodes)`, 'backend', ev);
  }

  if (ev.action === 'WriteError') {
    pulseSystemNode('backend', d.message || 'write error');
    addZoneLabel('backend', `ERR: ${(d.message || '').slice(0, 20)}`);
    log(`Kernel error: ${d.message}`, 'backend', ev);
  }

  refreshInspector('backend');
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
      pulseSystemNode('emitter', d.instruction || 'emitter before');
      spawnDataStream('stream', 'emitter', 'frame', 'stream-token', 900);
      activateFlowParticles('stream', 'emitter');
      addZoneLabel('emitter', `before · ${(d.instruction || '').slice(0, 12)}`);
      pulseZone('emitter');
      log(`Emitter (before): ${d.message}`, 'substrate', ev);
      refreshInspector('emitter');
    }

    if (st === 'chamber-after') {
      activateStage('insert', `merged · ${d.instruction || '?'}`);
      pulseSystemNode('backend', d.message || 'backend after');
      // Fold: incoming written into receiver, ALU executes
      spawnFoldEffect(d.chunkText || '', d.instruction || '', d.firmware || '');
      spawnDataStream('emitter', 'backend', (d.message || '').slice(0, 24), 'stream-fold', 1000);
      activateFlowParticles('emitter', 'backend');
      addZoneLabel('backend', (d.message || '').slice(0, 28));
      pulseZone('backend');
      log(`Backend (after): ${d.message}`, 'substrate', ev);
      refreshInspector('backend');
    }

    if (st === 'kernel') {
      activateStage('lookup', d.instruction || 'cpu');
      pulseSystemNode('backend', d.instruction || 'cpu');
      spawnDataStream('backend', 'pool', d.instruction || 'ALU', 'stream-compute', 850);
      activateFlowParticles('backend', 'pool');
      addZoneLabel('backend', (d.instruction || 'op').slice(0, 20));
      pulseZone('backend');
      log(`Backend: ${d.message}`, 'substrate', ev);
      refreshInspector('backend');

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
      pulseSystemNode('backend', d.instruction || 'cpu');
      spawnDataStream('backend', 'pool', d.instruction || 'cpu', 'stream-compute', 900);
      spawnDataStream('pool', 'machine', 'out', 'stream-result', 800);
      activateFlowParticles('backend', 'pool');
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
  }
}
