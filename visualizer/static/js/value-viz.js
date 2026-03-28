/* ═══════════════════════════════════════════════════════════
   value-viz.js — 3D Value structure visualization
   Renders the 1024-byte Value frame as a ring, and decodes
   live wire frames into inspectable snapshots.
   ═══════════════════════════════════════════════════════════ */
import * as THREE from 'three';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { valueGroup } from './scene.js';
import { SYS } from './architecture.js';

const WORDS = 128;
const BYTE_SIZE = 1024;
const TOTAL_BITS = WORDS * 64;
const DISPLAY_BITS = 512;

let cellMeshes = [];
let regionLabels = [];
let valueRingGroup = null;

const DEFAULT_LAYOUT = normalizeValueLayout({
  words: WORDS,
  byteSize: BYTE_SIZE,
  tokenBits: 3648,
  tokenWords: 57,
  indices: {
    valueId: 57,
    prevId: 58,
    nextId: 59,
    state: 60,
    sequence: 61,
    accumulator: 62,
    execStatus: 63,
    affinity: 64,
    gossip: 65,
    gossipWords: 5,
    ttl: 70,
    registersStart: 71,
    pc: 78,
    program: 79,
    programWords: 49,
    programSlots: 98,
  },
  registers: {
    r0: 71,
    r1: 72,
    r2: 73,
    r3: 74,
    r4: 75,
    r5: 76,
    fw: 77,
    pc: 78,
  },
  opcodeNames: [
    'const-0', 'nor', 'lt', 'not-b',
    'gt', 'not-a', 'xor', 'nand',
    'and', 'xnor', 'proj-b', 'implies',
    'proj-a', 'converse', 'or', 'const-1',
  ],
  execExitNames: ['none', 'exhausted', 'halt-opcode', 'bad-program-word'],
});

let valueLayout = DEFAULT_LAYOUT;

const FIELD_COLORS = {
  tokens: 0x4090e0,
  identity: 0x8fdc7a,
  link: 0x50c090,
  state: 0xffcc66,
  exec: 0xe06050,
  affinity: 0x50d0e0,
  gossip: 0x50a0a0,
  ttl: 0xe06050,
  registers: 0xa070e0,
  pc: 0xffb84d,
  program: 0xa070e0,
  default: 0x2040a0,
};

function normalizeValueLayout(layout = {}) {
  const indices = {
    valueId: 57,
    prevId: 58,
    nextId: 59,
    state: 60,
    sequence: 61,
    accumulator: 62,
    execStatus: 63,
    affinity: 64,
    gossip: 65,
    gossipWords: 5,
    ttl: 70,
    registersStart: 71,
    pc: 78,
    program: 79,
    programWords: 49,
    programSlots: 98,
    ...(layout.indices || {}),
  };

  const registers = {
    r0: 71,
    r1: 72,
    r2: 73,
    r3: 74,
    r4: 75,
    r5: 76,
    fw: 77,
    pc: 78,
    ...(layout.registers || {}),
  };

  const fields = Array.isArray(layout.fields) && layout.fields.length > 0
    ? layout.fields.map(normalizeField)
    : buildDefaultFields(indices);

  return {
    words: Number(layout.words || WORDS),
    byteSize: Number(layout.byteSize || BYTE_SIZE),
    tokenBits: Number(layout.tokenBits || 3648),
    tokenWords: Number(layout.tokenWords || 57),
    indices,
    registers,
    fields,
    opcodeNames: Array.isArray(layout.opcodeNames) && layout.opcodeNames.length >= 16
      ? layout.opcodeNames.slice(0, 16)
      : DEFAULT_LAYOUT.opcodeNames.slice(),
    execExitNames: Array.isArray(layout.execExitNames) && layout.execExitNames.length > 0
      ? layout.execExitNames.slice()
      : DEFAULT_LAYOUT.execExitNames.slice(),
  };
}

function normalizeField(field) {
  return {
    name: String(field.name || field.label || 'field'),
    kind: String(field.kind || 'default'),
    label: String(field.label || field.name || 'Field'),
    startWord: Number(field.startWord || 0),
    wordCount: Number(field.wordCount || 0),
    bits: Number(field.bits || 0),
  };
}

function buildDefaultFields(indices) {
  const registersWordCount = Math.max(0, indices.pc - indices.registersStart);
  return [
    {
      name: 'tokens',
      kind: 'tokens',
      label: 'Tokens',
      startWord: 0,
      wordCount: 57,
      bits: 3648,
    },
    {
      name: 'value-id',
      kind: 'identity',
      label: 'Value ID',
      startWord: 57,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'prev-id',
      kind: 'link',
      label: 'Prev ID',
      startWord: 58,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'next-id',
      kind: 'link',
      label: 'Next ID',
      startWord: 59,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'state',
      kind: 'state',
      label: 'State',
      startWord: 60,
      wordCount: 3,
      bits: 192,
    },
    {
      name: 'exec-status',
      kind: 'exec',
      label: 'Exec Status',
      startWord: 63,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'affinity',
      kind: 'affinity',
      label: 'Affinity',
      startWord: 64,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'gossip',
      kind: 'gossip',
      label: 'Gossip',
      startWord: 65,
      wordCount: 5,
      bits: 320,
    },
    {
      name: 'ttl',
      kind: 'ttl',
      label: 'TTL',
      startWord: 70,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'registers',
      kind: 'registers',
      label: 'Registers',
      startWord: indices.registersStart,
      wordCount: registersWordCount,
      bits: registersWordCount * 64,
    },
    {
      name: 'pc',
      kind: 'pc',
      label: 'PC',
      startWord: indices.pc,
      wordCount: 1,
      bits: 64,
    },
    {
      name: 'program',
      kind: 'program',
      label: 'Program',
      startWord: indices.program,
      wordCount: indices.programWords,
      bits: indices.programWords * 64,
    },
  ];
}

export function getValueLayout() {
  return valueLayout;
}

export function setValueLayout(layout) {
  valueLayout = normalizeValueLayout(layout);
}

function getRegionForBit(bit, fields = valueLayout.fields) {
  for (const field of fields) {
    const startBit = field.startWord * 64;
    const endBit = startBit + field.wordCount * 64;
    if (bit >= startBit && bit < endBit) return field;
  }
  return null;
}

function fieldColor(field) {
  return FIELD_COLORS[field?.kind] || FIELD_COLORS.default;
}

function formatHexWord(word) {
  const hex = word.toString(16).padStart(16, '0');
  return `0x${hex}`;
}

function formatDecimalWord(word) {
  return word.toString(10);
}

function formatBigIntWord(word) {
  if (word <= BigInt(Number.MAX_SAFE_INTEGER)) {
    return formatDecimalWord(word);
  }
  return `${formatDecimalWord(word)} (${formatHexWord(word)})`;
}

function formatValueId(word) {
  return BigInt.asUintN(64, word).toString(10);
}

function parseUint64Word(view, offset) {
  if (typeof view.getBigUint64 === 'function') {
    return view.getBigUint64(offset, true);
  }
  const lo = BigInt(view.getUint32(offset, true));
  const hi = BigInt(view.getUint32(offset + 4, true));
  return lo | (hi << 32n);
}

function wordToBytes(word, bytes, offset) {
  let v = BigInt.asUintN(64, word);
  for (let i = 0; i < 8; i++) {
    bytes[offset + i] = Number(v & 0xFFn);
    v >>= 8n;
  }
}

function bytesToBase64(bytes) {
  let binary = '';
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

function base64ToBytes(text) {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function isPrintable(code) {
  return code >= 32 && code < 127;
}

function popcount64(word) {
  let n = BigInt.asUintN(64, word);
  let count = 0;
  while (n !== 0n) {
    n &= n - 1n;
    count++;
  }
  return count;
}

function trimText(text, max = 48) {
  if (!text) return '';
  const runes = [...text];
  if (runes.length <= max) return text;
  return `${runes.slice(0, max).join('')}...`;
}

function readValueWords(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const words = new Array(valueLayout.words);
  for (let i = 0; i < valueLayout.words; i++) {
    words[i] = parseUint64Word(view, i * 8);
  }
  return words;
}

function resolveRegisterName(wordIndex) {
  for (const [name, idx] of Object.entries(valueLayout.registers)) {
    if (idx === wordIndex) return name;
  }
  return null;
}

function decodeOperand(code) {
  const flags = code & 0x3000;
  const idx = code & 0x0FFF;
  const regName = resolveRegisterName(idx);

  if (flags === 0x3000) {
    return regName || `word${idx}`;
  }

  if (flags === 0x2000) {
    return regName ? `*${regName}` : `*word${idx}`;
  }

  return `${code & 0x3FFF}`;
}

function decodeInstruction(instr, slot) {
  const op = instr & 0xF;
  const srcCode = (instr >> 4) & 0x3FFF;
  const dstCode = (instr >> 18) & 0x3FFF;
  const opcodeName = valueLayout.opcodeNames[op] || `op-${op.toString(16)}`;
  return {
    slot,
    op,
    opcodeName,
    srcCode,
    dstCode,
    src: decodeOperand(srcCode),
    dst: decodeOperand(dstCode),
    raw: `0x${instr.toString(16).padStart(8, '0')}`,
    text: `${decodeOperand(srcCode)} ${decodeOperand(dstCode)} ${opcodeName}`,
  };
}

function buildProgramSummary(words) {
  const programWordCount = valueLayout.indices.programWords;
  const programWordStart = valueLayout.indices.program;
  const program = [];
  let nonZero = 0;

  for (let i = 0; i < programWordCount; i++) {
    const word = words[programWordStart + i];
    const low = Number(word & 0xFFFFFFFFn);
    const high = Number((word >> 32n) & 0xFFFFFFFFn);
    const baseSlot = i * 2;
    if (low !== 0) {
      const decoded = decodeInstruction(low, baseSlot);
      decoded.wordIndex = programWordStart + i;
      program.push(decoded);
      if (decoded.op !== 0) nonZero++;
    }
    if (high !== 0) {
      const decoded = decodeInstruction(high, baseSlot + 1);
      decoded.wordIndex = programWordStart + i;
      program.push(decoded);
      if (decoded.op !== 0) nonZero++;
    }
  }

  return { program, nonZero };
}

function hexDump(bytes, limit = 128) {
  const slice = bytes.slice(0, Math.min(bytes.length, limit));
  const rows = [];
  for (let i = 0; i < slice.length; i += 16) {
    const part = slice.slice(i, i + 16);
    const hex = Array.from(part, b => b.toString(16).padStart(2, '0')).join(' ');
    const ascii = Array.from(part, b => (isPrintable(b) ? String.fromCharCode(b) : '.')).join('');
    rows.push(`${i.toString(16).padStart(4, '0')}: ${hex.padEnd(47, ' ')}  ${ascii}`);
  }
  return rows.join('\n');
}

function decodeWireBuffer(input) {
  if (!input) return new Uint8Array(0);
  if (input instanceof Uint8Array) return input;
  if (input instanceof ArrayBuffer) return new Uint8Array(input);
  if (ArrayBuffer.isView(input)) {
    return new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
  }
  if (typeof input === 'string') {
    return base64ToBytes(input);
  }
  return new Uint8Array(0);
}

function decodeFrame(bytes, includeProgram = false) {
  if (!bytes || bytes.length < BYTE_SIZE) {
    return null;
  }

  const words = readValueWords(bytes);
  const indices = valueLayout.indices;

  const tokenChars = [];
  const tokenEntries = [];
  const tokenWordCount = valueLayout.tokenWords;
  for (let i = 0; i < tokenWordCount; i++) {
    const word = words[i];
    if (word === 0n) break;
    const code = Number((word >> 32n) & 0xFFn);
    const seq = Number(word & 0xFFFFFFFFn);
    const ch = isPrintable(code) ? String.fromCharCode(code) : '.';
    tokenChars.push(ch);
    tokenEntries.push({
      index: i,
      sequence: seq,
      code,
      char: ch,
      raw: formatHexWord(word),
    });
  }

  const idWord = words[indices.valueId];
  const prevWord = words[indices.prevId];
  const nextWord = words[indices.nextId];
  const stateWord = words[indices.state];
  const sequenceWord = words[indices.sequence];
  const accumulatorWord = words[indices.accumulator];
  const affinityWord = words[indices.affinity];
  const ttlWord = words[indices.ttl];
  const pcWord = words[indices.pc];
  const execWord = words[indices.execStatus];

  const affinityPop = popcount64(affinityWord);
  const programPop = (() => {
    let total = 0;
    for (let i = 0; i < indices.programWords; i++) {
      total += popcount64(words[indices.program + i]);
    }
    return total;
  })();

  const currentPc = Number(pcWord & 0xFFFFFFFFn);
  const currentInstrWord = words[indices.program + Math.floor(currentPc / 2)] || 0n;
  const currentInstr = Number((currentPc % 2 === 0)
    ? (currentInstrWord & 0xFFFFFFFFn)
    : ((currentInstrWord >> 32n) & 0xFFFFFFFFn));
  const currentOp = currentInstr & 0xF;
  const currentOpName = valueLayout.opcodeNames[currentOp] || `op-${currentOp.toString(16)}`;

  const execStatusCode = Number((execWord >> 48n) & 0xFFFFn);
  const execStatusName = valueLayout.execExitNames[execStatusCode] || `exit-${execStatusCode}`;
  const ttlValue = Number(ttlWord & 0xFFn);

  const summary = [
    `id=${formatValueId(idWord)}`,
    `prev=${formatValueId(prevWord)}`,
    `next=${formatValueId(nextWord)}`,
    `tokens=${JSON.stringify(tokenChars.join(''))}`,
    `op=${currentOpName}`,
    `aff=${formatHexWord(affinityWord)}:${affinityPop}`,
    `pc=${currentPc}`,
    `ttl=${ttlValue}`,
    `prog=${programPop}`,
  ].join(' · ');

  const snapshot = {
    wire: bytesToBase64(bytes),
    byteLength: bytes.length,
    valueId: formatValueId(idWord),
    prevId: formatValueId(prevWord),
    nextId: formatValueId(nextWord),
    tokenText: tokenChars.join(''),
    tokenPreview: trimText(tokenChars.join(''), 64),
    tokenCount: tokenEntries.length,
    tokenEntries,
    state: {
      index: formatHexWord(stateWord),
      sequence: formatHexWord(sequenceWord),
      accumulator: formatHexWord(accumulatorWord),
    },
    affinity: formatHexWord(affinityWord),
    affinityPop,
    gossip: Array.from({ length: indices.gossipWords }, (_, i) => formatHexWord(words[indices.gossip + i] || 0n)),
    ttl: formatHexWord(ttlWord),
    ttlValue,
    registers: {
      r0: formatHexWord(words[valueLayout.registers.r0] || 0n),
      r1: formatHexWord(words[valueLayout.registers.r1] || 0n),
      r2: formatHexWord(words[valueLayout.registers.r2] || 0n),
      r3: formatHexWord(words[valueLayout.registers.r3] || 0n),
      r4: formatHexWord(words[valueLayout.registers.r4] || 0n),
      r5: formatHexWord(words[valueLayout.registers.r5] || 0n),
      fw: formatHexWord(words[valueLayout.registers.fw] || 0n),
      pc: formatHexWord(pcWord),
    },
    pc: formatBigIntWord(pcWord),
    currentOp,
    currentOpName,
    programPop,
    execStatusCode,
    execStatusName,
    summary,
  };

  if (includeProgram) {
    const program = buildProgramSummary(words);
    snapshot.program = program.program;
    snapshot.programSlots = program.program.length;
    snapshot.programNonZero = program.nonZero;
    snapshot.wordHex = words.map(formatHexWord);
    snapshot.rawHexDump = hexDump(bytes, 256);
    snapshot.gossip = snapshot.gossip.map(word => word || '0x0000000000000000');
  }

  return snapshot;
}

export function decodeValueFrame(input) {
  return decodeFrame(decodeWireBuffer(input), false);
}

export function expandValueSnapshot(snapshot) {
  if (!snapshot) return null;
  const bytes = snapshotToUint8Array(snapshot);
  if (!bytes || bytes.length < BYTE_SIZE) {
    return snapshot;
  }
  const expanded = decodeFrame(bytes, true);
  return expanded ? { ...snapshot, ...expanded } : snapshot;
}

export function snapshotToUint8Array(snapshot) {
  if (!snapshot) return new Uint8Array(0);
  if (snapshot.wire) return base64ToBytes(snapshot.wire);
  if (snapshot.bytes instanceof Uint8Array) return snapshot.bytes;
  if (snapshot.bytes instanceof ArrayBuffer) return new Uint8Array(snapshot.bytes);
  if (ArrayBuffer.isView(snapshot.bytes)) {
    return new Uint8Array(snapshot.bytes.buffer, snapshot.bytes.byteOffset, snapshot.bytes.byteLength);
  }
  if (Array.isArray(snapshot.wordHex)) {
    const bytes = new Uint8Array(BYTE_SIZE);
    const view = new DataView(bytes.buffer);
    snapshot.wordHex.forEach((word, idx) => {
      const raw = BigInt(word);
      if (typeof view.setBigUint64 === 'function') {
        view.setBigUint64(idx * 8, raw, true);
      } else {
        wordToBytes(raw, bytes, idx * 8);
      }
    });
    return bytes;
  }
  return new Uint8Array(0);
}

export function updateValueFromWireFrame(wire) {
  const bytes = decodeWireBuffer(wire);
  updateValueFromBinaryBuffer(bytes);
  return bytes;
}

function disposeValueRingGroup(group) {
  if (!group) return;
  const geometries = new Set();
  group.traverse((obj) => {
    if (obj.geometry) geometries.add(obj.geometry);
    if (obj.material) {
      const mats = Array.isArray(obj.material) ? obj.material : [obj.material];
      for (const mat of mats) {
        if (mat && typeof mat.dispose === 'function') mat.dispose();
      }
    }
  });
  for (const geometry of geometries) {
    geometry.dispose();
  }
}

export function buildValueRing() {
  if (valueRingGroup) {
    valueGroup.remove(valueRingGroup);
    disposeValueRingGroup(valueRingGroup);
    valueRingGroup = null;
    regionLabels.length = 0;
  }

  const fields = valueLayout.fields;
  const totalBits = valueLayout.words * 64;
  const step = Math.max(1, Math.floor(totalBits / DISPLAY_BITS));

  valueRingGroup = new THREE.Group();
  const anchor = SYS.machine || SYS.emitter || { x: 0, z: 0 };
  valueRingGroup.position.set(anchor.x, 3.0, anchor.z);

  const cellGeo = new THREE.BoxGeometry(0.06, 0.12, 0.06);
  cellMeshes = [];

  for (let i = 0; i < DISPLAY_BITS; i++) {
    const actualBit = Math.min(totalBits - 1, i * step);
    const field = getRegionForBit(actualBit, fields);
    const angle = (i / DISPLAY_BITS) * Math.PI * 2;
    const fieldOffset = field ? fields.indexOf(field) * 0.11 : 0;
    const radius = 5.5 + fieldOffset;
    const mat = new THREE.MeshBasicMaterial({
      color: fieldColor(field),
      transparent: true,
      opacity: 0.12,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });

    const cell = new THREE.Mesh(cellGeo, mat);
    cell.position.set(Math.cos(angle) * radius, 0, Math.sin(angle) * radius);
    cell.lookAt(0, 0, 0);
    valueRingGroup.add(cell);
    cellMeshes.push({ mesh: cell, bit: actualBit, field, baseOpacity: 0.12 });
  }

  const ringOutlineGeo = new THREE.RingGeometry(5.35, 5.65, 128, 1);
  const ringOutlineMat = new THREE.MeshBasicMaterial({
    color: 0x4080c0,
    transparent: true,
    opacity: 0.06,
    side: THREE.DoubleSide,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });
  const ringOutline = new THREE.Mesh(ringOutlineGeo, ringOutlineMat);
  ringOutline.rotation.x = -Math.PI / 2;
  valueRingGroup.add(ringOutline);

  for (const [idx, field] of fields.entries()) {
    const startBit = field.startWord * 64;
    const endBit = startBit + field.wordCount * 64;
    const midBit = startBit + Math.max(1, endBit - startBit) / 2;
    const angle = (midBit / totalBits) * Math.PI * 2;
    const radius = 6.8 + idx * 0.05;

    const div = document.createElement('div');
    div.className = `region-label ${field.kind}`;
    div.innerHTML = `${field.label}<br><span style="opacity:.55">${field.startWord}-${field.startWord + field.wordCount - 1} · ${field.bits}b</span>`;

    const label = new CSS2DObject(div);
    label.position.set(Math.cos(angle) * radius, 0.3, Math.sin(angle) * radius);
    valueRingGroup.add(label);
    regionLabels.push(label);
  }

  const centerDiv = document.createElement('div');
  centerDiv.className = 'region-label instr';
  centerDiv.textContent = `VALUE · ${valueLayout.byteSize} B`;
  const centerLabel = new CSS2DObject(centerDiv);
  centerLabel.position.set(0, 0.5, 0);
  valueRingGroup.add(centerLabel);

  valueGroup.add(valueRingGroup);
}

export function updateValueFromBinaryBuffer(buf) {
  if (!cellMeshes.length || buf == null) return;

  const bytes = decodeWireBuffer(buf);
  if (bytes.length < BYTE_SIZE) return;

  for (const cell of cellMeshes) {
    const bit = cell.bit;
    const byteIdx = bit >>> 3;
    const bitInByte = bit & 7;
    const active = ((bytes[byteIdx] >> bitInByte) & 1) === 1;
    cell.mesh.material.opacity = active ? 0.78 : 0.08;
    cell.mesh.scale.y = active ? 1.7 : 0.55;
  }
}

export function updateValueDisplay(data) {
  if (!cellMeshes.length) return;

  const dataPop = Number(data?.dataPop || 0);
  const operandPop = Number(data?.operandPop || 0);
  const affinityPop = Number(data?.affinityPop || 0);
  const programPop = Number(data?.programPop || 0);

  for (const cell of cellMeshes) {
    let active = false;
    let intensity = 0.12;

    if (cell.field) {
      switch (cell.field.kind) {
        case 'tokens':
          active = dataPop > 0;
          intensity = active ? 0.42 : 0.08;
          break;
        case 'affinity':
          active = affinityPop > 0;
          intensity = active ? 0.62 : 0.1;
          break;
        case 'program':
          active = programPop > 0 || operandPop > 0;
          intensity = active ? 0.46 : 0.08;
          break;
        case 'exec':
          active = true;
          intensity = 0.78;
          break;
        default:
          active = dataPop > 0;
          intensity = active ? 0.32 : 0.08;
          break;
      }
    }

    cell.mesh.material.opacity = intensity;
    cell.mesh.scale.y = active ? 1.45 : 0.6;
  }
}

export function animateValueRing(time) {
  if (!valueRingGroup) return;
  valueRingGroup.rotation.y = time * 0.0001;
  valueRingGroup.position.y = 3.0 + Math.sin(time * 0.0005) * 0.15;
}
