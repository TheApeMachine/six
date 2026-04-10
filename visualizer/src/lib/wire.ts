/*
wire.ts decodes the binary WebSocket frames from pkg/viz/wire.go v1.

Frame layout: [V, Z, B, 0x01, frameType, ...payload]
Frame types: Event(1), Bootstrap(2), Stats(3), Scrub(4), JSONBlob(5)
*/

const MAGIC = new Uint8Array([0x56, 0x5a, 0x42, 0x01]);

export const FrameType = {
  Event: 1,
  Bootstrap: 2,
  Stats: 3,
  Scrub: 4,
  JSONBlob: 5,
} as const;

export const EK = {
  NodeCreated: 0, NodeUpdated: 1, NodeRemoved: 2,
  PeerAdded: 3, PeerRemoved: 4, PeerLatency: 5,
  ValuePublished: 6, ValueReplicated: 7,
  GossipSent: 8, GossipReceived: 9,
  FieldDigest: 10, EigenmodeDetected: 11, FieldPressure: 12,
  TrieInsert: 13, TrieDecay: 14, TriePrune: 15,
  TriePredict: 16, TrieClassify: 17, TrieExperience: 18,
  PoolSchedule: 19, PoolComplete: 20,
  AdaptiveUpdate: 21,
  TrieCoupling: 22, TrieMode: 23, TriePressure: 24, TrieSignal: 25,
  BeamCollect: 26, BeamCompose: 27, BeamBreak: 28, BeamConverge: 29,
  Prompt: 30, PromptResult: 31,
  TrieGraphSnapshot: 32,
  CompilerCompile: 33,
  ALUDispatch: 34,
  FinalizerRun: 35,
  DatasetRead: 36,
  TokenizerChunk: 37,
  TokenizerEmit: 38,
  QueueSubmit: 39,
} as const;

export const KIND_NAMES: Record<number, string> = Object.fromEntries(
  Object.entries(EK).map(([k, v]) => [v, k])
);

export interface VizEvent {
  kind: number;
  ts: number;
  src: string;
  tgt: string;
  lbl: string;
  vals: Record<string, number>;
  meta: Record<string, string>;
}

export type DecodedFrame =
  | { frameType: 'event'; event: VizEvent }
  | { frameType: 'bootstrap'; nodes: string[] }
  | { frameType: 'stats'; dropped: number }
  | { frameType: 'scrub'; events: VizEvent[] }
  | { frameType: 'json'; text: string };

const textDecoder = new TextDecoder();

function matchMagic(u8: Uint8Array): boolean {
  for (let i = 0; i < 4; i++) {
    if (u8[i] !== MAGIC[i]) return false;
  }
  return true;
}

function readString(u8: Uint8Array, off: number): { s: string; off: number } {
  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
  if (off + 4 > u8.length) throw new Error('wire: truncated string len');
  const len = dv.getUint32(off, true);
  off += 4;
  if (len > 1 << 28) throw new Error('wire: string too large');
  if (off + len > u8.length) throw new Error('wire: truncated string data');
  const s = textDecoder.decode(u8.subarray(off, off + len));
  off += len;
  return { s, off };
}

function decodeEventPayload(u8: Uint8Array, off: number): VizEvent {
  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);

  if (off + 8 > u8.length) throw new Error('wire: event ts');
  const tsBig = dv.getBigInt64(off, true);
  off += 8;

  if (off >= u8.length) throw new Error('wire: event kind');
  const kind = u8[off];
  off += 1;

  let r = readString(u8, off); const src = r.s; off = r.off;
  r = readString(u8, off); const tgt = r.s; off = r.off;
  r = readString(u8, off); const lbl = r.s; off = r.off;

  if (off + 4 > u8.length) throw new Error('wire: n_vals');
  const nVals = dv.getUint32(off, true);
  off += 4;
  const vals: Record<string, number> = {};
  for (let i = 0; i < nVals; i++) {
    r = readString(u8, off); off = r.off;
    if (off + 8 > u8.length) throw new Error('wire: val f64');
    vals[r.s] = dv.getFloat64(off, true);
    off += 8;
  }

  if (off + 4 > u8.length) throw new Error('wire: n_meta');
  const nMeta = dv.getUint32(off, true);
  off += 4;
  const meta: Record<string, string> = {};
  for (let i = 0; i < nMeta; i++) {
    r = readString(u8, off); const key = r.s; off = r.off;
    r = readString(u8, off); meta[key] = r.s; off = r.off;
  }

  return { kind, ts: Number(tsBig), src, tgt, lbl, vals, meta };
}

export function decodeVizMessage(u8: Uint8Array): DecodedFrame | null {
  if (u8.length < 5 || !matchMagic(u8)) return null;

  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
  const frameType = u8[4];
  const off0 = 5;

  if (frameType === FrameType.Event) {
    return { frameType: 'event', event: decodeEventPayload(u8, off0) };
  }

  if (frameType === FrameType.Bootstrap) {
    let off = off0;
    const n = dv.getUint32(off, true); off += 4;
    const nodes: string[] = [];
    for (let i = 0; i < n; i++) {
      const r = readString(u8, off); nodes.push(r.s); off = r.off;
    }
    return { frameType: 'bootstrap', nodes };
  }

  if (frameType === FrameType.Stats) {
    const dropped = Number(dv.getBigUint64(off0, true));
    return { frameType: 'stats', dropped };
  }

  if (frameType === FrameType.Scrub) {
    let off = off0;
    const nEv = dv.getUint32(off, true); off += 4;
    const events: VizEvent[] = [];
    for (let i = 0; i < nEv; i++) {
      const chunkLen = dv.getUint32(off, true); off += 4;
      events.push(decodeEventPayload(u8.subarray(off, off + chunkLen), 0));
      off += chunkLen;
    }
    return { frameType: 'scrub', events };
  }

  if (frameType === FrameType.JSONBlob) {
    let off = off0;
    const jlen = dv.getUint32(off, true); off += 4;
    const text = textDecoder.decode(u8.subarray(off, off + jlen));
    return { frameType: 'json', text };
  }

  return null;
}
