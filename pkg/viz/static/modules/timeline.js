import { state } from './state.js';
import { clearScene } from './lifecycle.js';
import { replayEvent } from './events_pipeline.js';

const timelineBar = document.getElementById('timeline-bar');
const timelineFill = document.getElementById('timeline-fill');
const timelineCursor = document.getElementById('timeline-cursor');
const timelineLabel = document.getElementById('timeline-label');

export function updateTimeline() {
  const total = state.events.length;
  const pos = state.scrubPos >= 0 ? state.scrubPos : total;
  const pct = total > 0 ? (pos / total) * 100 : 0;
  timelineFill.style.width = `${pct}%`;
  timelineCursor.style.left = `${pct}%`;
  timelineLabel.textContent = `${pos} / ${total}`;
}

export function scrubTo(pos) {
  let p = pos;
  p = Math.max(0, Math.min(p, state.events.length));
  state.scrubPos = p;
  clearScene();
  for (let i = 0; i < p; i++) replayEvent(state.events[i]);
  updateTimeline();
  state.statsDirty = true;
}

export function initTimelineBar() {
  timelineBar.addEventListener('click', (e) => {
    if (!state.paused) return;
    const rect = timelineBar.getBoundingClientRect();
    scrubTo(Math.floor(((e.clientX - rect.left) / rect.width) * state.events.length));
  });
}
