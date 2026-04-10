import * as THREE from 'three';
import { canvas, camera, cameraFocus } from './scene.js';
import { state } from './state.js';
import { GF_LAYER } from './constants.js';
import { escapeHtml } from './text.js';
import { getPipelinePickMeshes } from './pipeline.js';

const raycaster = new THREE.Raycaster();
const mouse = new THREE.Vector2();

/*
focusCameraOn smoothly moves the camera so the target fills ~75% of the view.
distance controls how far the camera sits from the target.
*/
function focusCameraOn(worldPos, distance) {
  cameraFocus.active = true;
  cameraFocus.lookAt.copy(worldPos);

  const dir = camera.position.clone().sub(worldPos).normalize();
  cameraFocus.target.copy(worldPos).addScaledVector(dir, distance || 18);
  cameraFocus.target.y = Math.max(cameraFocus.target.y, worldPos.y + 4);
  cameraFocus.distance = distance || 18;
}

export function openInspectorTrieVertex(kadabraId, trieIdx, graphVertexVid) {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.15;
  }
  state.selectedPipeline = null;
  state.selected = kadabraId;
  state.selectedTrieVertex = { trieIdx, graphVertexVid };
  const kn = state.nodes.get(kadabraId);
  if (kn) {
    kn.wire.material.opacity = 0.5;
    focusCameraOn(kn.group.position, 14);
  }
  refreshInspector();
  document.getElementById('inspector').classList.add('open');
}

export function selectNode(id) {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.15;
  }
  state.selectedPipeline = null;
  state.selectedTrieVertex = null;
  state.selected = id;
  const node = state.nodes.get(id);
  if (node) {
    node.wire.material.opacity = 0.5;
    focusCameraOn(node.group.position, 16);
  }
  refreshInspector();
  document.getElementById('inspector').classList.add('open');
}

export function selectPipelineStage(stageId) {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.15;
  }
  state.selected = null;
  state.selectedTrieVertex = null;
  state.selectedPipeline = stageId;
  const stage = state.pipeline.stages.get(stageId);
  if (stage) {
    focusCameraOn(stage.group.position, 12);
  }
  refreshInspector();
  document.getElementById('inspector').classList.add('open');
}

export function closeInspector() {
  if (state.selected) {
    const prev = state.nodes.get(state.selected);
    if (prev) prev.wire.material.opacity = 0.15;
  }
  state.selected = null;
  state.selectedTrieVertex = null;
  state.selectedPipeline = null;
  document.getElementById('inspector').classList.remove('open');
}

export function refreshInspector() {
  if (state.selectedPipeline) {
    refreshPipelineInspector();
    return;
  }

  const node = state.nodes.get(state.selected);
  if (!node) return;

  document.getElementById('inspector-title').textContent = node.data.label;
  const body = document.getElementById('inspector-body');
  let html = '';
  const d = node.data;

  html += '<div class="insp-section"><h4>finite-field layers</h4>';
  html += `<div class="insp-row"><span class="insp-key" style="color:${GF_LAYER.global.hexCss}">DHT mesh</span><span class="insp-val">${GF_LAYER.global.label}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key" style="color:${GF_LAYER.node.hexCss}">this node</span><span class="insp-val">${GF_LAYER.node.label}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key" style="color:${GF_LAYER.trie.hexCss}">tries</span><span class="insp-val">${GF_LAYER.trie.label} \u2014 per-column only (trie-local phase)</span></div>`;
  html += '<div class="insp-row" style="opacity:0.75;font-size:10px"><span class="insp-key">note</span><span class="insp-val">column count follows trie_count \u00b7 arrows = phase reference per layer</span></div>';
  html += '<div class="insp-row" style="opacity:0.75;font-size:10px"><span class="insp-key">phase arrow</span><span class="insp-val">+X ref, \u2192 +Z (CCW)</span></div>';
  html += '</div>';

  if (state.selectedTrieVertex) {
    const tv = state.selectedTrieVertex;
    const trieSlot = node.tries[tv.trieIdx];
    const vn = trieSlot?.graphNodeByVid?.get(tv.graphVertexVid);
    html += '<div class="insp-section"><h4>selected trie vertex</h4>';
    if (vn) {
      html += `<div class="insp-row"><span class="insp-key">column</span><span class="insp-val">T${tv.trieIdx}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">vid</span><span class="insp-val">${vn.vid}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">depth</span><span class="insp-val">${vn.depth}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">visits</span><span class="insp-val">${vn.visits}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">token</span><span class="insp-val" style="word-break:break-all">${escapeHtml(vn.token || '\u2014')}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">value id</span><span class="insp-val">${vn.value_id}</span></div>`;
      const pl = trieSlot.graphPayload;
      if (pl?.edges) {
        const incoming = pl.edges.filter((ed) => ed.to === tv.graphVertexVid);
        const outgoing = pl.edges.filter((ed) => ed.from === tv.graphVertexVid);
        if (incoming.length) {
          html += '<div class="insp-row"><span class="insp-key">incoming</span></div>';
          for (const ed of incoming) {
            html += `<div class="insp-sequence" style="font-size:11px">\u2190 ${escapeHtml(ed.token)} (parent ${ed.from})</div>`;
          }
        }
        if (outgoing.length) {
          html += '<div class="insp-row"><span class="insp-key">outgoing</span></div>';
          for (const ed of outgoing) {
            html += `<div class="insp-sequence" style="font-size:11px">\u2192 ${escapeHtml(ed.token)} (child ${ed.to})</div>`;
          }
        }
      }
      if (pl?.truncated) {
        html += '<div style="color:#c08030;font-size:11px;margin-top:6px">Snapshot truncated server-side; not all trie vertices are streamed.</div>';
      }
    } else {
      html += '<div style="color:#3a5878;font-size:12px">Waiting for trie graph snapshot (T'
        + `${tv.trieIdx}, vid ${tv.graphVertexVid})\u2026</div>`;
    }
    html += '</div>';
  }

  html += '<div class="insp-section"><h4>field digest</h4>';
  for (const key of ['surprisal', 'entropy', 'growth']) {
    const v = d.digest[key];
    if (v === undefined) continue;
    const pct = Math.min(Math.abs(v) / 10, 1) * 100;
    const barColor = key === 'surprisal' ? (v > 5 ? '#c04040' : v > 2 ? '#c08030' : '#408060') : '#4a80c0';
    html += `<div class="insp-row"><span class="insp-key">${key}</span><span class="insp-val">${v.toFixed(4)}</span></div>`;
    html += `<div class="insp-bar"><div class="insp-bar-fill" style="width:${pct}%;background:${barColor}"></div></div>`;
  }
  html += '</div>';

  if (Object.keys(d.pressure).length) {
    html += '<div class="insp-section"><h4>field pressure</h4>';
    for (const [k, v] of Object.entries(d.pressure)) {
      html += `<div class="insp-row"><span class="insp-key">${k}</span><span class="insp-val">${typeof v === 'number' ? v.toFixed(6) : v}</span></div>`;
    }
    html += '</div>';
  }

  html += '<div class="insp-section"><h4>activity</h4>';
  html += `<div class="insp-row"><span class="insp-key">inserts</span><span class="insp-val">${d.insertCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">predictions</span><span class="insp-val">${d.predictCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">gossip</span><span class="insp-val">${d.gossipCount}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">tries</span><span class="insp-val">${d.trieCount}</span></div>`;
  html += '</div>';

  const latencyEntries = Object.entries(d.latencies);
  if (latencyEntries.length) {
    html += '<div class="insp-section"><h4>peer latencies</h4>';
    for (const [peerId, ms] of latencyEntries) {
      const color = ms > 50 ? '#c04040' : ms > 10 ? '#c08030' : '#408060';
      html += `<div class="insp-row"><span class="insp-key">${peerId.substring(0, 16)}</span><span class="insp-val" style="color:${color}">${ms.toFixed(1)}ms</span></div>`;
    }
    html += '</div>';
  }

  const labels = Object.entries(d.labelCounts).sort((a, b) => b[1] - a[1]);
  if (labels.length) {
    html += '<div class="insp-section"><h4>label distribution</h4>';
    const total = labels.reduce((s, [, v]) => s + v, 0);
    for (const [label, count] of labels) {
      const pct = (count / total * 100).toFixed(1);
      html += `<div class="insp-row"><span class="insp-key">${label}</span><span class="insp-val">${count} (${pct}%)</span></div>`;
      html += `<div class="insp-bar"><div class="insp-bar-fill" style="width:${pct}%;background:#7050a0"></div></div>`;
    }
    html += '</div>';
  }

  if (d.recentSequences.length) {
    html += '<div class="insp-section"><h4>recent sequences</h4>';
    for (const seq of [...d.recentSequences].reverse().slice(0, 8)) {
      html += `<div class="insp-sequence">${escapeHtml(seq)}</div>`;
    }
    html += '</div>';
  }

  if (node.trieSignals.length > 0 || node.trieModes.length > 0) {
    html += '<div class="insp-section"><h4>trie field state</h4>';
    const maxIdx = Math.max(node.trieSignals.length, node.trieModes.length, node.triePressures.length);
    for (let ti = 0; ti < maxIdx; ti++) {
      const sig = node.trieSignals[ti];
      const mode = node.trieModes[ti];
      const pres = node.triePressures[ti];
      const modeLabel = mode ? (mode.aligned ? '<span style="color:#c08030">aligned</span>' : '<span style="color:#3a5878">misaligned</span>') : '\u2014';
      html += '<div style="margin-bottom:4px;padding:3px 0;border-bottom:1px solid rgba(58,96,144,0.1);">';
      html += `<div class="insp-row"><span class="insp-key">T${ti}</span><span class="insp-val">${modeLabel}</span></div>`;
      if (sig) {
        const sColor = sig.surprisal > 5 ? '#c04040' : sig.surprisal > 2 ? '#c08030' : '#408060';
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">surprisal</span><span class="insp-val" style="color:${sColor}">${sig.surprisal.toFixed(4)}</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">entropy</span><span class="insp-val">${sig.entropy.toFixed(4)}</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">growth</span><span class="insp-val" style="color:${sig.growth > 0 ? '#408060' : '#c04060'}">${sig.growth >= 0 ? '+' : ''}${sig.growth.toFixed(4)}</span></div>`;
      }
      if (pres) {
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">decay</span><span class="insp-val" style="color:#c04060">${pres.decay.toFixed(6)} (x${pres.decayMul.toFixed(2)})</span></div>`;
        html += `<div class="insp-row"><span class="insp-key" style="padding-left:8px">learn</span><span class="insp-val" style="color:#408060">${pres.learn.toFixed(6)} (x${pres.learnMul.toFixed(2)})</span></div>`;
      }
      html += '</div>';
    }
    html += '</div>';
  }

  const bm = node.beam;
  if (bm.lastCompose > 0 || bm.activeCount > 0) {
    html += '<div class="insp-section"><h4>beam search</h4>';
    html += `<div class="insp-row"><span class="insp-key">active hypotheses</span><span class="insp-val" style="color:#408060">${bm.activeCount}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">last rejected</span><span class="insp-val" style="color:#c04040">${bm.rejectedCount}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">best score</span><span class="insp-val" style="color:#c08030">${bm.bestScore.toFixed(4)}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">collection rays</span><span class="insp-val">${bm.rays.length}</span></div>`;
    html += `<div class="insp-row"><span class="insp-key">orbiting hyps</span><span class="insp-val">${bm.hypotheses.length}</span></div>`;
    html += '</div>';
  }

  html += `<div class="insp-section"><h4>peers (${node.edges.size})</h4>`;
  for (const eid of node.edges) {
    const e = state.edges.get(eid);
    if (e) {
      const peer = e.from === state.selected ? e.to : e.from;
      const details = [];
      if (e.latencyMs > 0) details.push(`${e.latencyMs.toFixed(1)}ms`);
      if (e.gossipCount > 0) details.push(`gossip:${e.gossipCount}`);
      if (e.replicationCount > 0) details.push(`repl:${e.replicationCount}`);
      html += `<div class="insp-row"><span class="insp-key">${peer.substring(0, 16)}</span><span class="insp-val">${details.join(' ')}</span></div>`;
    }
  }
  html += '</div>';

  body.innerHTML = html;
}

function refreshPipelineInspector() {
  const stageId = state.selectedPipeline;
  const stage = state.pipeline.stages.get(stageId);
  if (!stage) return;

  document.getElementById('inspector-title').textContent = stage.def.label;
  const body = document.getElementById('inspector-body');
  const m = stage.metrics;
  const hexCss = `#${stage.def.color.toString(16).padStart(6, '0')}`;

  let html = '';

  html += '<div class="insp-section"><h4>overview</h4>';
  html += `<div class="insp-row"><span class="insp-key">component</span><span class="insp-val" style="color:${hexCss}">${stage.def.label}</span></div>`;
  html += `<div class="insp-row"><span class="insp-key">total events</span><span class="insp-val">${m.totalEvents}</span></div>`;
  if (m.bytesProcessed > 0) {
    html += `<div class="insp-row"><span class="insp-key">bytes processed</span><span class="insp-val">${(m.bytesProcessed / 1024).toFixed(1)} KiB</span></div>`;
  }
  if (m.inflight > 0) {
    html += `<div class="insp-row"><span class="insp-key">inflight</span><span class="insp-val">${m.inflight}</span></div>`;
  }
  if (m.emaDurationMs > 0) {
    html += `<div class="insp-row"><span class="insp-key">EMA duration</span><span class="insp-val">${m.emaDurationMs.toFixed(2)}ms</span></div>`;
  }
  if (m.lastDurationMs > 0) {
    html += `<div class="insp-row"><span class="insp-key">last duration</span><span class="insp-val">${m.lastDurationMs.toFixed(2)}ms</span></div>`;
  }
  html += '</div>';

  if (m.recentOps.length) {
    html += '<div class="insp-section"><h4>recent operations</h4>';
    for (const op of [...m.recentOps].reverse()) {
      html += `<div class="insp-sequence">${escapeHtml(op)}</div>`;
    }
    html += '</div>';
  }

  const c = state.compute;
  if (stageId === 'cpu' || stageId === 'metal' || stageId === 'cuda') {
    const subName = stageId;
    const sub = c.substrates[subName];
    if (sub) {
      html += '<div class="insp-section"><h4>substrate</h4>';
      html += `<div class="insp-row"><span class="insp-key">dispatches</span><span class="insp-val">${sub.totalDispatches}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">inflight</span><span class="insp-val">${sub.inflight}</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">last</span><span class="insp-val">${sub.lastDurationMs}ms</span></div>`;
      html += `<div class="insp-row"><span class="insp-key">ema</span><span class="insp-val">${sub.emaDurationMs.toFixed(1)}ms</span></div>`;
      const barPct = Math.min(sub.inflight / 8, 1) * 100;
      html += `<div class="insp-bar"><div class="insp-bar-fill" style="width:${barPct}%;background:${hexCss}"></div></div>`;
      html += '</div>';
    }
  }

  if (stageId === 'backend' || stageId === 'queue') {
    const substrates = Object.entries(c.substrates);
    if (substrates.length) {
      html += '<div class="insp-section"><h4>compute substrates</h4>';
      for (const [name, s] of substrates) {
        html += `<div class="insp-row"><span class="insp-key">${name}</span><span class="insp-val">disp:${s.totalDispatches} inf:${s.inflight} ema:${s.emaDurationMs.toFixed(1)}ms</span></div>`;
      }
      html += `<div class="insp-row"><span class="insp-key">total</span><span class="insp-val">${c.totalDispatches}</span></div>`;
      html += '</div>';
    }
  }

  body.innerHTML = html;
}

export function initInspectorPick() {
  canvas.addEventListener('click', (e) => {
    mouse.x = (e.clientX / window.innerWidth) * 2 - 1;
    mouse.y = -(e.clientY / window.innerHeight) * 2 + 1;
    raycaster.setFromCamera(mouse, camera);

    const pickMeshes = [];

    for (const n of state.nodes.values()) {
      pickMeshes.push(n.core);
      if (n.face) pickMeshes.push(n.face);
      for (const trie of n.tries) {
        if (trie.pickMeshes?.length) pickMeshes.push(...trie.pickMeshes);
      }
    }

    pickMeshes.push(...getPipelinePickMeshes());

    const hits = raycaster.intersectObjects(pickMeshes);
    if (hits.length > 0) {
      const hit = hits[0].object;
      if (hit.userData.kind === 'trieVertex') {
        openInspectorTrieVertex(hit.userData.kadabraNodeId, hit.userData.trieIdx, hit.userData.graphVertexVid);
        return;
      }
      if (hit.userData.kind === 'pipeline') {
        selectPipelineStage(hit.userData.id);
        return;
      }
      if (hit.userData.id) {
        selectNode(hit.userData.id);
      }
      return;
    }
    closeInspector();
  });
}
