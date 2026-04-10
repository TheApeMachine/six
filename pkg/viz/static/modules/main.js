import { GF_LAYER, PIPELINE_STAGES } from './constants.js';
import { state } from './state.js';
import { clearScene } from './lifecycle.js';
import { scrubTo, initTimelineBar } from './timeline.js';
import { replayEvent } from './events_pipeline.js';
import { closeInspector, initInspectorPick } from './inspector.js';
import { startSparklineTicker } from './nodes.js';
import { connect } from './ws.js';
import { animate } from './animate.js';
import { camera, renderer } from './scene.js';
import { initPipeline, advancePipelineSparklines } from './pipeline.js';

const logEl = document.getElementById('eventlog');

function installGFLayerLegend() {
  const topbar = document.getElementById('topbar');
  if (!topbar) return;

  const legendRow = document.createElement('div');
  legendRow.style.cssText = 'display:flex;align-items:center;gap:10px;font-size:10px;color:#6878a0;margin-left:6px;flex-wrap:wrap;';
  const dotLabel = (layer) => {
    const span = document.createElement('span');
    span.style.whiteSpace = 'nowrap';
    const dot = document.createElement('span');
    dot.textContent = '●';
    dot.style.color = layer.hexCss;
    dot.style.marginRight = '4px';
    const text = document.createElement('span');
    text.textContent = layer.label;
    span.appendChild(dot);
    span.appendChild(text);
    return span;
  };
  legendRow.appendChild(dotLabel(GF_LAYER.global));
  legendRow.appendChild(dotLabel(GF_LAYER.node));
  legendRow.appendChild(dotLabel(GF_LAYER.trie));

  const sep = document.createElement('span');
  sep.textContent = '│';
  sep.style.color = '#3a4868';
  legendRow.appendChild(sep);

  for (const stage of PIPELINE_STAGES) {
    legendRow.appendChild(dotLabel(stage));
  }

  topbar.appendChild(legendRow);
}

installGFLayerLegend();

document.getElementById('btn-pause').addEventListener('click', function onPause() {
  state.paused = !state.paused;
  this.textContent = state.paused ? 'resume' : 'pause';
  this.classList.toggle('active', state.paused);
  if (!state.paused) {
    state.scrubPos = -1;
    clearScene();
    for (const ev of state.events) replayEvent(ev);
    state.statsDirty = true;
  }
});

document.getElementById('btn-log').addEventListener('click', function onLog() {
  logEl.classList.toggle('open');
  this.classList.toggle('active');
});

document.getElementById('btn-prompt').addEventListener('click', function onPrompt() {
  const panel = document.getElementById('prompt-panel');
  panel.classList.toggle('open');
  this.classList.toggle('active');
  if (panel.classList.contains('open')) document.getElementById('prompt-input').focus();
});

document.getElementById('btn-close-inspector').addEventListener('click', closeInspector);

document.getElementById('btn-tl-start').addEventListener('click', () => {
  if (state.paused) scrubTo(0);
});

document.getElementById('btn-tl-end').addEventListener('click', () => {
  if (state.paused) scrubTo(state.events.length);
});

document.getElementById('btn-save').addEventListener('click', () => {
  const anchor = document.createElement('a');
  anchor.href = URL.createObjectURL(new Blob([JSON.stringify(state.events)], { type: 'application/json' }));
  anchor.download = `six-viz-${Date.now()}.json`;
  anchor.click();
});

document.getElementById('btn-load').addEventListener('click', () => {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.json';
  input.onchange = async (e) => {
    const events = JSON.parse(await e.target.files[0].text());
    state.events = events;
    state.eventCount = events.length;
    state.paused = true;
    document.getElementById('btn-pause').textContent = 'resume';
    document.getElementById('btn-pause').classList.add('active');
    scrubTo(events.length);
  };
  input.click();
});

document.getElementById('prompt-input').addEventListener('keydown', async (e) => {
  if (e.key !== 'Enter') return;
  const prompt = e.target.value.trim();
  if (!prompt) return;
  e.target.value = '';
  const resultEl = document.getElementById('prompt-result');
  resultEl.textContent = 'Sending...';
  try {
    const resp = await (
      await fetch('/api/prompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt }),
      })
    ).json();
    let text = `Generation: ${resp.generation || '(empty)'}\n\nClassification:\n`;
    if (resp.classification) for (const [k, v] of Object.entries(resp.classification)) text += `  ${k}: ${v.toFixed(2)}%\n`;
    resultEl.textContent = text;
  } catch (err) {
    resultEl.textContent = `Error: ${err.message}`;
  }
});

initTimelineBar();
initInspectorPick();
initPipeline();
startSparklineTicker();
setInterval(advancePipelineSparklines, 500);
connect();
requestAnimationFrame(animate);

window.addEventListener('resize', () => {
  camera.aspect = window.innerWidth / window.innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(window.innerWidth, window.innerHeight);
});

document.addEventListener('keydown', (e) => {
  if (e.target.tagName === 'INPUT') return;
  switch (e.key) {
    case ' ':
      e.preventDefault();
      document.getElementById('btn-pause').click();
      break;
    case 'l':
      document.getElementById('btn-log').click();
      break;
    case 'p':
      document.getElementById('btn-prompt').click();
      break;
    case 'Escape':
      closeInspector();
      document.getElementById('prompt-panel').classList.remove('open');
      break;
    case 'ArrowLeft':
      if (state.paused && state.scrubPos > 0) scrubTo(state.scrubPos - 1);
      break;
    case 'ArrowRight':
      if (state.paused) scrubTo((state.scrubPos >= 0 ? state.scrubPos : state.events.length) + 1);
      break;
    default:
      break;
  }
});
