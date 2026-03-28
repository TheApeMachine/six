/* ═══════════════════════════════════════════════════════════
   recording.js — Event recording and replay engine
   ═══════════════════════════════════════════════════════════ */

export const recording = [];
let replayMode = false;
let replayIdx = 0;
let replayTimer = null;
const startTime = Date.now();

let handleEventFn = null;
let resetVisFn = null;

export function initRecording(handleEvent, resetVis) {
  handleEventFn = handleEvent;
  resetVisFn = resetVis;
}

export function recordEvent(ev) {
  const copy = { ...ev };
  if (ev.data != null && typeof ev.data === 'object' && !Array.isArray(ev.data)) {
    copy.data = { ...ev.data };
  }
  copy._ts = Date.now() - startTime;
  recording.push(copy);
}

export function isReplayMode() {
  return replayMode;
}

export function getRecordingLength() {
  return recording.length;
}

export function getReplayIdx() {
  return replayIdx;
}

export function enterReplayMode() {
  replayMode = true;
  if (resetVisFn) resetVisFn();
}

export function enterLiveMode() {
  replayMode = false;
  if (replayTimer) {
    clearTimeout(replayTimer);
    replayTimer = null;
  }
}

export function replayTo(idx) {
  if (resetVisFn) resetVisFn();
  if (recording.length === 0) {
    replayIdx = 0;
    return;
  }
  const target = Math.min(idx, recording.length - 1);
  for (let i = 0; i <= target; i++) {
    if (handleEventFn) handleEventFn(recording[i]);
  }
  replayIdx = target;
}

export function startPlayback(onTick) {
  enterReplayMode();
  if (replayTimer) {
    clearTimeout(replayTimer);
    replayTimer = null;
  }
  if (recording.length === 0) {
    replayIdx = 0;
    return;
  }

  replayIdx = -1;

  function playStep() {
    if (replayIdx >= recording.length - 1) {
      replayTimer = null;
      if (onTick) onTick(replayIdx, recording.length);
      return;
    }
    replayIdx += 1;
    if (handleEventFn) handleEventFn(recording[replayIdx]);
    if (onTick) onTick(replayIdx, recording.length);

    if (replayIdx >= recording.length - 1) {
      replayTimer = null;
      return;
    }
    const curTs = recording[replayIdx]._ts ?? 0;
    const nextTs = recording[replayIdx + 1]._ts ?? 0;
    const delay = Math.max(0, nextTs - curTs);
    replayTimer = setTimeout(playStep, delay);
  }

  replayTimer = setTimeout(playStep, 0);
}

export function pausePlayback() {
  if (replayTimer) {
    clearTimeout(replayTimer);
    replayTimer = null;
  }
}

export function stepForward() {
  if (!replayMode) enterReplayMode();
  if (replayIdx < recording.length - 1) {
    replayIdx++;
    if (handleEventFn) handleEventFn(recording[replayIdx]);
  }
  return replayIdx;
}

export function exportRecording() {
  const blob = new Blob([JSON.stringify(recording, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `six-recording-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
  try {
    a.click();
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 2500);
  }
}

export function importRecording(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const data = JSON.parse(e.target.result);
        if (!Array.isArray(data)) {
          reject(new Error('Imported data is not an array'));
          return;
        }
        recording.length = 0;
        recording.push(...data);
        replayTo(0);
        resolve(recording.length);
      } catch (err) {
        console.error('Import error:', err);
        reject(err);
      }
    };
    reader.onerror = () => reject(new Error('Failed to read import file'));
    reader.readAsText(file);
  });
}
