/* ═══════════════════════════════════════════════════════════
   websocket.js — WebSocket connection management
   ═══════════════════════════════════════════════════════════ */

let activeWS = null;
let onEventCallback = null;
let onConnectCallback = null;
let onDisconnectCallback = null;

const RECONNECT_INITIAL_MS = 2000;
const RECONNECT_MAX_MS = 30000;
let reconnectDelay = RECONNECT_INITIAL_MS;

export function initWebSocket(onEvent, onConnect, onDisconnect) {
  onEventCallback = onEvent;
  onConnectCallback = onConnect;
  onDisconnectCallback = onDisconnect;
}

export function connect() {
  const ws = new WebSocket(`ws://${location.host}/ws`);
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    activeWS = ws;
    reconnectDelay = RECONNECT_INITIAL_MS;
    if (onConnectCallback) onConnectCallback();
  };

  ws.onclose = () => {
    activeWS = null;
    if (onDisconnectCallback) onDisconnectCallback();
    const delay = reconnectDelay;
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
    setTimeout(connect, delay);
  };

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      if (onEventCallback) onEventCallback({ _binary: true, buffer: event.data });
      return;
    }
    try {
      const ev = JSON.parse(event.data);
      if (onEventCallback) onEventCallback(ev);
    } catch (e) {
      console.error('Parse:', e);
    }
  };
}

export function sendPrompt(msg) {
  if (!activeWS || activeWS.readyState !== WebSocket.OPEN || !msg.trim()) return false;
  activeWS.send(JSON.stringify({ type: 'prompt', message: msg.trim() }));
  return true;
}

export function sendIngest(text) {
  const msg = text.trim();
  if (!activeWS || activeWS.readyState !== WebSocket.OPEN || !msg) return false;
  activeWS.send(JSON.stringify({ type: 'ingest', message: msg }));
  return true;
}

export function isConnected() {
  return activeWS && activeWS.readyState === WebSocket.OPEN;
}
