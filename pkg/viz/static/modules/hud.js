import { state } from './state.js';

const computePanel = document.createElement('div');
computePanel.id = 'compute-panel';
computePanel.style.cssText = 'position:fixed;bottom:36px;left:0;width:320px;background:rgba(10,14,20,0.92);border-right:1px solid rgba(58,96,144,0.15);border-top:1px solid rgba(58,96,144,0.15);backdrop-filter:blur(12px);padding:10px 14px;font-size:11px;z-index:10;pointer-events:auto;';
document.getElementById('hud').appendChild(computePanel);

export function renderComputePanel() {
  const c = state.compute;
  let html = '<div style="font-size:13px;font-weight:700;color:#806030;margin-bottom:6px;">Compute Backend</div>';

  const substrates = Object.entries(c.substrates);
  if (substrates.length === 0) {
    html += '<div style="color:#203858;">no dispatches yet</div>';
  } else {
    for (const [name, s] of substrates) {
      const barPct = Math.min(s.inflight / 8, 1) * 100;
      const color = name === 'cuda' ? '#608030' : name === 'metal' ? '#7050a0' : '#4a80c0';
      html += '<div style="margin-bottom:6px;">';
      html += `<div style="display:flex;justify-content:space-between;"><span style="color:${color};font-weight:600;text-transform:uppercase;">${name}</span><span style="color:#3a5878;">dispatches: <b style="color:#6090c0;">${s.totalDispatches}</b></span></div>`;
      html += '<div style="display:flex;gap:12px;color:#3a5878;">';
      html += `<span>inflight: <b style="color:#6090c0;">${s.inflight}</b></span>`;
      html += `<span>last: <b style="color:#6090c0;">${s.lastDurationMs}ms</b></span>`;
      html += `<span>ema: <b style="color:#6090c0;">${s.emaDurationMs.toFixed(1)}ms</b></span>`;
      html += '</div>';
      html += `<div style="height:3px;background:rgba(16,24,40,0.8);border-radius:2px;margin-top:2px;"><div style="height:100%;width:${barPct}%;background:${color};border-radius:2px;"></div></div>`;
      html += '</div>';
    }
  }

  html += `<div style="color:#203858;margin-top:4px;">total: ${c.totalDispatches}</div>`;

  if (c.recentActions.length) {
    html += '<div style="margin-top:6px;border-top:1px solid rgba(58,96,144,0.15);padding-top:4px;color:#3a5878;font-size:10px;">';
    for (const a of c.recentActions.slice(-10)) {
      html += `<div>${a}</div>`;
    }
    html += '</div>';
  }

  computePanel.innerHTML = html;
}

renderComputePanel();

const throughputCanvas = document.createElement('canvas');
throughputCanvas.width = 240;
throughputCanvas.height = 40;
throughputCanvas.style.cssText = 'position:fixed;top:40px;right:390px;z-index:10;pointer-events:none;opacity:0.8;';
document.getElementById('hud').appendChild(throughputCanvas);

function renderThroughputChart() {
  const ctx = throughputCanvas.getContext('2d');
  const w = throughputCanvas.width;
  const h = throughputCanvas.height;
  const tp = state.throughput;

  ctx.clearRect(0, 0, w, h);
  ctx.fillStyle = 'rgba(10,14,20,0.7)';
  ctx.fillRect(0, 0, w, h);

  let max = 1;
  for (let i = 0; i < 120; i++) {
    if (tp.buckets[i] > max) max = tp.buckets[i];
  }

  ctx.beginPath();
  ctx.strokeStyle = '#3a6090';
  ctx.lineWidth = 1;
  for (let i = 0; i < 120; i++) {
    const bi = (tp.idx + 1 + i) % 120;
    const x = (i / 119) * w;
    const y = h - (tp.buckets[bi] / max) * (h - 4);
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  }
  ctx.stroke();

  ctx.lineTo(w, h);
  ctx.lineTo(0, h);
  ctx.closePath();
  ctx.fillStyle = 'rgba(58,96,144,0.08)';
  ctx.fill();

  ctx.font = '9px monospace';
  ctx.fillStyle = '#203858';
  ctx.fillText(`${tp.buckets[tp.idx] | 0} evt/s`, 4, 10);
}

export function tickThroughput() {
  const now = Math.floor(Date.now() / 1000);
  const tp = state.throughput;

  if (now !== tp.lastSec) {
    tp.idx = (tp.idx + 1) % 120;
    tp.buckets[tp.idx] = tp.countThisSec;
    tp.countThisSec = 0;
    tp.lastSec = now;
    renderThroughputChart();
  }

  tp.countThisSec++;
}
