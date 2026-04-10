import { state } from './state.js';
import { createNode } from './nodes.js';
import { clearScene } from './lifecycle.js';
import { normalizeVizEvent, handleEvent, replayEvent } from './events_pipeline.js';
import { updateStats } from './stats.js';
import { decodeVizMessage } from './wire_decode.js';

let ws = null;
let reconnectTimer = null;

const textDecoder = new TextDecoder();

/*
handleServerResponse applies out-of-band control messages from the viz server
(bootstrap node list, scrub replay bundles, drop counters).
*/
export function handleServerResponse(resp) {
  if (resp.action === 'bootstrap' && Array.isArray(resp.nodes)) {
    for (const id of resp.nodes) {
      if (typeof id === 'string' && id.startsWith('node_')) createNode(id, id);
    }
    state.statsDirty = true;
    return;
  }
  if (resp.action === 'scrub_result' && resp.events) {
    clearScene();
    for (const ev of resp.events) replayEvent(ev);
    state.statsDirty = true;
    return;
  }
  if (resp.action === 'stats') {
    state.droppedCount = resp.dropped || 0;
    updateStats();
  }
}

function dispatchBinary(u8) {
  const decoded = decodeVizMessage(u8);
  if (!decoded) return false;

  switch (decoded.frameType) {
    case 'bootstrap':
      handleServerResponse({ action: 'bootstrap', nodes: decoded.nodes });
      break;
    case 'stats':
      handleServerResponse({ action: 'stats', dropped: decoded.dropped });
      break;
    case 'scrub':
      handleServerResponse({ action: 'scrub_result', events: decoded.events });
      break;
    case 'event':
      normalizeVizEvent(decoded.event);
      handleEvent(decoded.event);
      break;
    case 'json': {
      const env = JSON.parse(decoded.text);
      if (env.action) {
        handleServerResponse(env);
      }
      break;
    }
    default:
      break;
  }

  return true;
}

/*
connect opens the WebSocket (binary viz frames) and reconnects after backoff.
*/
export function connect() {
  const url = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws`;
  ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => {
    document.title = 'Six — Connected';
  };
  ws.onmessage = async (msg) => {
    try {
      if (msg.data instanceof ArrayBuffer) {
        const u8 = new Uint8Array(msg.data);
        if (dispatchBinary(u8)) return;
        const text = textDecoder.decode(u8);
        const ev = JSON.parse(text);
        if (ev.action) {
          handleServerResponse(ev);
          return;
        }
        normalizeVizEvent(ev);
        handleEvent(ev);
        return;
      }

      if (typeof Blob !== 'undefined' && msg.data instanceof Blob) {
        const u8 = new Uint8Array(await msg.data.arrayBuffer());
        if (dispatchBinary(u8)) return;
        const text = textDecoder.decode(u8);
        const ob = JSON.parse(text);
        if (ob.action) {
          handleServerResponse(ob);
          return;
        }
        normalizeVizEvent(ob);
        handleEvent(ob);
        return;
      }

      const ev = JSON.parse(msg.data);
      if (ev.action) {
        handleServerResponse(ev);
        return;
      }
      normalizeVizEvent(ev);
      handleEvent(ev);
    } catch (err) {
      console.error('viz: ws message error', err);
    }
  };
  ws.onclose = () => {
    document.title = 'Six — Disconnected';
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 2000);
  };
  ws.onerror = () => {
    ws.close();
  };
}
