export { EK, KIND_NAMES } from './event_kinds.js';

export function kindClass(kind) {
  if (kind <= 2) return 'c-node';
  if (kind <= 5) return 'c-peer';
  if (kind <= 7) return 'c-data';
  if (kind <= 12) return 'c-field';
  if (kind <= 18) return 'c-trie';
  if (kind <= 20) return 'c-pool';
  if (kind <= 21) return 'c-node';
  if (kind <= 25) return 'c-field';
  if (kind <= 29) return 'c-beam';
  if (kind === 32) return 'c-trie';
  if (kind >= 33 && kind <= 35) return 'c-pool';
  if (kind >= 36 && kind <= 39) return 'c-pipeline';
  return 'c-user';
}

export const EDGE_COLORS = {
  peer: 0x4868a8,
  gossip: 0xa080e0,
  replication: 0xe06888,
  latency: 0x6ea8fe,
};

/*
Cap trie column geometry: TrieSignal/TrieMode/TriePressure indices are real
Markov trie slots. TrieCoupling must never grow this list — Go publishes
pairwise participants over digest origins there, not node.tries indices.
*/
export const MAX_VIZ_TRIE_VISUALS = 64;

export const NODE_RADIUS = 16;

/*
Finite-field phase layers (pkg/core/numeric/gf, Kadabra Field) — colors drive viz only.
*/
export const GF_LAYER = {
  global: {
    mod: 65537,
    color: 0xa868e8,
    hexCss: '#a868e8',
    label: 'GF(65537)',
  },
  node: {
    mod: 8191,
    color: 0xe89850,
    hexCss: '#e89850',
    label: 'GF(8191)',
  },
  trie: {
    mod: 257,
    color: 0x40c8a0,
    hexCss: '#40c8a0',
    label: 'GF(257)',
  },
};

/*
Local XZ ring under each host for trie column groups — fixed like NODE_RADIUS so
columns stay equally spaced on the full circle as count changes (same rule as
repositionNodes, smaller scale).
*/
export const TRIE_COLUMN_RING_RADIUS = 4.25;

/*
Pipeline stage definitions — 5 stages arranged in an arc below the node ring.
Each stage has a fixed position in 3D space, a color, and a shape.
The arc sits at Z = -NODE_RADIUS * 1.6 so it is clearly separated from the
Kadabra node ring but visible in the default camera view.
*/
/*
Pipeline legend entries for the topbar — matches the stage definitions in pipeline.js.
*/
export const PIPELINE_STAGES = [
  { id: 'machine',   label: 'Machine',   hexCss: '#4a80c0' },
  { id: 'dataset',   label: 'Dataset',   hexCss: '#408060' },
  { id: 'tokenizer', label: 'Tokenizer', hexCss: '#308070' },
  { id: 'backend',   label: 'Backend',   hexCss: '#806030' },
  { id: 'queue',     label: 'Queue',     hexCss: '#7050a0' },
  { id: 'cpu',       label: 'CPU',       hexCss: '#4a80c0' },
  { id: 'metal',     label: 'Metal',     hexCss: '#7050a0' },
  { id: 'cuda',      label: 'CUDA',      hexCss: '#608030' },
];
