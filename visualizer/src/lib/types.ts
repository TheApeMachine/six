/*
types.ts — shared state types for the visualizer.
Mirrors the actual systems in the Six project.
*/

import type * as THREE from 'three';
import type { VizEvent } from './wire';

export interface NodeState {
  id: string;
  label: string;
  insertCount: number;
  predictCount: number;
  gossipCount: number;
  trieCount: number;
  digest: { surprisal: number; entropy: number; growth: number };
  pressure: Record<string, number>;
  latencies: Record<string, number>;
  labelCounts: Record<string, number>;
  recentSequences: string[];
  eigenmode: number;
}

export interface TrieState {
  index: number;
  nodeId: string;
  insertFlash: number;
  surprisal: number;
  entropy: number;
  growth: number;
  decayMul: number;
  learnMul: number;
  aligned: boolean;
  graphPayload: TrieGraphPayload | null;
}

export interface TrieGraphPayload {
  vertices: TrieGraphVertex[];
  edges: TrieGraphEdge[];
  truncated: boolean;
}

export interface TrieGraphVertex {
  vid: number;
  depth: number;
  visits: number;
  token: string;
  value_id: number;
  x: number;
  y: number;
}

export interface TrieGraphEdge {
  from: number;
  to: number;
  token: string;
}

export interface EdgeState {
  from: string;
  to: string;
  latencyMs: number;
  gossipCount: number;
  replicationCount: number;
  activity: number;
}

export interface BeamState {
  activeCount: number;
  rejectedCount: number;
  bestScore: number;
  lastSequence: string;
  hypotheses: BeamHypothesis[];
  converged: boolean;
}

export interface BeamHypothesis {
  tokens: string;
  score: number;
  origin: string;
}

export interface ALUState {
  totalDispatches: number;
  substrates: Record<string, SubstrateState>;
  recentOps: ALUOp[];
}

export interface SubstrateState {
  inflight: number;
  totalDispatches: number;
  emaDurationNs: number;
  lastDurationNs: number;
}

export interface ALUOp {
  timestamp: number;
  substrate: string;
  opcode: number;
  durationNs: number;
  label: string;
}

export interface PipelineStageState {
  id: string;
  totalEvents: number;
  bytesProcessed: number;
  inflight: number;
  emaDurationMs: number;
  recentOps: string[];
}

export interface ComputeState {
  totalDispatches: number;
  substrates: Record<string, SubstrateState>;
}

export interface InspectorTarget {
  kind: 'node' | 'trie' | 'pipeline' | 'beam' | 'alu' | 'algorithm' | 'value';
  id: string;
  trieIndex?: number;
  vertexVid?: number;
}

export interface TimelineState {
  events: VizEvent[];
  cursor: number;
  paused: boolean;
  totalFrames: number;
}

export interface FieldState {
  globalPhase: number;
  phaseConcentration: number;
  eigenmodes: EigenmodeEntry[];
}

export interface EigenmodeEntry {
  members: string[];
  energy: number;
  phase: number;
}

export interface ValueParticle {
  id: string;
  mesh: THREE.Mesh;
  from: THREE.Vector3;
  to: THREE.Vector3;
  mid: THREE.Vector3;
  tmpPos: THREE.Vector3;
  t: number;
  speed: number;
  active: boolean;
  color: number;
}

export interface ValueGraphNode {
  id: string;
  prevId: string;
  nextId: string;
  lastSeen: number;
  depth: number;
  content: string;
  stage: string;
}

/*
SixValue is live visualizer state for one logical value (particle / beam).
*/
export interface SixValue {
  id: string;
  pos: THREE.Vector3;
  velocity: THREE.Vector3;
  tokens: Uint8Array;
  role: 'data' | 'action' | 'reaction';
  program: string | null;
  resonance: number;
  age: number;
  cooldown: number;
  meshIndex: number; // For instanced rendering
}

/*
ProcessEntry is one structured entry in the live process feed.
Each kind maps to a distinct rendering style in the UI.
*/
export interface ProcessEntry {
  ts: number;
  kind: 'beam_compose' | 'beam_converge' | 'beam_break' | 'value_submit' | 'classify' | 'experience' | 'alu_reasoning' | 'sense' | 'surprisal' | 'causal_hub';
  nodeId: string;
  label: string;
  content: string;
  score?: number;
  hypotheses?: Array<{ tokens: string; score: number }>;
}

export interface Community {
  id: number;
  members: string[]; // Value IDs
  center: THREE.Vector3;
  resonanceLevel: number;
  saturated: boolean;
  affinity: string; // Hex string of initial affinity
}
