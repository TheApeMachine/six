/* ═══════════════════════════════════════════════════════════
   websocket.js — WebSocket connection management
   ═══════════════════════════════════════════════════════════ */

let activeWS = null;
let onEventCallback = null;
let onConnectCallback = null;
let onDisconnectCallback = null;

export function initWebSocket(onEvent, onConnect, onDisconnect) {
  onEventCallback = onEvent;
  onConnectCallback = onConnect;
  onDisconnectCallback = onDisconnect;
}

export function connect() {
  const ws = new WebSocket(`ws://${location.host}/ws`);

  ws.onopen = () => {
    activeWS = ws;
    if (onConnectCallback) onConnectCallback();
  };

  ws.onclose = () => {
    activeWS = null;
    if (onDisconnectCallback) onDisconnectCallback();
    setTimeout(connect, 2000);
  };

  ws.onmessage = (event) => {
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
  if (!activeWS || activeWS.readyState !== WebSocket.OPEN || !text.trim()) return false;
  activeWS.send(JSON.stringify({ type: 'ingest', message: text }));
  return true;
}

export function isConnected() {
  return activeWS && activeWS.readyState === WebSocket.OPEN;
}
