/* ═══════════════════════════════════════════════════════════
   event-handler.js — Routes WebSocket events to visualization
   ═══════════════════════════════════════════════════════════ */
import * as state from './state.js';
import { spawnDataStream, activateFlowParticles } from './particles.js';
import { addZoneLabel, clearZoneLabels, pulseZone } from './architecture.js';
import { updateValueDisplay } from './value-viz.js';

let hudUpdateFn = null;
let inspectorRefreshFn = null;
let logFn = null;
let activateStageFn = null;
let pushSparkFn = null;
let addFoldNodeFn = null;
let resetFoldGraphFn = null;
let graphAddNodeFn = null;
let graphAddEdgeFn = null;

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
    spawnDataStream('machine', 'chamber', insertText, 'stream-store', 1400);
    activateFlowParticles('machine', 'chamber');
    state.inc('totalFlowEdges');
    pulseZone('chamber');
    log(`Radix insert: ${insertText}`, 'ingest', ev);
    refreshInspector('frame');
  }

  // ── SpatialIndex Lookup ──
  else if (ev.component === 'SpatialIndex' && ev.action === 'Lookup') {
    const pathCount = d.pathCount || d.paths || 0;
    state.inc('totalPaths', pathCount);
    activateStage('lookup', `${pathCount} paths`);
    spawnDataStream('chamber', 'machine', `${pathCount} paths found`, 'stream-lookup', 1000);
    addZoneLabel('chamber', `${pathCount} paths`);
    pulseZone('chamber');
    log(`Lookup: ${pathCount} paths`, 'lookup', ev);
    refreshInspector('chamber');
  }

  // ── Graph Evaluate ──
  else if (ev.component === 'Graph' && ev.action === 'Evaluate') {
    const pathCount = d.pathCount || 0;
    activateStage('fold', `${pathCount} results`);
    spawnDataStream('machine', 'chamber', `evaluate ${pathCount} paths`, 'stream-fold', 1200);
    log(`Fold complete: ${pathCount} result paths`, 'fold', ev);
    refreshInspector('chamber');
  }

  // ── Graph Fold ──
  else if (ev.component === 'Graph' && ev.action === 'Fold') {
    state.inc('totalFolds');
    state.inc('totalFoldLinks');
    const foldText = d.chunkText || '';
    if (addFoldNodeFn) addFoldNodeFn(d.bin || 0, d.level || 0, d.density || 0, foldText, d.childCount || 0);
    spawnDataStream('chamber', 'kernel', foldText || `L${d.level} fold`, 'stream-fold', 1400);
    activateFlowParticles('chamber', 'kernel');

    state.foldHistory.push({
      level: d.level || 0, bin: d.bin || 0, density: d.density || 0,
      text: foldText, children: d.childCount || 0, ts: ev._ts,
    });
    while (state.foldHistory.length > 200) state.foldHistory.shift();
    pulseZone('chamber');
    log(`Fold L${d.level} bin=${d.bin} children=${d.childCount} "${foldText.slice(0, 40)}"`, 'fold', ev);
    refreshInspector('chamber');
  }

  // ── Machine Pipeline ──
  else if (ev.component === 'Machine' && ev.action === 'Pipeline') {
    handleMachineEvent(d, ev);
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
    addZoneLabel('chamber', spanLabel);
    if (level === 0) {
      spawnDataStream('machine', 'chamber', spanText || `span`, 'stream-fold', 1000);
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

function handleMachineEvent(d, ev) {
  const stg = d.stage || '';

  if (stg === 'prompt-start') {
    if (resetFoldGraphFn) resetFoldGraphFn();
    clearZoneLabels('chamber');
    const promptText = d.message || d.msg || '';

    spawnDataStream('dataset', 'frame', promptText, 'stream-prompt', 800);
    activateFlowParticles('dataset', 'frame');
    setTimeout(() => {
      spawnDataStream('frame', 'machine', promptText, 'stream-prompt', 800);
      activateFlowParticles('frame', 'machine');
    }, 400);
    setTimeout(() => {
      spawnDataStream('machine', 'chamber', promptText, 'stream-prompt', 800);
      activateFlowParticles('machine', 'chamber');
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
    spawnDataStream('chamber', 'machine', resultContent || 'result', 'stream-result', 1000);
    activateFlowParticles('chamber', 'machine');
    addZoneLabel('machine', resultContent.slice(0, 30));
    pulseZone('machine');

    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.result = resultContent; lastPrompt.stage = 'complete'; }
    log(`Result: "${resultContent.slice(0, 60)}"`, 'result', ev);
  }

  if (stg === 'prompt-empty') {
    activateStage('decode', '∅');
    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.result = '∅ empty'; lastPrompt.stage = 'empty'; }
    log(`Empty result`, 'state', ev);
  }

  if (stg === 'prompt-error') {
    const lastPrompt = state.promptHistory[state.promptHistory.length - 1];
    if (lastPrompt) { lastPrompt.error = d.message; lastPrompt.stage = 'error'; }
    log(`Prompt error: ${d.message}`, 'state', ev);
  }

  refreshInspector('machine');
}

function handleProgramExecute(d, ev) {
  const stg = d.stage || '';

  if (stg === 'start') {
    spawnDataStream('machine', 'chamber', `${d.candidateCount} candidates`, 'stream-prompt', 1000);
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
    spawnDataStream('machine', 'chamber', stepLabel, adv ? 'stream-result' : 'stream-fold', 700);

    const lastExec = state.executeHistory[state.executeHistory.length - 1];
    if (lastExec) lastExec.steps.push({ step, pre, post, advanced: adv, stable: d.stable });
    log(`Execute step ${step}: ${pre}→${post}`, 'pipeline', ev);
  }

  if (stg === 'complete') {
    const outcome = d.outcome || '';
    if (outcome === 'stable') {
      spawnDataStream('chamber', 'machine', `stable (step ${d.step})`, 'stream-result', 1000);
      activateFlowParticles('chamber', 'machine');
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
    spawnDataStream('machine', 'kernel', d.jobId || 'job', 'stream-compute', 800);
    activateFlowParticles('machine', 'kernel');
    addZoneLabel('kernel', `schedule: ${d.jobId || '?'}`);
    pulseZone('kernel');
    log(`Schedule: ${d.jobId} type=${d.taskType} queue=${d.queueSize}`, 'compute', ev);
  }

  if (ev.action === 'Dispatch') {
    spawnDataStream('machine', 'kernel', d.taskType || 'task', 'stream-compute', 600);
    addZoneLabel('kernel', `dispatch: ${d.taskType || '?'}`);
    log(`Dispatch: ${d.jobId} type=${d.taskType}`, 'compute', ev);
  }

  if (ev.action === 'JobDone') {
    state.inc('totalJobsDone');
    spawnDataStream('kernel', 'machine', `done ${d.durationMs}ms`, 'stream-result', 800);
    activateFlowParticles('kernel', 'machine');
    addZoneLabel('kernel', `done: ${d.durationMs}ms`);
    pulseZone('kernel');

    state.poolJobHistory.push({ id: d.jobId, type: d.taskType, duration: d.durationMs, success: true, ts: ev._ts });
    while (state.poolJobHistory.length > 100) state.poolJobHistory.shift();
    state.poolLatencyHistory.push(d.durationMs || 0);
    while (state.poolLatencyHistory.length > 200) state.poolLatencyHistory.shift();
    log(`JobDone: ${d.jobId} ${d.durationMs}ms`, 'compute', ev);
  }

  if (ev.action === 'JobFail') {
    state.inc('totalJobsFailed');
    addZoneLabel('kernel', `FAIL: ${(d.message || '').slice(0, 24)}`);
    state.poolJobHistory.push({ id: d.jobId, type: d.taskType, duration: d.durationMs, success: false, error: d.message, ts: ev._ts });
    while (state.poolJobHistory.length > 100) state.poolJobHistory.shift();
    log(`JobFail: ${d.jobId} "${(d.message || '').slice(0, 40)}"`, 'compute', ev);
  }

  if (ev.action === 'Drop') {
    addZoneLabel('kernel', `DROP: ${d.jobId || '?'}`);
    log(`Drop: ${d.jobId} — ${d.message}`, 'compute', ev);
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
    addZoneLabel('kernel', `scale ${dir}: ${cnt} → ${total}`);
    state.poolScaleHistory.push({ direction: dir, count: cnt, total, ts: ev._ts });
    while (state.poolScaleHistory.length > 50) state.poolScaleHistory.shift();
    log(`Scale ${dir}: ${cnt} workers (total=${total})`, 'compute', ev);
  }

  refreshInspector('kernel');
}

function handleKernelEvent(ev, d) {
  if (ev.action === 'Route') {
    addZoneLabel('kernel', `backend #${d.bestIndex} avail=${d.avail}`);
    spawnDataStream('kernel', 'machine', `backend #${d.bestIndex}`, 'stream-kernel', 800);
    pulseZone('kernel');
    log(`Route: backend #${d.bestIndex} avail=${d.avail}`, 'kernel', ev);
  }

  if (ev.action === 'PeerAdd') {
    addZoneLabel('kernel', `peer: ${d.nodeAddr}`);
    log(`PeerAdd: ${d.nodeAddr} (${d.nodeCount} nodes)`, 'kernel', ev);
  }

  if (ev.action === 'WriteError') {
    addZoneLabel('kernel', `ERR: ${(d.message || '').slice(0, 20)}`);
    log(`Kernel error: ${d.message}`, 'kernel', ev);
  }

  refreshInspector('kernel');
}

function handleSubstrateEvent(ev, d) {
  if (ev.action === 'Run') {
    if (d.stage === 'start') {
      if (resetFoldGraphFn) resetFoldGraphFn();
      clearZoneLabels('chamber');
      clearZoneLabels('dataset');
      log(`Substrate: ${d.message}`, 'substrate', ev);
      refreshInspector('machine');
    }

    if (d.stage === 'complete') {
      activateStage('decode', 'ok');
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
      activateStage('tokenize', `row ${d.frameIndex}`);
      spawnDataStream('dataset', 'frame', (d.chunkText || 'frame').slice(0, 28), 'stream-ingest', 1200);
      activateFlowParticles('dataset', 'frame');
      addZoneLabel('dataset', `frame ${d.frameIndex}`);
      pulseZone('dataset');
      log(`Frame ${d.frameIndex}: ${d.message}`, 'substrate', ev);
      refreshInspector('dataset');
    }

    if (st === 'chamber-before') {
      activateStage('insert', 'absorb');
      spawnDataStream('frame', 'machine', 'chamber', 'stream-token', 900);
      activateFlowParticles('frame', 'machine');
      addZoneLabel('chamber', `before · ${(d.instruction || '').slice(0, 12)}`);
      pulseZone('chamber');
      log(`Chamber (before): ${d.message}`, 'substrate', ev);
      refreshInspector('chamber');
    }

    if (st === 'chamber-after') {
      activateStage('insert', `merged · ${d.instruction || '?'}`);
      spawnDataStream('machine', 'chamber', (d.message || '').slice(0, 24), 'stream-fold', 1000);
      activateFlowParticles('machine', 'chamber');
      addZoneLabel('machine', (d.message || '').slice(0, 28));
      pulseZone('machine');
      log(`Chamber (after): ${d.message}`, 'substrate', ev);
      refreshInspector('machine');
    }

    if (st === 'kernel') {
      activateStage('lookup', d.instruction || 'cpu');
      spawnDataStream('chamber', 'kernel', d.instruction || 'ALU', 'stream-compute', 850);
      activateFlowParticles('chamber', 'kernel');
      addZoneLabel('kernel', (d.instruction || 'op').slice(0, 20));
      pulseZone('kernel');
      log(`Kernel: ${d.message}`, 'substrate', ev);
      refreshInspector('kernel');

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
      spawnDataStream('chamber', 'kernel', d.instruction || 'cpu', 'stream-compute', 900);
      spawnDataStream('kernel', 'machine', 'out', 'stream-result', 800);
      activateFlowParticles('kernel', 'machine');
      addZoneLabel('kernel', `${d.instruction || 'op'} acc=${d.accumPop ?? '?'}`);
      pulseZone('kernel');
      log(`State: ${d.message}`, 'substrate', ev);
      refreshInspector('kernel');

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
