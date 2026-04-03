/* ═══════════════════════════════════════════════════════════
   graph2d.js — 2D SVG Value-chain graph in the top strip
   Renders Values as a horizontal chain: newest on the right,
   linked by PrevID / NextID edges.
   ═══════════════════════════════════════════════════════════ */

// ── Config ────────────────────────────────────────────────
const NODE_W = 120;
const NODE_H = 48;
const NODE_GAP = 40;        // horizontal gap between nodes
const NODE_Y_CENTER = 60;   // vertical center in the viewport
const PADDING_X = 20;
const MAX_NODES = 60;

// ── DOM refs ──────────────────────────────────────────────
const viewport = document.getElementById('graph-viewport');
const svg = document.getElementById('graph-svg');
const worldG = document.getElementById('graph-world');
const edgesG = document.getElementById('graph-edges');
const nodesG = document.getElementById('graph-nodes');
const countEl = document.getElementById('graph-count');

// ── State ─────────────────────────────────────────────────
const nodeMap = new Map();    // id → { id, x, y, el, type, snapshot, text, order }
const nodeOrder = [];         // insertion order (ids)
const edgeMap = new Map();    // "kind:from:to" → { el, fromId, toId, kind }

let panX = 0;
let nextSlotX = PADDING_X;
let nodeCounter = 0;

// ── Pan / drag ────────────────────────────────────────────
let dragging = false;
let dragStartX = 0;
let dragStartPan = 0;

if (viewport) {
  viewport.addEventListener('pointerdown', (e) => {
    if (e.target.closest('.svg-node')) return; // don't pan when clicking node
    dragging = true;
    dragStartX = e.clientX;
    dragStartPan = panX;
    viewport.setPointerCapture(e.pointerId);
  });
  viewport.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    panX = dragStartPan + (e.clientX - dragStartX);
    applyTransform();
  });
  viewport.addEventListener('pointerup', () => { dragging = false; });
  viewport.addEventListener('pointercancel', () => { dragging = false; });

  // scroll wheel pans horizontally
  viewport.addEventListener('wheel', (e) => {
    e.preventDefault();
    panX -= e.deltaX || e.deltaY;
    applyTransform();
  }, { passive: false });
}

function applyTransform() {
  if (worldG) worldG.setAttribute('transform', `translate(${panX}, 0)`);
}

// ── Fit to view ───────────────────────────────────────────
export function fitGraph() {
  if (nodeMap.size === 0) return;
  const vpWidth = viewport ? viewport.clientWidth : 800;
  const totalWidth = nextSlotX + PADDING_X;
  if (totalWidth <= vpWidth) {
    panX = 0;
  } else {
    // Scroll so the rightmost node is visible
    panX = vpWidth - totalWidth;
  }
  applyTransform();
}

// ── Build SVG node ────────────────────────────────────────
function createNodeEl(id, x, y, text, chainText, type) {
  const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
  g.setAttribute('class', `svg-node type-${type}`);
  g.setAttribute('transform', `translate(${x}, ${y})`);
  g.dataset.nodeId = id;

  const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
  rect.setAttribute('x', 0);
  rect.setAttribute('y', 0);
  rect.setAttribute('width', NODE_W);
  rect.setAttribute('height', NODE_H);
  rect.setAttribute('rx', 2);
  g.appendChild(rect);

  const idText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
  idText.setAttribute('class', 'svg-node-id');
  idText.setAttribute('x', 6);
  idText.setAttribute('y', 14);
  idText.textContent = `#${String(id).slice(-6)}`;
  g.appendChild(idText);

  const bodyText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
  bodyText.setAttribute('class', 'svg-node-text');
  bodyText.setAttribute('x', 6);
  bodyText.setAttribute('y', 28);
  bodyText.textContent = truncate(text, 18);
  g.appendChild(bodyText);

  const chainEl = document.createElementNS('http://www.w3.org/2000/svg', 'text');
  chainEl.setAttribute('class', 'svg-node-chain');
  chainEl.setAttribute('x', 6);
  chainEl.setAttribute('y', 42);
  chainEl.textContent = chainText;
  g.appendChild(chainEl);

  return { g, bodyText, chainEl, idText };
}

function truncate(s, max) {
  if (!s) return '';
  return s.length > max ? s.slice(0, max - 1) + '…' : s;
}

// ── Public: add / update node ─────────────────────────────
export function graphAddNode(id, tokens, type, extra = {}) {
  const key = String(id);
  const existing = nodeMap.get(key);

  if (existing) {
    // Update text
    if (extra.text !== undefined) {
      existing.els.bodyText.textContent = truncate(extra.text, 18);
    } else if (tokens !== undefined) {
      existing.els.bodyText.textContent = truncate(tokens || '', 18);
    }
    const snap = extra.snapshot || existing.snapshot;
    const prevS = snap?.prevId ? String(snap.prevId).slice(-4) : '—';
    const nextS = snap?.nextId ? String(snap.nextId).slice(-4) : '—';
    existing.els.chainEl.textContent = `${prevS} → ${nextS}`;
    existing.type = type;
    existing.snapshot = snap;
    if (type !== 'ref') {
      existing.els.g.setAttribute('class', `svg-node type-${type}`);
    }
    flushPendingEdges();
    return existing;
  }

  // New node — assign a horizontal slot
  const x = nextSlotX;
  const y = NODE_Y_CENTER - NODE_H / 2;
  nextSlotX += NODE_W + NODE_GAP;

  const snap = extra.snapshot || null;
  const prevS = snap?.prevId ? String(snap.prevId).slice(-4) : '—';
  const nextS = snap?.nextId ? String(snap.nextId).slice(-4) : '—';
  const text = extra.text || (tokens || '').trim() || `#${key.slice(-6)}`;

  const els = createNodeEl(key, x, y, text, `${prevS} → ${nextS}`, type);
  nodesG.appendChild(els.g);

  const node = { id: key, x, y, type, snapshot: snap, els, order: nodeCounter++ };
  nodeMap.set(key, node);
  nodeOrder.push(key);

  // Click handler
  els.g.addEventListener('click', () => {
    if (typeof window._graphNodeClick === 'function') {
      window._graphNodeClick(key);
    }
  });

  pruneNodes();
  flushPendingEdges();
  updateCount();

  // Auto-scroll to show latest node
  autoScrollRight();

  return node;
}

function autoScrollRight() {
  if (!viewport) return;
  const vpWidth = viewport.clientWidth;
  const rightEdge = nextSlotX + PADDING_X;
  if (rightEdge + panX > vpWidth) {
    panX = vpWidth - rightEdge;
    applyTransform();
  }
}

function pruneNodes() {
  while (nodeOrder.length > MAX_NODES) {
    const oldest = nodeOrder.shift();
    removeNode(oldest);
  }
}

function removeNode(id) {
  const key = String(id);
  const node = nodeMap.get(key);
  if (!node) return;
  node.els.g.remove();
  nodeMap.delete(key);

  // Remove edges touching this node
  for (const [edgeKey, edge] of edgeMap.entries()) {
    if (edge.fromId === key || edge.toId === key) {
      edge.el.remove();
      edgeMap.delete(edgeKey);
    }
  }
}

// ── Edges ─────────────────────────────────────────────────
const pendingEdges = new Map();

export function graphAddEdge(fromId, toId, kind = 'link') {
  const fromKey = String(fromId);
  const toKey = String(toId);
  const key = `${kind}:${fromKey}:${toKey}`;
  if (edgeMap.has(key)) return;

  const from = nodeMap.get(fromKey);
  const to = nodeMap.get(toKey);
  if (!from || !to) {
    pendingEdges.set(key, { fromKey: fromKey, toKey: toKey, kind });
    return;
  }

  drawEdge(key, from, to, kind);
}

function drawEdge(key, from, to, kind) {
  const x1 = from.x + NODE_W;
  const y1 = from.y + NODE_H / 2;
  const x2 = to.x;
  const y2 = to.y + NODE_H / 2;

  // Bezier curve for a nice arc
  const dx = x2 - x1;
  const midX = x1 + dx / 2;
  // If edge goes backward (left), arc upward more
  const arcY = dx < 0 ? -30 : (kind === 'next' ? 8 : -8);

  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('class', `svg-edge edge-${kind}`);
  path.setAttribute('d', `M${x1},${y1} C${midX},${y1 + arcY} ${midX},${y2 + arcY} ${x2},${y2}`);
  edgesG.appendChild(path);

  edgeMap.set(key, { el: path, fromId: from.id, toId: to.id, kind });
}

function flushPendingEdges() {
  if (pendingEdges.size === 0) return;
  for (const [key, edge] of [...pendingEdges.entries()]) {
    const from = nodeMap.get(edge.fromKey);
    const to = nodeMap.get(edge.toKey);
    if (!from || !to) continue;
    pendingEdges.delete(key);
    drawEdge(key, from, to, edge.kind);
  }
}

// ── Public: add Value node (high-level) ───────────────────
export function graphAddValueNode(snapshot) {
  if (!snapshot || !snapshot.valueId) return null;

  const hasChainLink = (snapshot.prevId && snapshot.prevId !== '0' && snapshot.prevId !== 0)
    || (snapshot.nextId && snapshot.nextId !== '0' && snapshot.nextId !== 0);

  const nodeType = hasChainLink ? 'value' : 'raw';
  const text = snapshot.tokenPreview || snapshot.tokenText || `#${String(snapshot.valueId).slice(-6)}`;
  const summary = snapshot.summary
    || `#${snapshot.valueId} prev=${snapshot.prevId || '0'} next=${snapshot.nextId || '0'}`;

  const node = graphAddNode(snapshot.valueId, text, nodeType, {
    text,
    summary,
    snapshot,
  });

  // Draw chain link edges
  if (snapshot.prevId && snapshot.prevId !== '0' && snapshot.prevId !== 0) {
    if (!nodeMap.has(String(snapshot.prevId))) {
      graphAddNode(snapshot.prevId, '', 'ref', { text: `#${String(snapshot.prevId).slice(-6)}` });
    }
    graphAddEdge(snapshot.prevId, snapshot.valueId, 'prev');
  }
  if (snapshot.nextId && snapshot.nextId !== '0' && snapshot.nextId !== 0) {
    if (!nodeMap.has(String(snapshot.nextId))) {
      graphAddNode(snapshot.nextId, '', 'ref', { text: `#${String(snapshot.nextId).slice(-6)}` });
    }
    graphAddEdge(snapshot.valueId, snapshot.nextId, 'next');
  }

  return node;
}

// ── Public: clear ─────────────────────────────────────────
export function graphClear() {
  for (const node of nodeMap.values()) node.els.g.remove();
  for (const edge of edgeMap.values()) edge.el.remove();
  nodeMap.clear();
  nodeOrder.length = 0;
  edgeMap.clear();
  pendingEdges.clear();
  nextSlotX = PADDING_X;
  nodeCounter = 0;
  panX = 0;
  applyTransform();
  updateCount();
}

function updateCount() {
  if (countEl) countEl.textContent = `${nodeMap.size} nodes`;
}

// ── Wire up buttons ───────────────────────────────────────
const clearBtn = document.getElementById('graph-clear-btn');
const fitBtn = document.getElementById('graph-fit-btn');
if (clearBtn) clearBtn.addEventListener('click', graphClear);
if (fitBtn) fitBtn.addEventListener('click', fitGraph);
