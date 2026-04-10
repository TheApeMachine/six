import { state } from './state.js';

export function updateStats() {
  document.getElementById('stat-nodes').textContent = state.nodes.size;
  document.getElementById('stat-tries').textContent = [...state.nodes.values()].reduce((s, n) => s + n.tries.length, 0);
  document.getElementById('stat-edges').textContent = state.edges.size;
  document.getElementById('stat-events').textContent = state.eventCount;
  document.getElementById('stat-dropped').textContent = state.droppedCount;
}
