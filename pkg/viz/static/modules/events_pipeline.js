import { state } from './state.js';
import { EK, MAX_VIZ_TRIE_VISUALS } from './constants.js';
import { createNode } from './nodes.js';
import { addTrieVisual } from './tries.js';
import { applyEvent } from './events_apply.js';
import { addLogEntry } from './logging.js';
import { tickThroughput } from './hud.js';

export function normalizeVizEvent(ev) {
  if (!ev || typeof ev !== 'object' || ev.action !== undefined) return;
  ev.kind = Number(ev.kind);
}

export function ensureTopologyForEvent(ev) {
  if (ev == null || ev.kind === undefined) return;
  if (ev.kind === EK.NodeCreated || ev.kind === EK.NodeRemoved) return;

  const stub = (id) => {
    if (typeof id !== 'string' || !id.startsWith('node_')) return;
    if (!state.nodes.has(id)) createNode(id, id);
  };

  stub(ev.src);
  stub(ev.tgt);
}

export function ensureTrieCapacityForEvent(ev) {
  if (ev == null || ev.kind === undefined) return;

  const nodeId = ev.src;
  if (typeof nodeId !== 'string' || !nodeId.startsWith('node_')) return;

  let maxIdx = -1;
  switch (ev.kind) {
    case EK.TrieMode:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    case EK.TriePressure:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    case EK.TrieSignal:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    case EK.TrieGraphSnapshot:
      maxIdx = ev.vals?.trie_idx | 0;
      break;
    default:
      return;
  }

  if (maxIdx < 0) return;

  maxIdx = Math.min(maxIdx, MAX_VIZ_TRIE_VISUALS - 1);

  const node = state.nodes.get(nodeId);
  if (!node) return;

  /*
  Visual columns must not exceed the node’s reported trie_count (NodeUpdated).
  High trie_idx in telemetry is otherwise capped to phantom slots — empty wedges
  on the GF(257) halo. trie_count === 0 means we have not seen NodeUpdated yet;
  then allow growth up to maxIdx + 1 for early events.
  */
  const reported = node.data.trieCount;
  const cap = reported > 0 ? reported : MAX_VIZ_TRIE_VISUALS;
  const needSlots = Math.min(maxIdx + 1, cap);

  while (node.tries.length < needSlots && node.tries.length < MAX_VIZ_TRIE_VISUALS) {
    addTrieVisual(nodeId);
  }
}

export function replayEvent(ev) {
  normalizeVizEvent(ev);
  try {
    ensureTopologyForEvent(ev);
    ensureTrieCapacityForEvent(ev);
    applyEvent(ev);
  } catch (err) {
    console.warn('viz: replayEvent', err);
  }
}

export function handleEvent(ev) {
  state.events.push(ev);
  state.eventCount++;
  tickThroughput();
  state.statsDirty = true;
  if (state.paused && state.scrubPos >= 0) return;
  replayEvent(ev);
  addLogEntry(ev);
}
