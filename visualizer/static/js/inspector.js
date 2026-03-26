/* ═══════════════════════════════════════════════════════════
   inspector.js — Inspector panel (click-to-inspect zones)
   ═══════════════════════════════════════════════════════════ */
import * as state from './state.js';
import { SYS } from './architecture.js';

let inspectorRefreshTimer = null;

const els = {};

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
    els.tabs.querySelectorAll('.inspector-tab').forEach(
      t => {t.classList.toggle('active', t.dataset.tab === state.inspectorTab)}
    );
    renderInspector();
  });

  els.detailClose.addEventListener('click', closeEventDetail);
  els.detailOverlay.addEventListener('click', (e) => {
    if (e.target === els.detailOverlay) closeEventDetail();
  });
}

export function openInspector(sysKey) {
  state.set('inspectorSysKey', sysKey);
  state.set('inspectorOpen', true);
  state.set('inspectorTab', 'feed');

  els.panel.classList.add('open');
  els.title.textContent = SYS[sysKey]?.label || sysKey;
  els.desc.textContent = state.ZONE_DESCRIPTIONS[sysKey] || '';

  els.tabs.querySelectorAll('.inspector-tab').forEach(
    t => {t.classList.toggle('active', t.dataset.tab === 'feed')}
  );

  renderInspector();
}

export function closeInspector() {
  state.set('inspectorOpen', false);
  state.set('inspectorSysKey', null);
  els.panel.classList.remove('open');
}

export function refreshInspectorIfMatch(sysKey) {
  if (!state.inspectorOpen || state.inspectorSysKey !== sysKey) return;
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

// ── Render ──
function renderInspector() {
  if (!state.inspectorOpen || !state.inspectorSysKey) return;
  if (state.inspectorTab === 'feed') renderFeedTab();
  else if (state.inspectorTab === 'state') renderStateTab();
  else if (state.inspectorTab === 'internals') renderInternalsTab();
}

function renderFeedTab() {
  const components = state.ZONE_COMPONENTS[state.inspectorSysKey] || [];
  const events = [];
  for (const comp of components) {
    events.push(...(state.eventsByComponent.get(comp) || []));
  }
  events.sort((a, b) => (b._ts || 0) - (a._ts || 0));

  els.body.innerHTML = '';

  if (events.length === 0) {
    els.body.innerHTML = `<div class="inspector-empty">
      No events for this zone yet.<br><br>
      Run the live demo (<code style="opacity:.8">go run . viz</code>) to stream telemetry.
    </div>`;
    return;
  }

  const countDiv = document.createElement('div');
  countDiv.className = 'inspector-section-title';
  countDiv.textContent = `${events.length} events`;
  els.body.appendChild(countDiv);

  for (const ev of events.slice(0, 80)) {
    const div = document.createElement('div');
    div.className = 'inspector-event';
    div.innerHTML = `
      <div class="inspector-event-header">
        <span class="inspector-event-comp">${ev.component}</span>
        <span class="inspector-event-action">${ev.action}</span>
        <span class="inspector-event-ts">${((ev._ts || 0) / 1000).toFixed(1)}s</span>
      </div>
      <div class="inspector-event-data">${formatEventData(ev.data)}</div>
    `;
    div.addEventListener('click', () => showEventDetail(ev));
    els.body.appendChild(div);
  }
}

function renderStateTab() {
  els.body.innerHTML = '';
  const components = state.ZONE_COMPONENTS[state.inspectorSysKey] || [];
  const events = [];
  for (const comp of components) events.push(...(state.eventsByComponent.get(comp) || []));

  const section = makeSection('Accumulated State');
  const stats = computeSubsystemStats(state.inspectorSysKey, events);
  for (const [key, val] of stats) {
    const row = document.createElement('div');
    row.className = 'inspector-stat';
    row.innerHTML = `<span>${key}</span><span class="val ${val.cls || ''}">${val.text}</span>`;
    section.appendChild(row);
  }
  els.body.appendChild(section);
}

function renderInternalsTab() {
  els.body.innerHTML = '';
  switch (state.inspectorSysKey) {
    case 'dataset': renderForestInternals(); break;
    case 'chamber': renderGraphInternals(); break;
    case 'machine': renderMachineInternals(); break;
    case 'frame':   renderTokenizerInternals(); break;
    case 'kernel':  renderKernelInternals(); break;
    default:        renderGenericInternals(); break;
  }
}

function computeSubsystemStats(sysKey, events) {
  const stats = [];
  switch (sysKey) {
    case 'dataset':
      stats.push(['Frames (substrate)', { text: state.substrateFrames.toLocaleString(), cls: 'accent' }]);
      stats.push(['LSM inserts', { text: (state.eventsByComponent.get('LSM') || []).length.toLocaleString() }]);
      break;
    case 'frame':
      stats.push(['Frames', { text: state.substrateFrames.toLocaleString(), cls: 'accent' }]);
      stats.push(['Token nodes', { text: state.totalTokenNodes.toLocaleString() }]);
      break;
    case 'chamber':
      stats.push(['Substrate events', { text: (state.eventsByComponent.get('Substrate') || []).length.toLocaleString() }]);
      stats.push(['Folds', { text: state.totalFolds.toLocaleString() }]);
      stats.push(['Paths', { text: state.totalPaths.toLocaleString() }]);
      break;
    case 'machine':
      stats.push(['Substrate frames', { text: state.substrateFrames.toLocaleString(), cls: 'accent' }]);
      stats.push(['Prompts (UI)', { text: state.promptHistory.length.toLocaleString() }]);
      stats.push(['Executions', { text: state.executeHistory.length.toLocaleString() }]);
      break;
    case 'kernel': {
      const kernelEvents = state.eventsByComponent.get('Kernel') || [];
      stats.push(['CPU kernel events', { text: kernelEvents.length.toLocaleString() }]);
      stats.push(['Pool jobs done', { text: state.totalJobsDone.toLocaleString() }]);
      const routes = kernelEvents.filter(e => e.action === 'Route').length;
      if (routes > 0) stats.push(['GPU routes', { text: routes.toLocaleString() }]);
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
      node.innerHTML = `<span class="fold-level">L${fold.level}</span> bin=${fold.bin} ch=${fold.children} ` +
        `${fold.text ? `"${fold.text.slice(0, 24)}"` : ''}` +
        `<span class="fold-density">${(fold.density * 100).toFixed(0)}%</span>`;
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
      div.innerHTML = `<div class="prompt-q">Q: ${p.prompt.slice(0, 60)}</div>`;
      if (p.result) div.innerHTML += `<div class="prompt-a">A: ${p.result.slice(0, 80)}</div>`;
      else if (p.error) div.innerHTML += `<div class="prompt-err">Error: ${p.error.slice(0, 60)}</div>`;
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
      header.innerHTML = `<div class="timeline-dot ${dotCls}"></div>` +
        `<span class="timeline-label">${exec.candidates || 0} candidates</span>` +
        `<span class="timeline-detail">${exec.outcome || 'running'}</span>`;
      div.appendChild(header);
      sec2.appendChild(div);
    }
  }
  els.body.appendChild(sec2);
}

function renderTokenizerInternals() {
  const sec1 = makeSection('Token Bin Distribution');
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
      row.innerHTML = `<span class="latency-label">#${idx}</span>` +
        `<div class="latency-track"><div class="latency-fill" style="width:${(count / maxUsage) * 100}%"></div></div>` +
        `<span class="latency-val">${count}×</span>`;
      sec1.appendChild(row);
    }
  }
  els.body.appendChild(sec1);

  const sec2 = makeSection('Recent Activity');
  for (const ev of kernelEvents.slice(-15).reverse()) {
    const row = document.createElement('div');
    row.className = 'fold-tree-node';
    row.innerHTML = `<span class="fold-level">${ev.action}</span> ` +
      `${formatEventData(ev.data)}` +
      `<span class="fold-density">${((ev._ts || 0) / 1000).toFixed(1)}s</span>`;
    row.addEventListener('click', () => showEventDetail(ev));
    sec2.appendChild(row);
  }
  els.body.appendChild(sec2);
}

function renderGenericInternals() {
  els.body.appendChild(emptyMsg(`This subsystem does not emit telemetry yet.<br>Check the Feed tab for events.`));
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
  div.innerHTML = text;
  return div;
}

export function formatEventData(data) {
  if (!data) return '—';
  const parts = [];
  if (data.stage) parts.push(`stage=${data.stage}`);
  if (data.chunkText) parts.push(`"${data.chunkText.slice(0, 30)}"`);
  if (data.message) parts.push(data.message.slice(0, 40));
  if (data.edgeCount) parts.push(`edges=${data.edgeCount}`);
  if (data.pathCount) parts.push(`paths=${data.pathCount}`);
  if (data.entryCount) parts.push(`entries=${data.entryCount}`);
  if (data.level !== undefined && data.level !== 0) parts.push(`L${data.level}`);
  if (data.bin) parts.push(`bin=${data.bin}`);
  if (data.density) parts.push(`${(data.density * 100).toFixed(0)}%`);
  if (data.instruction) parts.push(data.instruction);
  if (data.dataPop !== undefined) parts.push(`data↑${data.dataPop}`);
  if (data.frameIndex !== undefined) parts.push(`#${data.frameIndex}`);
  return parts.join(' · ') || '—';
}
