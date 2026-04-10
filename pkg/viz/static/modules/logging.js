import { KIND_NAMES, kindClass } from './constants.js';

const logEl = document.getElementById('eventlog');

export function addLogEntry(ev) {
  const div = document.createElement('div');
  div.className = 'log-entry';
  const ts = new Date(ev.ts / 1000).toISOString().substr(11, 12);
  const kc = kindClass(ev.kind);
  div.innerHTML = `<span class="log-time">${ts}</span><span class="log-kind ${kc}">${KIND_NAMES[ev.kind] || ev.kind}</span><span class="log-src">${ev.src}${ev.tgt ? ` > ${ev.tgt}` : ''}</span>${ev.lbl ? ` ${ev.lbl}` : ''}`;
  if (ev.meta?.sequence) {
    const meta = document.createElement('span');
    meta.className = 'log-meta';
    meta.textContent = ev.meta.sequence;
    div.appendChild(meta);
  }
  logEl.appendChild(div);
  while (logEl.children.length > 500) logEl.removeChild(logEl.firstChild);
  logEl.scrollTop = logEl.scrollHeight;
}
