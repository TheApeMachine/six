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
let reconnectTimer = null;
let shouldReconnect = true;

export function initWebSocket(onEvent, onConnect, onDisconnect) {
  onEventCallback = onEvent;
  onConnectCallback = onConnect;
  onDisconnectCallback = onDisconnect;
}

export function disconnect() {
  shouldReconnect = false;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  reconnectDelay = RECONNECT_INITIAL_MS;
  if (activeWS) {
    try {
      activeWS.close();
    } catch (_) { /* ignore */ }
    activeWS = null;
  }
}

export function connect() {
  shouldReconnect = true;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${scheme}://${location.host}/ws`);
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    activeWS = ws;
    reconnectDelay = RECONNECT_INITIAL_MS;
    if (onConnectCallback) onConnectCallback();
  };

  ws.onerror = (event) => {
    const errMsg = event?.message || (event?.error && String(event.error)) || 'WebSocket error';
    console.error('WebSocket error:', errMsg, event);
    if (onEventCallback) {
      onEventCallback({
        type: 'error',
        component: 'WebSocket',
        action: 'Error',
        data: { message: errMsg },
      });
    }
  };

  ws.onclose = () => {
    activeWS = null;
    if (onDisconnectCallback) onDisconnectCallback();
    if (!shouldReconnect) return;
    const delay = reconnectDelay;
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, delay);
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
