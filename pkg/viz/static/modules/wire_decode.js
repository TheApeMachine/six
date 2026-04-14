/*
wire_decode parses Viz binary WebSocket frames (pkg/viz/wire.go v1).

Maps wire events into the same object shape the JSON path used: kind, ts, src,
tgt, lbl, vals, meta — so normalizeVizEvent / handleEvent stay unchanged.
*/

const MAGIC = new Uint8Array([0x56, 0x5a, 0x42, 0x01]);

export const WireFrame = {
  Event: 1,
  Bootstrap: 2,
  Stats: 3,
  Scrub: 4,
  JSONBlob: 5,
  Value: 6,
};

const textDecoder = new TextDecoder();

function matchMagic(u8) {
  for (let i = 0; i < 4; i++) {
    if (u8[i] !== MAGIC[i]) return false;
  }
  return true;
}

function readString(u8, off) {
  if (off + 4 > u8.length) throw new Error('wire: truncated string len');
  const len = new DataView(u8.buffer, u8.byteOffset, u8.byteLength).getUint32(off, true);
  off += 4;
  if (len > 1 << 28) throw new Error('wire: string too large');
  if (off + len > u8.length) throw new Error('wire: truncated string data');
  const s = textDecoder.decode(u8.subarray(off, off + len));
  off += len;
  return { s, off };
}

function decodeEventPayload(u8, off) {
  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
  if (off + 8 > u8.length) throw new Error('wire: event ts');
  const tsBig = dv.getBigInt64(off, true);
  off += 8;
  if (off >= u8.length) throw new Error('wire: event kind');
  const kind = u8[off];
  off += 1;

  let r = readString(u8, off);
  const src = r.s;
  off = r.off;

  r = readString(u8, off);
  const tgt = r.s;
  off = r.off;

  r = readString(u8, off);
  const lbl = r.s;
  off = r.off;

  if (off + 4 > u8.length) throw new Error('wire: n_vals');
  const nVals = dv.getUint32(off, true);
  off += 4;
  const vals = {};
  for (let i = 0; i < nVals; i++) {
    r = readString(u8, off);
    off = r.off;
    if (off + 8 > u8.length) throw new Error('wire: val f64');
    const fv = dv.getFloat64(off, true);
    off += 8;
    vals[r.s] = fv;
  }

  if (off + 4 > u8.length) throw new Error('wire: n_meta');
  const nMeta = dv.getUint32(off, true);
  off += 4;
  const meta = {};
  for (let i = 0; i < nMeta; i++) {
    r = readString(u8, off);
    const key = r.s;
    off = r.off;
    r = readString(u8, off);
    meta[key] = r.s;
    off = r.off;
  }

  if (off !== u8.length) throw new Error(`wire: ${u8.length - off} trailing bytes in event`);

  return {
    kind,
    ts: Number(tsBig),
    src,
    tgt,
    lbl,
    vals,
    meta,
  };
}

/*
decodeVizMessage returns a discriminated result, or null if this is not a viz binary frame.
*/
export function decodeVizMessage(u8) {
  if (!(u8 instanceof Uint8Array) || u8.length < 5) return null;
  if (!matchMagic(u8)) return null;

  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
  const frameType = u8[4];
  const off0 = 5;

  if (frameType === WireFrame.Event) {
    const event = decodeEventPayload(u8, off0);
    return { frameType: 'event', event };
  }

  if (frameType === WireFrame.Bootstrap) {
    let off = off0;
    if (off + 4 > u8.length) throw new Error('wire: bootstrap n');
    const n = dv.getUint32(off, true);
    off += 4;
    const nodes = [];
    for (let i = 0; i < n; i++) {
      const r = readString(u8, off);
      nodes.push(r.s);
      off = r.off;
    }
    if (off !== u8.length) throw new Error('wire: bootstrap trailing');
    return { frameType: 'bootstrap', nodes };
  }

  if (frameType === WireFrame.Stats) {
    if (off0 + 8 > u8.length) throw new Error('wire: stats');
    const dropped = dv.getBigUint64(off0, true);
    if (off0 + 8 !== u8.length) throw new Error('wire: stats trailing');
    return { frameType: 'stats', dropped: Number(dropped) };
  }

  if (frameType === WireFrame.Scrub) {
    let off = off0;
    if (off + 4 > u8.length) throw new Error('wire: scrub n');
    const nEv = dv.getUint32(off, true);
    off += 4;
    const events = [];
    for (let i = 0; i < nEv; i++) {
      if (off + 4 > u8.length) throw new Error('wire: scrub chunk len');
      const chunkLen = dv.getUint32(off, true);
      off += 4;
      if (off + chunkLen > u8.length) throw new Error('wire: scrub chunk data');
      const chunk = u8.subarray(off, off + chunkLen);
      off += chunkLen;
      events.push(decodeEventPayload(chunk, 0));
    }
    if (off !== u8.length) throw new Error('wire: scrub trailing');
    return { frameType: 'scrub', events };
  }

  if (frameType === WireFrame.JSONBlob) {
    let off = off0;
    if (off + 4 > u8.length) throw new Error('wire: json len');
    const jlen = dv.getUint32(off, true);
    off += 4;
    if (off + jlen > u8.length) throw new Error('wire: json body');
    const jsonBytes = u8.subarray(off, off + jlen);
    off += jlen;
    if (off !== u8.length) throw new Error('wire: json trailing');
    const text = textDecoder.decode(jsonBytes);
    return { frameType: 'json', text };
  }

  if (frameType === WireFrame.Value) {
    if (off0 + 8 + 4 > u8.length) throw new Error('wire: value header');
    const valueId = dv.getBigUint64(off0, true);
    let off = off0 + 8;
    const blen = dv.getUint32(off, true);
    off += 4;
    if (off + blen > u8.length) throw new Error('wire: value body');
    const bytes = u8.subarray(off, off + blen);
    off += blen;
    if (off !== u8.length) throw new Error('wire: value trailing');
    return { frameType: 'value', valueId, bytes };
  }

  throw new Error(`wire: unknown frame type ${frameType}`);
}
