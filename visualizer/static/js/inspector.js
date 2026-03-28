/* ═══════════════════════════════════════════════════════════
   inspector.js — Inspector panel (click-to-inspect zones)
   ═══════════════════════════════════════════════════════════ */
import * as state from './state.js';
import { SYS, resolveZoneKey } from './architecture.js';
import { expandValueSnapshot } from './value-viz.js';

let inspectorRefreshTimer = null;

const els = {};
const TAB_SETS = {
  zone: [
    { id: 'feed', label: 'Feed' },
    { id: 'state', label: 'State' },
    { id: 'internals', label: 'Internals' },
  ],
  value: [
    { id: 'summary', label: 'Summary' },
    { id: 'program', label: 'Program' },
    { id: 'raw', label: 'Raw' },
  ],
};

export function initInspector() {
  els.panel = document.getElementById('inspector-panel');
  els.title = document.getElementById('inspector-title');
  els.desc = document.getElementById('inspector-desc');
  els.body = document.getElementById('inspector-body');
  els.close = document.getElementById('inspector-close');
  els.tabs = document.getElementById('inspector-tabs');
  els.detailOverlay = document.getElementById('event-detail-overlay');
  els.detailJson = document.getElementById('event-detail-json');
  els.detailClose = document.getElementById('event-detail-close');

  els.close.addEventListener('click', closeInspector);
  els.tabs.addEventListener('click', (e) => {
    const tab = e.target.closest('.inspector-tab');
    if (!tab) return;
    state.set('inspectorTab', tab.dataset.tab);
    syncInspectorTabs();
    renderInspector();
  });

  els.detailClose.addEventListener('click', closeEventDetail);
  els.detailOverlay.addEventListener('click', (e) => {
    if (e.target === els.detailOverlay) closeEventDetail();
  });
}

export function openInspector(sysKey) {
  const actualKey = resolveZoneKey(sysKey);
  state.set('inspectorMode', 'zone');
  state.set('inspectorKey', actualKey);
  state.set('inspectorOpen', true);
  state.set('inspectorTab', 'feed');

  els.panel.classList.add('open');
  els.title.textContent = SYS[actualKey]?.label || actualKey;
  els.desc.textContent = state.ZONE_DESCRIPTIONS[actualKey] || '';

  syncInspectorTabs();
  renderInspector();
}

export function openValueInspector(valueId) {
  state.set('inspectorMode', 'value');
  state.set('inspectorKey', String(valueId));
  state.set('inspectorOpen', true);
  state.set('inspectorTab', 'summary');

  els.panel.classList.add('open');
  els.title.textContent = `Value #${String(valueId)}`;
  els.desc.textContent = 'Decoded binary Value snapshot';

  syncInspectorTabs();
  renderInspector();
}

export function closeInspector() {
  state.set('inspectorOpen', false);
  state.set('inspectorKey', null);
  state.set('inspectorMode', 'zone');
  els.panel.classList.remove('open');
}

export function refreshInspectorIfMatch(key) {
  if (!state.inspectorOpen || resolveZoneKey(state.inspectorKey) !== resolveZoneKey(key)) return;
  if (inspectorRefreshTimer) return;
  inspectorRefreshTimer = setTimeout(() => {
    inspectorRefreshTimer = null;
    renderInspector();
  }, 200);
}

export function showEventDetail(ev) {
  els.detailJson.textContent = JSON.stringify(ev, null, 2);
  els.detailOverlay.style.display = 'flex';
}

export function closeEventDetail() {
  els.detailOverlay.style.display = 'none';
}

export function isDetailOpen() {
  return els.detailOverlay && els.detailOverlay.style.display !== 'none';
}

function syncInspectorTabs() {
  if (!els.tabs) return;
  const tabs = TAB_SETS[state.inspectorMode] || TAB_SETS.zone;
  els.tabs.innerHTML = '';
  for (const tab of tabs) {
    const btn = document.createElement('button');
    btn.className = 'inspector-tab';
    btn.dataset.tab = tab.id;
    btn.textContent = tab.label;
    btn.classList.toggle('active', tab.id === state.inspectorTab);
    els.tabs.appendChild(btn);
  }
}

// ── Render ──
function renderInspector() {
  if (!state.inspectorOpen || !state.inspectorKey) return;
  if (state.inspectorMode === 'value') {
    if (state.inspectorTab === 'summary') renderValueSummaryTab();
    else if (state.inspectorTab === 'program') renderValueProgramTab();
    else if (state.inspectorTab === 'raw') renderValueRawTab();
    return;
  }

  if (state.inspectorTab === 'feed') renderFeedTab();
  else if (state.inspectorTab === 'state') renderStateTab();
  else if (state.inspectorTab === 'internals') renderInternalsTab();
}

function renderFeedTab() {
  const actualKey = resolveZoneKey(state.inspectorKey);
  const components = state.ZONE_COMPONENTS[actualKey] || [];
  const events = [];
  for (const comp of components) {
    events.push(...(state.eventsByComponent.get(comp) || []));
  }
  events.sort((a, b) => (b._ts || 0) - (a._ts || 0));

  els.body.innerHTML = '';

  if (events.length === 0) {
    const wrap = document.createElement('div');
    wrap.className = 'inspector-empty';
    wrap.style.whiteSpace = 'pre-line';
    const p1 = document.createElement('div');
    p1.textContent = 'No events for this zone yet.';
    const p2 = document.createElement('div');
    p2.style.marginTop = '0.75em';
    const code = document.createElement('code');
    code.style.opacity = '0.8';
    code.textContent = 'go run . viz';
    p2.append(
      document.createTextNode('Run the live demo ('),
      code,
      document.createTextNode(') to stream telemetry.'),
    );
    wrap.append(p1, p2);
    els.body.appendChild(wrap);
    return;
  }

  const countDiv = document.createElement('div');
  countDiv.className = 'inspector-section-title';
  countDiv.textContent = `${events.length} events`;
  els.body.appendChild(countDiv);

  for (const ev of events.slice(0, 80)) {
    const div = document.createElement('div');
    div.className = 'inspector-event';
    const header = document.createElement('div');
    header.className = 'inspector-event-header';
    const comp = document.createElement('span');
    comp.className = 'inspector-event-comp';
    comp.textContent = ev.component ?? '';
    const action = document.createElement('span');
    action.className = 'inspector-event-action';
    action.textContent = ev.action ?? '';
    const ts = document.createElement('span');
    ts.className = 'inspector-event-ts';
    ts.textContent = `${((ev._ts || 0) / 1000).toFixed(1)}s`;
    header.append(comp, action, ts);
    const dataEl = document.createElement('div');
    dataEl.className = 'inspector-event-data';
    dataEl.textContent = formatEventData(ev.data);
    div.append(header, dataEl);
    div.addEventListener('click', () => showEventDetail(ev));
    els.body.appendChild(div);
  }
}

function renderStateTab() {
  els.body.innerHTML = '';
  const actualKey = resolveZoneKey(state.inspectorKey);
  const components = state.ZONE_COMPONENTS[actualKey] || [];
  const events = [];
  for (const comp of components) events.push(...(state.eventsByComponent.get(comp) || []));

  const section = makeSection('Accumulated State');
  const stats = computeSubsystemStats(actualKey, events);
  for (const [key, val] of stats) {
    const row = document.createElement('div');
    row.className = 'inspector-stat';
    const sk = document.createElement('span');
    sk.textContent = key;
    const sv = document.createElement('span');
    sv.className = `val ${val.cls || ''}`;
    sv.textContent = val.text;
    row.append(sk, sv);
    section.appendChild(row);
  }
  els.body.appendChild(section);
}

function renderInternalsTab() {
  els.body.innerHTML = '';
  switch (resolveZoneKey(state.inspectorKey)) {
    case 'machine':
      renderMachineInternals();
      break;
    case 'stream':
      renderTokenizerInternals();
      break;
    case 'emitter':
      renderEmitterInternals();
      break;
    case 'backend':
      renderKernelInternals();
      break;
    case 'pool':
      renderPoolInternals();
      break;
    case 'cuda':
    case 'metal':
    case 'cpu':
      renderHardwareInternals(resolveZoneKey(state.inspectorKey));
      break;
    default:
      renderGenericInternals();
      break;
  }
}

function computeSubsystemStats(sysKey, events) {
  const stats = [];
  switch (resolveZoneKey(sysKey)) {
    case 'machine':
      stats.push(['Prompts', { text: state.promptHistory.length.toLocaleString(), cls: 'accent' }]);
      stats.push(['Executions', { text: state.executeHistory.length.toLocaleString() }]);
      stats.push(['Substrate frames', { text: state.substrateFrames.toLocaleString() }]);
      break;
    case 'stream':
      stats.push(['Frames', { text: state.substrateFrames.toLocaleString(), cls: 'accent' }]);
      stats.push(['Token nodes', { text: state.totalTokenNodes.toLocaleString() }]);
      stats.push(['Flow edges', { text: state.totalFlowEdges.toLocaleString() }]);
      break;
    case 'emitter':
      stats.push(['Value snapshots', { text: state.valueHistory.length.toLocaleString(), cls: 'accent' }]);
      stats.push(['Selected value', { text: state.lastValueSummary || '—' }]);
      stats.push(['Value events', { text: state.totalValueFrames.toLocaleString() }]);
      break;
    case 'backend': {
      const kernelEvents = state.eventsByComponent.get('Kernel') || [];
      stats.push(['Route events', { text: kernelEvents.filter(e => e.action === 'Route').length.toLocaleString(), cls: 'accent' }]);
      stats.push(['Backend events', { text: (state.eventsByComponent.get('Backend') || []).length.toLocaleString() }]);
      stats.push(['Pool jobs done', { text: state.totalJobsDone.toLocaleString() }]);
      break;
    }
    case 'pool':
      stats.push(['Workers', { text: state.poolWorkerCount.toLocaleString(), cls: 'accent' }]);
      stats.push(['Idle', { text: state.poolIdleWorkers.toLocaleString() }]);
      stats.push(['Queue', { text: state.poolQueueSize.toLocaleString() }]);
      stats.push(['Success', { text: `${(state.poolSuccessRate * 100).toFixed(1)}%` }]);
      break;
    case 'cuda':
    case 'metal':
    case 'cpu': {
      const hardware = resolveZoneKey(sysKey);
      const kernelEvents = state.eventsByComponent.get('Kernel') || [];
      const routes = kernelEvents.filter(e => e.data?.selectedHardware === hardware);
      stats.push(['Route hits', { text: routes.length.toLocaleString(), cls: 'accent' }]);
      stats.push(['Backend routes', { text: (state.eventsByComponent.get('Backend') || []).length.toLocaleString() }]);
      stats.push(['Pool jobs', { text: state.totalJobsScheduled.toLocaleString() }]);
      break;
    }
    default:
      stats.push(['Total Events', { text: events.length.toLocaleString() }]);
  }
  return stats;
}

function renderForestInternals() {
  const sec1 = makeSection('Key Prefix Distribution (256 bins)');
  const totalKeys = state.forestKeyBins.reduce((s, v) => s + v, 0);
  const maxCount = Math.max(1, ...state.forestKeyBins);

  if (totalKeys === 0) {
    sec1.appendChild(emptyMsg('No key data yet. Keys will appear as data is inserted.'));
  } else {
    const heatmap = document.createElement('div');
    heatmap.className = 'key-heatmap';
    for (let i = 0; i < 256; i++) {
      const cell = document.createElement('div');
      cell.className = 'heatmap-cell';
      const intensity = state.forestKeyBins[i] / maxCount;
      cell.style.background = `rgba(140, 200, 255, ${0.05 + intensity * 0.7})`;
      cell.dataset.label = `0x${i.toString(16).padStart(2, '0')}: ${state.forestKeyBins[i]} keys`;
      heatmap.appendChild(cell);
    }
    sec1.appendChild(heatmap);
  }
  els.body.appendChild(sec1);
}

function renderGraphInternals() {
  const sec1 = makeSection('Fold Hierarchy');
  if (state.foldHistory.length === 0) {
    sec1.appendChild(emptyMsg('No fold events recorded yet.'));
  } else {
    const container = document.createElement('div');
    container.className = 'fold-tree-container';
    for (const fold of state.foldHistory.slice(-40).reverse()) {
      const node = document.createElement('div');
      node.className = 'fold-tree-node';
      node.style.marginLeft = `${fold.level * 12}px`;
      const lv = document.createElement('span');
      lv.className = 'fold-level';
      lv.textContent = `L${fold.level}`;
      node.append(lv, document.createTextNode(` bin=${fold.bin} ch=${fold.children} `));
      if (fold.text) {
        const qt = document.createElement('span');
        qt.textContent = `"${String(fold.text).slice(0, 24)}"`;
        node.appendChild(qt);
      }
      const dn = document.createElement('span');
      dn.className = 'fold-density';
      dn.textContent = `${(fold.density * 100).toFixed(0)}%`;
      node.appendChild(dn);
      container.appendChild(node);
    }
    sec1.appendChild(container);
  }
  els.body.appendChild(sec1);
}

function renderMachineInternals() {
  const sec1 = makeSection('Prompt History');
  if (state.promptHistory.length === 0) {
    sec1.appendChild(emptyMsg('No prompts sent yet.'));
  } else {
    for (const p of [...state.promptHistory].reverse()) {
      const div = document.createElement('div');
      div.className = 'timeline-prompt';
      const promptQ = document.createElement('div');
      promptQ.className = 'prompt-q';
      promptQ.textContent = `Q: ${(p.prompt || '').slice(0, 60)}`;
      div.appendChild(promptQ);
      if (p.result) {
        const promptA = document.createElement('div');
        promptA.className = 'prompt-a';
        promptA.textContent = `A: ${(p.result || '').slice(0, 80)}`;
        div.appendChild(promptA);
      } else if (p.error) {
        const promptErr = document.createElement('div');
        promptErr.className = 'prompt-err';
        promptErr.textContent = `Error: ${(p.error || '').slice(0, 60)}`;
        div.appendChild(promptErr);
      }
      sec1.appendChild(div);
    }
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Execution Traces');
  if (state.executeHistory.length === 0) {
    sec2.appendChild(emptyMsg('No executions recorded yet.'));
  } else {
    for (const exec of [...state.executeHistory].reverse().slice(0, 10)) {
      const div = document.createElement('div');
      div.className = 'timeline-list';
      const dotCls = exec.outcome === 'stable' ? 'complete'
        : exec.outcome === 'stalled' ? 'error'
        : exec.outcome ? 'pending' : 'active';
      const header = document.createElement('div');
      header.className = 'timeline-entry';
      const dot = document.createElement('div');
      dot.className = `timeline-dot ${dotCls}`;
      const lab = document.createElement('span');
      lab.className = 'timeline-label';
      lab.textContent = `${exec.candidates || 0} candidates`;
      const det = document.createElement('span');
      det.className = 'timeline-detail';
      det.textContent = exec.outcome || 'running';
      header.append(dot, lab, det);
      div.appendChild(header);
      sec2.appendChild(div);
    }
  }
  els.body.appendChild(sec2);
}

function renderTokenizerInternals() {
  const sec1 = makeSection('Stream Token Distribution');
  const maxVal = Math.max(1, state.tokenBinMax);
  const usedBins = [];
  for (let i = 0; i < 256; i++) if (state.tokenBinCounts[i] > 0) usedBins.push(i);

  if (usedBins.length === 0) {
    sec1.appendChild(emptyMsg('No token bins recorded yet.'));
  } else {
    const barContainer = document.createElement('div');
    barContainer.className = 'bin-bars';
    for (let i = 0; i < 256; i++) {
      const bar = document.createElement('div');
      bar.className = 'bin-bar';
      bar.style.height = `${Math.max((state.tokenBinCounts[i] / maxVal) * 100, 1)}%`;
      bar.dataset.label = `bin ${i}: ${state.tokenBinCounts[i]}`;
      barContainer.appendChild(bar);
    }
    sec1.appendChild(barContainer);
  }
  els.body.appendChild(sec1);
}

function renderEmitterInternals() {
  const sec1 = makeSection('Recent Value Snapshots');
  const recent = [...state.valueHistory].slice(-24).reverse();

  if (recent.length === 0) {
    sec1.appendChild(emptyMsg('No Value snapshots recorded yet.'));
  } else {
    for (const snapshot of recent) {
      const row = document.createElement('div');
      row.className = 'timeline-prompt';
      const promptQ = document.createElement('div');
      promptQ.className = 'prompt-q';
      promptQ.textContent = snapshot.summary || snapshot.tokenPreview || `Value ${snapshot.valueId || '?'}`;
      const promptA = document.createElement('div');
      promptA.className = 'prompt-a';
      promptA.textContent = `prev=${snapshot.prevId || '0'} · next=${snapshot.nextId || '0'} · ` +
        `aff=${snapshot.affinityPop ?? 0} · pc=${snapshot.pc || '0'}`;
      row.append(promptQ, promptA);
      sec1.appendChild(row);
    }
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Wire Preview');
  const selected = getSelectedValueSnapshot();
  if (!selected) {
    sec2.appendChild(emptyMsg('Select a Value to inspect its wire snapshot.'));
  } else {
    const compact = expandValueSnapshot(selected) || selected;
    const pre = document.createElement('pre');
    pre.className = 'value-raw';
    pre.textContent = JSON.stringify({
      valueId: compact.valueId,
      prevId: compact.prevId,
      nextId: compact.nextId,
      tokenPreview: compact.tokenPreview,
      summary: compact.summary,
      affinity: compact.affinity,
      pc: compact.pc,
      ttl: compact.ttlValue,
    }, null, 2);
    sec2.appendChild(pre);
  }
  els.body.appendChild(sec2);
}

function renderPoolInternals() {
  const sec1 = makeSection('Worker Pool');
  const stats = [
    ['Workers', state.poolWorkerCount],
    ['Idle', state.poolIdleWorkers],
    ['Queue', state.poolQueueSize],
    ['Success %', `${(state.poolSuccessRate * 100).toFixed(1)}%`],
    ['Avg latency', `${state.poolAvgLatencyMs.toFixed(1)}ms`],
    ['P95 latency', `${state.poolP95LatencyMs.toFixed(1)}ms`],
    ['P99 latency', `${state.poolP99LatencyMs.toFixed(1)}ms`],
    ['Failures', state.poolFailureCount],
  ];
  for (const [label, value] of stats) {
    const row = document.createElement('div');
    row.className = 'inspector-stat';
    const left = document.createElement('span');
    left.textContent = label;
    const right = document.createElement('span');
    right.className = 'val accent';
    right.textContent = String(value);
    row.append(left, right);
    sec1.appendChild(row);
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Recent Jobs');
  if (state.poolJobHistory.length === 0) {
    sec2.appendChild(emptyMsg('No pool jobs recorded yet.'));
  } else {
    for (const job of [...state.poolJobHistory].reverse().slice(0, 12)) {
      const row = document.createElement('div');
      row.className = 'timeline-prompt';
      const promptQ = document.createElement('div');
      promptQ.className = 'prompt-q';
      promptQ.textContent = `${job.success ? 'Job' : 'Fail'} ${job.id || '?'}`;
      const promptA = document.createElement('div');
      promptA.className = 'prompt-a';
      promptA.textContent = `${job.type || 'task'} · ${job.duration ?? 0}ms${job.error ? ` · ${job.error}` : ''}`;
      row.append(promptQ, promptA);
      sec2.appendChild(row);
    }
  }
  els.body.appendChild(sec2);
}

function renderHardwareInternals(kind) {
  const sec1 = makeSection(`${kind.toUpperCase()} Route Activity`);
  const kernelEvents = state.eventsByComponent.get('Kernel') || [];
  const hits = kernelEvents.filter(e => e.data?.selectedHardware === kind);

  if (hits.length === 0) {
    sec1.appendChild(emptyMsg(`No routes hit ${kind.toUpperCase()} yet.`));
  } else {
    const routesByIndex = {};
    for (const ev of hits) {
      const idx = ev.data?.bestIndex ?? 0;
      routesByIndex[idx] = (routesByIndex[idx] || 0) + 1;
    }
    const maxUsage = Math.max(1, ...Object.values(routesByIndex));
    for (const [idx, count] of Object.entries(routesByIndex)) {
      const row = document.createElement('div');
      row.className = 'latency-bar-row';
      const lab = document.createElement('span');
      lab.className = 'latency-label';
      lab.textContent = `#${idx}`;
      const track = document.createElement('div');
      track.className = 'latency-track';
      const fill = document.createElement('div');
      fill.className = 'latency-fill';
      fill.style.width = `${(count / maxUsage) * 100}%`;
      track.appendChild(fill);
      const val = document.createElement('span');
      val.className = 'latency-val';
      val.textContent = `${count}×`;
      row.append(lab, track, val);
      sec1.appendChild(row);
    }
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Recent Backend Activity');
  for (const ev of hits.slice(-15).reverse()) {
    const row = document.createElement('div');
    row.className = 'fold-tree-node';
    const act = document.createElement('span');
    act.className = 'fold-level';
    act.textContent = ev.action ?? '';
    const dataSpan = document.createElement('span');
    dataSpan.textContent = formatEventData(ev.data);
    const ts = document.createElement('span');
    ts.className = 'fold-density';
    ts.textContent = `${((ev._ts || 0) / 1000).toFixed(1)}s`;
    row.append(act, document.createTextNode(' '), dataSpan, ts);
    row.addEventListener('click', () => showEventDetail(ev));
    sec2.appendChild(row);
  }
  els.body.appendChild(sec2);
}

function renderKernelInternals() {
  const kernelEvents = state.eventsByComponent.get('Kernel') || [];
  const sec1 = makeSection('Backend Routing');
  const routes = kernelEvents.filter(e => e.action === 'Route');

  if (routes.length === 0) {
    sec1.appendChild(emptyMsg('No routing events yet.'));
  } else {
    const backendUsage = {};
    for (const ev of routes) {
      const idx = ev.data?.bestIndex || 0;
      backendUsage[idx] = (backendUsage[idx] || 0) + 1;
    }
    const maxUsage = Math.max(1, ...Object.values(backendUsage));
    for (const [idx, count] of Object.entries(backendUsage)) {
      const row = document.createElement('div');
      row.className = 'latency-bar-row';
      const lab = document.createElement('span');
      lab.className = 'latency-label';
      lab.textContent = `#${idx}`;
      const track = document.createElement('div');
      track.className = 'latency-track';
      const fill = document.createElement('div');
      fill.className = 'latency-fill';
      fill.style.width = `${(count / maxUsage) * 100}%`;
      track.appendChild(fill);
      const val = document.createElement('span');
      val.className = 'latency-val';
      val.textContent = `${count}×`;
      row.append(lab, track, val);
      sec1.appendChild(row);
    }
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Recent Activity');
  for (const ev of kernelEvents.slice(-15).reverse()) {
    const row = document.createElement('div');
    row.className = 'fold-tree-node';
    const act = document.createElement('span');
    act.className = 'fold-level';
    act.textContent = ev.action ?? '';
    const dataSpan = document.createElement('span');
    dataSpan.textContent = formatEventData(ev.data);
    const ts = document.createElement('span');
    ts.className = 'fold-density';
    ts.textContent = `${((ev._ts || 0) / 1000).toFixed(1)}s`;
    row.append(act, document.createTextNode(' '), dataSpan, ts);
    row.addEventListener('click', () => showEventDetail(ev));
    sec2.appendChild(row);
  }
  els.body.appendChild(sec2);
}

function renderGenericInternals() {
  const wrap = document.createElement('div');
  wrap.className = 'inspector-empty';
  wrap.style.whiteSpace = 'pre-line';
  wrap.textContent = 'This subsystem does not emit telemetry yet.\nCheck the Feed tab for events.';
  els.body.appendChild(wrap);
}

function getSelectedValueSnapshot() {
  const key = String(state.inspectorKey || '');
  if (!key) return null;
  return state.valueSnapshots.get(key)
    || [...state.valueHistory].reverse().find(v => String(v.valueId) === key)
    || null;
}

function renderValueSummaryTab() {
  els.body.innerHTML = '';
  const snapshot = getSelectedValueSnapshot();

  if (!snapshot) {
    els.body.appendChild(emptyMsg('No Value snapshot is selected yet.'));
    return;
  }

  const sec1 = makeSection('Snapshot');
  const rows = [
    ['Value ID', snapshot.valueId || '—'],
    ['Prev ID', snapshot.prevId || '0'],
    ['Next ID', snapshot.nextId || '0'],
    ['Token Text', snapshot.tokenPreview || snapshot.tokenText || '—'],
    ['Token Count', snapshot.tokenCount ?? 0],
    ['Current Op', snapshot.currentOpName || '—'],
    ['Affinity', snapshot.affinity || '—'],
    ['Affinity Pop', snapshot.affinityPop ?? 0],
    ['PC', snapshot.pc || '0'],
    ['TTL', `${snapshot.ttlValue ?? 0}`],
    ['Exec Status', snapshot.execStatusName || '—'],
    ['Program Pop', snapshot.programPop ?? 0],
  ];

  for (const [label, value] of rows) {
    const row = document.createElement('div');
    row.className = 'inspector-stat';
    const left = document.createElement('span');
    left.textContent = label;
    const right = document.createElement('span');
    right.className = 'val accent';
    right.textContent = String(value);
    row.append(left, right);
    sec1.appendChild(row);
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Registers');
  const regs = snapshot.registers || {};
  for (const [name, value] of Object.entries(regs)) {
    const row = document.createElement('div');
    row.className = 'inspector-stat';
    const left = document.createElement('span');
    left.textContent = name;
    const right = document.createElement('span');
    right.className = 'val';
    right.textContent = String(value);
    row.append(left, right);
    sec2.appendChild(row);
  }
  els.body.appendChild(sec2);

  const sec3 = makeSection('Recent Observations');
  const recent = [...state.valueHistory]
    .filter(v => String(v.valueId) === String(snapshot.valueId))
    .slice(-8)
    .reverse();
  if (recent.length === 0) {
    sec3.appendChild(emptyMsg('No recent observations for this Value yet.'));
  } else {
    for (const obs of recent) {
      const row = document.createElement('div');
      row.className = 'timeline-prompt';
      const promptQ = document.createElement('div');
      promptQ.className = 'prompt-q';
      promptQ.textContent = obs.summary || obs.tokenPreview || 'Value snapshot';
      const promptA = document.createElement('div');
      promptA.className = 'prompt-a';
      promptA.textContent = `tokens=${(obs.tokenPreview || obs.tokenText || '').slice(0, 80)} · ` +
        `pc=${obs.pc || '0'} · op=${obs.currentOpName || '—'} · ` +
        `${((obs._ts || 0) / 1000).toFixed(1)}s`;
      row.append(promptQ, promptA);
      sec3.appendChild(row);
    }
  }
  els.body.appendChild(sec3);
}

function renderValueProgramTab() {
  els.body.innerHTML = '';
  const snapshot = getSelectedValueSnapshot();

  if (!snapshot) {
    els.body.appendChild(emptyMsg('No Value snapshot is selected yet.'));
    return;
  }

  const expanded = expandValueSnapshot(snapshot);
  if (!expanded || !Array.isArray(expanded.program) || expanded.program.length === 0) {
    const sec = makeSection('Program');
    sec.appendChild(emptyMsg('This Value does not currently expose a program.'));
    els.body.appendChild(sec);
    return;
  }

  const sec1 = makeSection('Program');
  const activePc = Number.parseInt(String(expanded.pc || '0'), 10) || 0;
  const activeSlot = Number.isFinite(activePc) ? activePc : 0;

  for (const instr of expanded.program) {
    const row = document.createElement('div');
    row.className = `program-row${instr.slot === activeSlot ? ' active' : ''}`;
    const slot = document.createElement('span');
    slot.className = 'program-slot';
    slot.textContent = `#${String(instr.slot).padStart(2, '0')}`;
    const op = document.createElement('span');
    op.className = 'program-op';
    op.textContent = instr.text;
    const raw = document.createElement('span');
    raw.className = 'program-raw';
    raw.textContent = instr.raw;
    row.append(slot, op, raw);
    row.addEventListener('click', () => showEventDetail({ component: 'Value', action: 'Program', data: { valueId: expanded.valueId, instruction: instr } }));
    sec1.appendChild(row);
  }

  els.body.appendChild(sec1);
}

function renderValueRawTab() {
  els.body.innerHTML = '';
  const snapshot = getSelectedValueSnapshot();
  if (!snapshot) {
    els.body.appendChild(emptyMsg('No Value snapshot is selected yet.'));
    return;
  }

  const expanded = expandValueSnapshot(snapshot);
  const sec = makeSection('Raw Snapshot');
  const pre = document.createElement('pre');
  pre.className = 'value-raw';
  pre.textContent = JSON.stringify(expanded || snapshot, null, 2);
  sec.appendChild(pre);
  els.body.appendChild(sec);
}

// ── Helpers ──
function makeSection(titleText) {
  const sec = document.createElement('div');
  sec.className = 'inspector-section';
  const t = document.createElement('div');
  t.className = 'inspector-section-title';
  t.textContent = titleText;
  sec.appendChild(t);
  return sec;
}

function emptyMsg(text) {
  const div = document.createElement('div');
  div.className = 'inspector-empty';
  div.textContent = text;
  return div;
}

export function formatEventData(data) {
  if (!data) return '—';
  const parts = [];
  if (data.stage) parts.push(`stage=${data.stage}`);
  if (data.chunkText) parts.push(`"${data.chunkText.slice(0, 30)}"`);
  if (data.message) parts.push(data.message.slice(0, 40));
  if (data.resultText) parts.push(`→ ${data.resultText.slice(0, 40)}`);
  if (data.summary) parts.push(data.summary.slice(0, 80));
  if (data.tokenPreview) parts.push(data.tokenPreview.slice(0, 40));
  if (data.valueId) parts.push(`Value#${data.valueId}`);
  if (data.edgeCount) parts.push(`edges=${data.edgeCount}`);
  if (data.pathCount) parts.push(`paths=${data.pathCount}`);
  if (data.entryCount) parts.push(`entries=${data.entryCount}`);
  if (data.level !== undefined && data.level !== 0) parts.push(`L${data.level}`);
  if (data.bin) parts.push(`bin=${data.bin}`);
  if (data.density) parts.push(`${(data.density * 100).toFixed(0)}%`);
  if (data.currentOpName) parts.push(`op=${data.currentOpName}`);
  if (data.instruction) parts.push(data.instruction);
  if (data.dataPop !== undefined) parts.push(`data↑${data.dataPop}`);
  if (data.frameIndex !== undefined) parts.push(`#${data.frameIndex}`);
  return parts.join(' · ') || '—';
}
