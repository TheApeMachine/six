```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Kinetic Substrate v3 — Reaction Language</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { background: #010308; overflow: hidden; }
  canvas { display: block; }

  #hud {
    position: fixed; top: 24px; left: 24px;
    font-family: 'Courier New', monospace;
    font-size: 10px; letter-spacing: 0.12em; line-height: 2;
    color: rgba(0,242,255,0.9);
    background: rgba(1,3,12,0.8);
    border: 1px solid rgba(0,242,255,0.18);
    border-left: 2px solid rgba(0,242,255,0.6);
    padding: 14px 20px;
    pointer-events: none;
    min-width: 180px;
  }
  #hud .title {
    font-size: 9px; color: rgba(0,242,255,0.45);
    letter-spacing: 0.2em; margin-bottom: 8px;
    border-bottom: 1px solid rgba(0,242,255,0.1);
    padding-bottom: 6px;
  }
  #hud .row { display: flex; justify-content: space-between; gap: 20px; }
  #hud .key { color: rgba(0,242,255,0.4); }
  #hud .val { color: #fff; }

  #legend {
    position: fixed; bottom: 24px; left: 24px;
    font-family: 'Courier New', monospace;
    font-size: 9px; letter-spacing: 0.1em; line-height: 2.2;
    color: rgba(255,255,255,0.35);
    pointer-events: none;
  }
  .pip {
    display: inline-block; width: 7px; height: 7px;
    border-radius: 50%; margin-right: 7px;
    vertical-align: middle; position: relative; top: -1px;
  }

  #hint {
    position: fixed; bottom: 24px; right: 24px;
    font-family: 'Courier New', monospace;
    font-size: 9px; letter-spacing: 0.1em; line-height: 2;
    color: rgba(255,255,255,0.2);
    pointer-events: none; text-align: right;
  }

  #rule-log {
    position: fixed; top: 24px; right: 24px;
    font-family: 'Courier New', monospace;
    font-size: 9px; letter-spacing: 0.09em; line-height: 1.9;
    color: rgba(255,255,255,0.5);
    background: rgba(1,3,12,0.75);
    border: 1px solid rgba(255,255,255,0.07);
    border-right: 2px solid rgba(255,255,255,0.15);
    padding: 12px 16px;
    pointer-events: none;
    min-width: 220px; max-width: 260px;
    overflow: hidden;
  }
  #rule-log .rl-title {
    font-size: 8px; color: rgba(255,255,255,0.2);
    letter-spacing: 0.2em; margin-bottom: 7px;
    border-bottom: 1px solid rgba(255,255,255,0.07);
    padding-bottom: 5px;
  }
  #rule-log .rl-entry { opacity: 1; transition: opacity 2s; }
  #rule-log .rl-entry.fading { opacity: 0.2; }
  .rl-fire  { color: #ff00aa; }
  .rl-evo   { color: #ffd700; font-weight: bold; }
  .rl-arrow { color: rgba(255,255,255,0.25); }
  .rl-node  { color: rgba(255,255,255,0.3); }
</style>
</head>
<body>

<div id="hud">
  <div class="title">KINETIC SUBSTRATE v3</div>
  <div class="row"><span class="key">NODES</span><span class="val" id="h-nodes">—</span></div>
  <div class="row"><span class="key">TOKENS</span><span class="val" id="h-tokens">—</span></div>
  <div class="row"><span class="key">EDGES</span><span class="val" id="h-edges">—</span></div>
  <div class="row"><span class="key">Ē ENERGY</span><span class="val" id="h-energy">—</span></div>
  <div class="row"><span class="key">RULES ACTIVE</span><span class="val" id="h-rules">—</span></div>
  <div class="row"><span class="key">CHEMISTRIES</span><span class="val" id="h-chem">—</span></div>
  <div class="row"><span class="key">BUFFERED</span><span class="val" id="h-buf">—</span></div>
  <div class="row"><span class="key">EVOLUTIONS</span><span class="val" id="h-evo">—</span></div>
</div>

<div id="legend">
  <div><span class="pip" style="background:#00f2ff;box-shadow:0 0 6px #00f2ff"></span>INERT NODE</div>
  <div><span class="pip" style="background:#ffd700;box-shadow:0 0 6px #ffd700"></span>RULE NODE (1+ laws)</div>
  <div><span class="pip" style="background:#ff00aa;box-shadow:0 0 4px #ff00aa"></span>DATA</div>
  <div><span class="pip" style="background:#00ffaa;box-shadow:0 0 4px #00ffaa"></span>ARCHITECT</div>
  <div><span class="pip" style="background:#ffd700;box-shadow:0 0 4px #ffd700"></span>LAW (carries rule)</div>
  <div><span class="pip" style="background:#ff7700;box-shadow:0 0 4px #ff7700"></span>SIGNAL</div>
  <div><span class="pip" style="background:#aaddff;box-shadow:0 0 4px #aaddff"></span>RESULT</div>
</div>

<div id="rule-log">
  <div class="rl-title">REACTION LOG</div>
  <div id="rl-entries"></div>
</div>

<div id="hint">
  DRAG — orbit<br>
  SCROLL — zoom<br>
  CLICK — spawn token
</div>

<script src="https://cdnjs.cloudflare.com/ajax/libs/three.js/r128/three.min.js"></script>
<script>
// ── RENDERER ─────────────────────────────────────────────────────
const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.setSize(innerWidth, innerHeight);
renderer.setClearColor(0x010308, 1);
document.body.appendChild(renderer.domElement);

const scene = new THREE.Scene();
scene.fog = new THREE.FogExp2(0x010308, 0.00055);

const camera = new THREE.PerspectiveCamera(55, innerWidth / innerHeight, 0.5, 8000);
camera.position.set(0, -350, 720);

// ── GLOW TEXTURE FACTORY ─────────────────────────────────────────
const glowCache = {};
function makeGlowTex(r, g, b) {
  const key = `${r},${g},${b}`;
  if (glowCache[key]) return glowCache[key];
  const sz = 128;
  const c = document.createElement('canvas');
  c.width = c.height = sz;
  const ctx = c.getContext('2d');
  const grad = ctx.createRadialGradient(sz/2, sz/2, 0, sz/2, sz/2, sz/2);
  grad.addColorStop(0,    `rgba(${r},${g},${b},1)`);
  grad.addColorStop(0.25, `rgba(${r},${g},${b},0.7)`);
  grad.addColorStop(0.6,  `rgba(${r},${g},${b},0.2)`);
  grad.addColorStop(1,    `rgba(${r},${g},${b},0)`);
  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, sz, sz);
  const t = new THREE.CanvasTexture(c);
  glowCache[key] = t;
  return t;
}

const GLOW = {
  cyan:  makeGlowTex(0, 242, 255),
  gold:  makeGlowTex(255, 210, 0),
  pink:  makeGlowTex(255, 0, 170),
  green: makeGlowTex(0, 255, 170),
  orange:makeGlowTex(255, 120, 0),
  white: makeGlowTex(180, 220, 255),
};

// ── STARFIELD ─────────────────────────────────────────────────────
{
  const N = 2500;
  const pos = new Float32Array(N * 3);
  const col = new Float32Array(N * 3);
  for (let i = 0; i < N; i++) {
    const r = 3000 + Math.random() * 4000;
    const theta = Math.random() * Math.PI * 2;
    const phi   = Math.acos(2 * Math.random() - 1);
    pos[i*3]   = r * Math.sin(phi) * Math.cos(theta);
    pos[i*3+1] = r * Math.sin(phi) * Math.sin(theta);
    pos[i*3+2] = r * Math.cos(phi);
    const bright = 0.2 + Math.random() * 0.5;
    const tint = Math.random();
    col[i*3]   = bright * (0.7 + tint * 0.3);
    col[i*3+1] = bright * (0.8 + tint * 0.1);
    col[i*3+2] = bright;
  }
  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  geo.setAttribute('color',    new THREE.BufferAttribute(col, 3));
  scene.add(new THREE.Points(geo, new THREE.PointsMaterial({
    vertexColors: true, size: 1.8, sizeAttenuation: true,
    transparent: true, opacity: 0.6, depthWrite: false,
  })));
}

// ── SIMULATION STATE ──────────────────────────────────────────────
let nodes  = [];
let tokens = [];
let frame  = 0;
let evolutionCount = 0;

// ── REACTION RULE SYSTEM ──────────────────────────────────────────
// Single-token rules: one input consumed, one output emitted
// Binary rules: two buffered inputs consumed, one output emitted
// cost: energy required to fire. If insufficient, token is re-routed.

const SINGLE_RULES = [
  { inputs: ['DATA'],      output: 'RESULT',    cost: 1 },  // basic processing
  { inputs: ['DATA'],      output: 'SIGNAL',    cost: 1 },  // amplification
  { inputs: ['SIGNAL'],    output: 'DATA',      cost: 1 },  // demodulation
  { inputs: ['RESULT'],    output: 'ARCHITECT', cost: 2 },  // bootstrapping (expensive)
  { inputs: ['RESULT'],    output: 'DATA',      cost: 1 },  // recycling
  { inputs: ['ARCHITECT'], output: 'RESULT',    cost: 1 },  // meta-output
  { inputs: ['DATA'],      output: 'ARCHITECT', cost: 2 },  // direct build (expensive)
  { inputs: ['RESULT'],    output: 'LAW',       cost: 2 },  // evolution (expensive but reachable)
];

const BINARY_RULES = [
  { inputs: ['DATA',   'SIGNAL'],    output: 'RESULT',    cost: 2 },  // catalysis
  { inputs: ['RESULT', 'DATA'],      output: 'ARCHITECT', cost: 3 },  // assembly
  { inputs: ['DATA',   'DATA'],      output: 'ARCHITECT', cost: 2 },  // fusion
  { inputs: ['SIGNAL', 'SIGNAL'],    output: 'LAW',       cost: 4 },  // resonance → law
  { inputs: ['RESULT', 'RESULT'],    output: 'LAW',       cost: 3 },  // synthesis → law
  { inputs: ['ARCHITECT', 'DATA'],   output: 'RESULT',    cost: 2 },  // digestion
];

const ALL_RULES = [...SINGLE_RULES, ...BINARY_RULES];

// Color identity per token kind
const KIND_COLOR = {
  DATA:      new THREE.Color(1.0, 0.0,  0.67),
  SIGNAL:    new THREE.Color(1.0, 0.47, 0.0),
  RESULT:    new THREE.Color(0.7, 0.93, 1.0),
  ARCHITECT: new THREE.Color(0.0, 1.0,  0.67),
  LAW:       new THREE.Color(1.0, 0.82, 0.0),
};

function ruleKey(r) { return r.inputs.join('+') + '→' + r.output; }
function isBinary(r) { return r.inputs.length === 2; }

function randomRule() {
  // Bias toward single rules (cheaper, more common as LAW payloads)
  return Math.random() < 0.3
    ? BINARY_RULES[Math.floor(Math.random() * BINARY_RULES.length)]
    : SINGLE_RULES[Math.floor(Math.random() * SINGLE_RULES.length)];
}

// Reaction log
const rlEntries = document.getElementById('rl-entries');
const MAX_LOG = 8;
function logReaction(nodeIdx, rule, isEvo) {
  const cls = isEvo ? 'rl-evo' : 'rl-fire';
  const lhs = rule.inputs.join('<span class="rl-arrow">+</span>');
  const entry = document.createElement('div');
  entry.className = 'rl-entry';
  entry.innerHTML =
    `<span class="rl-node">[N${nodeIdx}]</span> ` +
    `<span class="${cls}">${lhs}</span>` +
    `<span class="rl-arrow"> → </span>` +
    `<span class="${cls}">${rule.output}</span>` +
    `<span class="rl-node"> -${rule.cost}E</span>`;
  rlEntries.prepend(entry);
  while (rlEntries.children.length > MAX_LOG) rlEntries.removeChild(rlEntries.lastChild);
  Array.from(rlEntries.children).forEach((el, i) => {
    el.style.opacity = Math.max(0.15, 1 - i * 0.12);
  });
}

// Shared geo
const coreGeoNode = new THREE.BoxGeometry(13, 13, 13);
const coreGeoRule = new THREE.BoxGeometry(19, 19, 19);
const coreGeoSm   = new THREE.BoxGeometry(9, 9, 9);

// ── REACTION NODE ─────────────────────────────────────────────────
class ReactionNode {
  constructor(x, y, z) {
    this.pos    = new THREE.Vector3(x, y, z);
    this.vel    = new THREE.Vector3();
    this.edges  = [];
    this.energy = 0;
    this.rules  = [];        // installed reaction rules
    this.buffer = [];        // pending token kinds waiting for binary match
    this.bufferAge = [];     // frame each buffer slot was filled (for expiry)
    this.traffic = 0;
    this.birthFrame = frame;
    this._nodeIdx = nodes.length;

    this.coreMat = new THREE.MeshBasicMaterial({
      color: 0x00f2ff, wireframe: true, transparent: true, opacity: 0.85,
    });
    this.core = new THREE.Mesh(coreGeoNode, this.coreMat);
    scene.add(this.core);

    this.fill = new THREE.Mesh(coreGeoSm, new THREE.MeshBasicMaterial({
      color: 0x003344, transparent: true, opacity: 0.5,
    }));
    scene.add(this.fill);

    this.halo = new THREE.Sprite(new THREE.SpriteMaterial({
      map: GLOW.cyan, blending: THREE.AdditiveBlending,
      transparent: true, opacity: 0.45, depthWrite: false,
    }));
    scene.add(this.halo);
  }

  get hasLaw() { return this.rules.length > 0; }

  connect(other) {
    if (other && other !== this && !this.edges.includes(other))
      this.edges.push(other);
  }

  installRule(rule) {
    if (this.rules.some(r => ruleKey(r) === ruleKey(rule))) return;
    if (this.rules.length >= 3) this.rules.shift();
    this.rules.push(rule);
    this._refreshVisual();
  }

  _refreshVisual() {
    if (this.rules.length === 0) {
      this.coreMat.color.set(0x00f2ff);
      this.core.geometry = coreGeoNode;
      this.halo.material.map = GLOW.cyan;
    } else {
      const blend = new THREE.Color(0, 0, 0);
      for (const r of this.rules) {
        blend.add(KIND_COLOR[r.inputs[0]] || KIND_COLOR.DATA);
      }
      blend.multiplyScalar(1 / this.rules.length);
      this.coreMat.color.copy(blend);
      this.core.geometry = coreGeoRule;
      const dominant = this.rules[this.rules.length - 1];
      const glowMap = {
        DATA: GLOW.pink, SIGNAL: GLOW.orange, RESULT: GLOW.white,
        ARCHITECT: GLOW.green, LAW: GLOW.gold,
      };
      this.halo.material.map = glowMap[dominant.inputs[0]] || GLOW.cyan;
      this.halo.material.color.copy(blend);
    }
    this.halo.material.needsUpdate = true;
  }

  // Expire stale buffer tokens (stuck waiting > 120 frames)
  _tickBuffer() {
    for (let i = this.buffer.length - 1; i >= 0; i--) {
      if (frame - this.bufferAge[i] > 120) {
        this.buffer.splice(i, 1);
        this.bufferAge.splice(i, 1);
      }
    }
  }

  handleArrival(token) {
    this.traffic++;
    this._tickBuffer();

    // ── LAW: install rule payload ──────────────────────────────────
    if (token.kind === 'LAW') {
      const rule = token.rule || randomRule();
      this.installRule(rule);
      token.dead = true;
      return;
    }

    // ── ARCHITECT: structural expansion ───────────────────────────
    if (token.kind === 'ARCHITECT') {
      if (this.energy >= 5) {
        this.expand();
        this.energy -= 5;
        token.dead = true;
        return;
      }
      // Not enough energy — fall through to re-route
    } else {

      // ── BINARY RULE CHECK ──────────────────────────────────────
      // Does this token + something already in the buffer satisfy a rule?
      const binaryMatch = this.rules.find(r =>
        isBinary(r) &&
        this.energy >= r.cost &&
        r.inputs.includes(token.kind) &&
        this.buffer.includes(r.inputs.find(k => k !== token.kind))
      );

      if (binaryMatch) {
        // Consume the matching buffer slot
        const partnerKind = binaryMatch.inputs.find(k => k !== token.kind);
        const bufIdx = this.buffer.indexOf(partnerKind);
        this.buffer.splice(bufIdx, 1);
        this.bufferAge.splice(bufIdx, 1);

        this.energy -= binaryMatch.cost;
        this._fireRule(binaryMatch, token);
        return;
      }

      // ── SINGLE RULE CHECK ──────────────────────────────────────
      const singleMatch = this.rules.find(r =>
        !isBinary(r) &&
        r.inputs[0] === token.kind &&
        this.energy >= r.cost
      );

      if (singleMatch) {
        this.energy -= singleMatch.cost;
        this._fireRule(singleMatch, token);
        return;
      }

      // ── BUFFER: hold token hoping for a binary match later ─────
      const wantedByBinary = this.rules.some(r =>
        isBinary(r) && r.inputs.includes(token.kind)
      );
      if (wantedByBinary && this.buffer.length < 3) {
        this.buffer.push(token.kind);
        this.bufferAge.push(frame);
        token.dead = true;  // absorbed into buffer
        return;
      }

      // No rule match, no buffer interest — passively charge energy
      if (token.kind === 'DATA')   this.energy = Math.min(this.energy + 0.5, 10);
      if (token.kind === 'SIGNAL') this.energy = Math.min(this.energy + 0.3, 10);
      if (token.kind === 'RESULT') this.energy = Math.min(this.energy + 0.4, 10);
    }

    // Re-route surviving token
    if (this.edges.length > 0)
      token.setTarget(this.edges[Math.floor(Math.random() * this.edges.length)]);
    else token.dead = true;
  }

  _fireRule(rule, token) {
    const isEvo = rule.output === 'LAW';
    // Firing a rule is itself an energy source (reactions are exothermic)
    // but we already deducted cost; net effect is positive only if cost < gain
    // gain is modest — keeps high-traffic nodes alive, not infinitely rich
    this.energy = Math.min(this.energy + 0.4, 10);

    if (isEvo) {
      evolutionCount++;
      const newRule = Math.random() < 0.35
        ? randomRule()
        : { ...rule, output: ALL_RULES[Math.floor(Math.random() * ALL_RULES.length)].output };
      const lawToken = new Token(this, 'LAW');
      lawToken.rule = newRule;
      tokens.push(lawToken);
      logReaction(this._nodeIdx, { inputs: rule.inputs, output: newRule.output, cost: rule.cost }, true);
    } else {
      tokens.push(new Token(this, rule.output));
      logReaction(this._nodeIdx, rule, false);
    }
    token.dead = true;
  }

  expand() {
    const dir = new THREE.Vector3(
      Math.random()-0.5, Math.random()-0.5, Math.random()-0.5
    ).normalize().multiplyScalar(130 + Math.random() * 60);

    const nw = new ReactionNode(
      this.pos.x + dir.x, this.pos.y + dir.y, this.pos.z + dir.z
    );
    nw._nodeIdx = nodes.length;
    nodes.push(nw);
    this.connect(nw); nw.connect(this);

    // Inherit one rule from parent — chemistry propagates through growth
    if (this.rules.length > 0) {
      const inherited = this.rules[Math.floor(Math.random() * this.rules.length)];
      nw.installRule({ ...inherited });
    }

    let closest = null, minD = Infinity;
    for (const n of nodes) {
      if (n === nw || n === this) continue;
      const d = nw.pos.distanceTo(n.pos);
      if (d < minD) { minD = d; closest = n; }
    }
    if (closest && minD < 450) { nw.connect(closest); closest.connect(nw); }

    for (let i = 0; i < 2; i++) tokens.push(new Token(nw, 'DATA'));
  }

  update() {
    const age    = frame - this.birthFrame;
    const appear = Math.min(age / 20, 1);

    // Energy starvation: nodes with no energy flicker visibly
    const energyRatio = this.energy / 10;
    const starved = this.energy < 1.0;
    const flicker = starved
      ? 0.4 + 0.4 * Math.sin(frame * 0.3 + this._nodeIdx)
      : 1.0;

    const rotSpeed = this.hasLaw ? 0.016 : 0.006;
    this.core.rotation.y += rotSpeed * (starved ? 0.3 : 1.0); // slow rotation when starved
    this.core.rotation.x += rotSpeed * 0.65 * (starved ? 0.3 : 1.0);
    this.fill.rotation.copy(this.core.rotation);

    const pulse = 1 + Math.sin(frame * 0.06 + this.pos.x * 0.01) * 0.06;
    const es    = 1 + this.energy * 0.035;
    const s     = appear * pulse * es * flicker;
    this.core.scale.setScalar(s);
    this.fill.scale.setScalar(s * 0.8);

    // Buffer indicator: slightly enlarge core when holding buffered tokens
    if (this.buffer.length > 0) {
      const bufPulse = 1 + this.buffer.length * 0.08 * Math.sin(frame * 0.15);
      this.core.scale.multiplyScalar(bufPulse);
    }

    this.core.position.copy(this.pos);
    this.fill.position.copy(this.pos);

    const haloS = (55 + this.energy * 15 + this.rules.length * 20) * appear * flicker;
    this.halo.scale.setScalar(haloS);
    this.halo.position.copy(this.pos);
    this.halo.material.opacity =
      (0.15 + energyRatio * 0.25 + this.rules.length * 0.06) * appear * flicker;

    // Inert nodes dim toward grey when energy-starved
    if (!this.hasLaw) {
      const bright = 0.3 + energyRatio * 0.5;
      this.coreMat.color.setRGB(bright * 0.15, bright * 0.85, bright);
    } else {
      // Rule nodes: tint shifts toward grey when starved
      this.coreMat.opacity = 0.4 + energyRatio * 0.45;
    }
  }

  dispose() {
    scene.remove(this.core);
    scene.remove(this.fill);
    scene.remove(this.halo);
  }
}

// ── TOKEN ─────────────────────────────────────────────────────────
const TOKEN_META = {
  LAW:       { col: new THREE.Color(1.0, 0.84, 0.0), glow: 'gold',   geo: 'box',    sz: 7,  trailLen: 16 },
  ARCHITECT: { col: new THREE.Color(0.0, 1.0, 0.67), glow: 'green',  geo: 'sphere', sz: 5,  trailLen: 14 },
  DATA:      { col: new THREE.Color(1.0, 0.0, 0.67), glow: 'pink',   geo: 'sphere', sz: 4,  trailLen: 10 },
  SIGNAL:    { col: new THREE.Color(1.0, 0.50, 0.0), glow: 'orange', geo: 'sphere', sz: 4,  trailLen: 12 },
  RESULT:    { col: new THREE.Color(0.7, 0.93, 1.0), glow: 'white',  geo: 'sphere', sz: 3.5,trailLen: 8  },
};

const sharedGeos = {
  box:    new THREE.BoxGeometry(1, 1, 1),
  sphere: new THREE.SphereGeometry(1, 7, 7),
};

class Token {
  constructor(origin, kind) {
    this.kind = kind;
    this.rule = null;  // payload: set for LAW tokens carrying a reaction rule
    this.dead = false;
    this.pos  = origin.pos.clone();
    this.startPos = this.pos.clone();
    this.target = origin.edges.length > 0
      ? origin.edges[Math.floor(Math.random() * origin.edges.length)]
      : null;
    this.progress = 0;
    if (!this.target) { this.dead = true; return; }

    const meta = TOKEN_META[kind] || TOKEN_META.DATA;
    this.trailLen = meta.trailLen;
    this.trail = [];

    this.mesh = new THREE.Mesh(
      sharedGeos[meta.geo],
      new THREE.MeshBasicMaterial({ color: meta.col })
    );
    this.mesh.scale.setScalar(meta.sz);
    scene.add(this.mesh);

    this.sprite = new THREE.Sprite(new THREE.SpriteMaterial({
      map: GLOW[meta.glow],
      color: meta.col,
      blending: THREE.AdditiveBlending,
      transparent: true, opacity: 0.9, depthWrite: false,
    }));
    this.sprite.scale.setScalar(meta.sz * 6);
    scene.add(this.sprite);

    const trailPos = new Float32Array(this.trailLen * 3);
    const tgeo = new THREE.BufferGeometry();
    tgeo.setAttribute('position', new THREE.BufferAttribute(trailPos, 3));
    tgeo.setDrawRange(0, 0);
    this.trail3d = new THREE.Line(tgeo, new THREE.LineBasicMaterial({
      color: meta.col,
      transparent: true, opacity: 0.5,
      blending: THREE.AdditiveBlending, depthWrite: false,
    }));
    scene.add(this.trail3d);
  }

  setTarget(node) {
    this.startPos.copy(this.pos);
    this.target = node;
    this.progress = 0;
  }

  update() {
    if (this.dead || !this.target) return;

    this.trail.unshift(this.pos.clone());
    if (this.trail.length > this.trailLen) this.trail.pop();

    this.progress += 0.048 + Math.random() * 0.008;
    const t = Math.min(this.progress, 1);
    this.pos.lerpVectors(this.startPos, this.target.pos, t);

    if (this.progress >= 1) {
      this.target.handleArrival(this);
      if (this.progress >= 1) this.dead = true;
    }

    if (this.dead) return;

    this.mesh.position.copy(this.pos);
    this.sprite.position.copy(this.pos);

    const pa = this.trail3d.geometry.attributes.position.array;
    const n  = this.trail.length;
    for (let i = 0; i < n; i++) {
      pa[i*3]   = this.trail[i].x;
      pa[i*3+1] = this.trail[i].y;
      pa[i*3+2] = this.trail[i].z;
    }
    this.trail3d.geometry.attributes.position.needsUpdate = true;
    this.trail3d.geometry.setDrawRange(0, n);
  }

  dispose() {
    if (!this.mesh) return;
    scene.remove(this.mesh);
    scene.remove(this.sprite);
    scene.remove(this.trail3d);
    this.mesh.material.dispose();
    this.sprite.material.dispose();
    this.trail3d.geometry.dispose();
    this.trail3d.material.dispose();
  }
}

// ── EDGE RENDERER ─────────────────────────────────────────────────
const MAX_EDGES = 2000;
const edgePosArr = new Float32Array(MAX_EDGES * 2 * 3);
const edgeColArr = new Float32Array(MAX_EDGES * 2 * 3);
const edgeGeo = new THREE.BufferGeometry();
edgeGeo.setAttribute('position', new THREE.BufferAttribute(edgePosArr, 3));
edgeGeo.setAttribute('color',    new THREE.BufferAttribute(edgeColArr, 3));
const edgeLines = new THREE.LineSegments(edgeGeo, new THREE.LineBasicMaterial({
  vertexColors: true, transparent: true, opacity: 0.35,
  blending: THREE.AdditiveBlending, depthWrite: false,
}));
scene.add(edgeLines);

function updateEdges() {
  let vi = 0;
  const seen = new Set();
  for (const n of nodes) {
    for (const e of n.edges) {
      const ia = nodes.indexOf(n), ib = nodes.indexOf(e);
      const key = ia < ib ? `${ia}-${ib}` : `${ib}-${ia}`;
      if (seen.has(key) || vi >= MAX_EDGES) continue;
      seen.add(key);

      edgePosArr[vi*6]   = n.pos.x; edgePosArr[vi*6+1] = n.pos.y; edgePosArr[vi*6+2] = n.pos.z;
      edgePosArr[vi*6+3] = e.pos.x; edgePosArr[vi*6+4] = e.pos.y; edgePosArr[vi*6+5] = e.pos.z;

      const traffic = Math.min((n.traffic + e.traffic) * 0.015, 0.8);
      // Edges between rule-carrying nodes glow with their chemistry blend
      const nRules = n.rules.length, eRules = e.rules.length;
      const ruleTint = Math.min((nRules + eRules) * 0.15, 0.6);
      // Sample dominant rule color from the more-developed endpoint
      const dominant = nRules >= eRules ? n : e;
      let cr = 0, cg = traffic * 0.85 + 0.06, cb = traffic + 0.12;
      if (dominant.rules.length > 0) {
        const rc = KIND_COLOR[dominant.rules[dominant.rules.length-1].inputs[0]];
        cr = rc.r * ruleTint + traffic * 0.05;
        cg = rc.g * ruleTint * 0.6 + traffic * 0.5 + 0.04;
        cb = rc.b * ruleTint * 0.5 + traffic * 0.4 + 0.08;
      }

      edgeColArr[vi*6]   = cr; edgeColArr[vi*6+1] = cg; edgeColArr[vi*6+2] = cb;
      edgeColArr[vi*6+3] = cr; edgeColArr[vi*6+4] = cg; edgeColArr[vi*6+5] = cb;
      vi++;
    }
  }
  // blank remainder
  for (let i = vi; i < Math.min(vi + 4, MAX_EDGES); i++) {
    edgePosArr.fill(0, i*6, i*6+6);
  }
  edgeGeo.attributes.position.needsUpdate = true;
  edgeGeo.attributes.color.needsUpdate = true;
  edgeGeo.setDrawRange(0, vi * 2);
}

// ── PHYSICS ───────────────────────────────────────────────────────
const _f  = new THREE.Vector3();
const _d  = new THREE.Vector3();
function rebalance() {
  for (const n1 of nodes) {
    _f.set(0, 0, 0);

    // Repulsion
    for (const n2 of nodes) {
      if (n1 === n2) continue;
      _d.subVectors(n1.pos, n2.pos);
      const dist = _d.length();
      if (dist < 380 && dist > 0.5) {
        _f.addScaledVector(_d.normalize(), 110 / (dist * 0.18 + 1));
      }
    }

    // Spring attraction toward neighbors
    for (const e of n1.edges) {
      _d.subVectors(e.pos, n1.pos);
      const dist = _d.length();
      const restLen = 190;
      _f.addScaledVector(_d.normalize(), (dist - restLen) * 0.042);
    }

    // Soft gravity toward origin
    _f.addScaledVector(n1.pos, -0.0045);

    n1.vel.addScaledVector(_f, 1.0);
    n1.vel.multiplyScalar(0.32); // syrup damping
    if (n1.vel.length() > 6) n1.vel.setLength(6);
    if (n1.vel.length() > 0.08) n1.pos.add(n1.vel);
  }
}

// ── CAMERA ────────────────────────────────────────────────────────
let camTheta  = 0.0;
let camPhi    = 1.1;   // slightly above equator
let camRadius = 720;
let autoOrbit = true;
let isDragging = false;
let prevMouse  = {x:0, y:0};
let lastAction = 0;

const camTarget = new THREE.Vector3();
const camSmooth = new THREE.Vector3();

renderer.domElement.addEventListener('mousedown', e => {
  isDragging = true; autoOrbit = false;
  prevMouse = {x: e.clientX, y: e.clientY};
  lastAction = Date.now();
});
window.addEventListener('mousemove', e => {
  if (!isDragging) return;
  camTheta -= (e.clientX - prevMouse.x) * 0.006;
  camPhi    = Math.max(0.15, Math.min(Math.PI - 0.15,
                camPhi + (e.clientY - prevMouse.y) * 0.006));
  prevMouse = {x: e.clientX, y: e.clientY};
  lastAction = Date.now();
});
window.addEventListener('mouseup', () => { isDragging = false; });
renderer.domElement.addEventListener('wheel', e => {
  camRadius = Math.max(200, Math.min(2200, camRadius + e.deltaY * 0.6));
  lastAction = Date.now();
}, {passive:true});

// Click to spawn token
renderer.domElement.addEventListener('click', e => {
  if (nodes.length === 0) return;
  const origin = nodes[Math.floor(Math.random() * nodes.length)];
  const types = ['DATA','DATA','DATA','SIGNAL','ARCHITECT','LAW'];
  const kind  = types[Math.floor(Math.random() * types.length)];
  const tok   = new Token(origin, kind);
  if (kind === 'LAW') tok.rule = randomRule();
  tokens.push(tok);
});

function updateCamera(com) {
  if (!isDragging && Date.now() - lastAction > 2500) autoOrbit = true;
  if (autoOrbit) camTheta += 0.0025;

  const sp = Math.sin(camPhi), cp = Math.cos(camPhi);
  const st = Math.sin(camTheta), ct = Math.cos(camTheta);
  const cx = camRadius * sp * st;
  const cy = camRadius * cp;
  const cz = camRadius * sp * ct;

  camTarget.copy(com);
  camSmooth.lerp(camTarget, 0.018);

  camera.position.set(camSmooth.x + cx, camSmooth.y + cy, camSmooth.z + cz);
  camera.lookAt(camSmooth);
}

// ── SPAWN ─────────────────────────────────────────────────────────
function spawnToken() {
  if (nodes.length === 0) return;
  const origin = nodes[Math.floor(Math.random() * nodes.length)];
  if (!origin || origin.edges.length === 0) return;
  const rnd = Math.random();
  let type;
  if      (rnd < 0.07)  type = 'LAW';
  else if (rnd < 0.16)  type = 'ARCHITECT';
  else if (rnd < 0.28)  type = 'SIGNAL';
  else                  type = 'DATA';

  const t = new Token(origin, type);
  // LAW tokens always carry a reaction rule payload
  if (type === 'LAW') t.rule = randomRule();
  tokens.push(t);
}

// ── INIT ──────────────────────────────────────────────────────────
function init() {
  const positions = [
    [-110,  0,   0],
    [ 110,  0,   0],
    [   0,  0, 160],
    [   0, 120,  60],
  ];
  for (const [x,y,z] of positions) nodes.push(new ReactionNode(x,y,z));

  for (let i = 0; i < nodes.length; i++)
    for (let j = 0; j < nodes.length; j++)
      if (i !== j) nodes[i].connect(nodes[j]);

  // Seed distinct starter rules — immediately creates chemical divergence
  nodes[0].installRule({ inputs: ['DATA'],   output: 'RESULT',    cost: 1 });
  nodes[1].installRule({ inputs: ['SIGNAL'], output: 'DATA',      cost: 1 });
  nodes[2].installRule({ inputs: ['RESULT'], output: 'ARCHITECT', cost: 2 });
  nodes[3].installRule({ inputs: ['DATA', 'SIGNAL'], output: 'RESULT', cost: 2 });
  // Seed the evolutionary rule — without this it almost never gets installed naturally
  nodes[2].installRule({ inputs: ['RESULT'], output: 'LAW', cost: 2 });
  nodes[0].energy = 3;
}
init();

// ── HUD ───────────────────────────────────────────────────────────
const hNodes  = document.getElementById('h-nodes');
const hTokens = document.getElementById('h-tokens');
const hEdges  = document.getElementById('h-edges');
const hEnergy = document.getElementById('h-energy');
const hRules  = document.getElementById('h-rules');
const hChem   = document.getElementById('h-chem');
const hBuf    = document.getElementById('h-buf');
const hEvo    = document.getElementById('h-evo');

function updateHUD() {
  let edgeCount = 0, totalEnergy = 0, totalRules = 0, totalBuf = 0;
  const chemistries = new Set();
  for (const n of nodes) {
    edgeCount += n.edges.length;
    totalEnergy += n.energy;
    totalRules += n.rules.length;
    totalBuf += n.buffer.length;
    for (const r of n.rules) chemistries.add(ruleKey(r));
  }
  const avgE = nodes.length > 0 ? (totalEnergy / nodes.length).toFixed(2) : '—';
  hNodes.textContent  = nodes.length;
  hTokens.textContent = tokens.length;
  hEdges.textContent  = (edgeCount / 2).toFixed(0);
  hEnergy.textContent = avgE;
  hRules.textContent  = totalRules;
  hChem.textContent   = chemistries.size + ' / ' + ALL_RULES.length;
  hBuf.textContent    = totalBuf;
  hEvo.textContent    = evolutionCount;
}

// ── ANIMATE ───────────────────────────────────────────────────────
const _com = new THREE.Vector3();

function animate() {
  requestAnimationFrame(animate);
  frame++;

  rebalance();

  // Center of mass
  _com.set(0,0,0);
  for (const n of nodes) _com.add(n.pos);
  if (nodes.length) _com.divideScalar(nodes.length);

  updateCamera(_com);

  // Update nodes
  for (const n of nodes) n.update();

  // Energy decay every ~3 seconds
  if (frame % 190 === 0)
    for (const n of nodes) n.energy = Math.max(0, n.energy - 0.25);

  // Traffic decay
  if (frame % 300 === 0)
    for (const n of nodes) n.traffic = Math.max(0, n.traffic - 1);

  // Token step
  for (let i = tokens.length - 1; i >= 0; i--) {
    const t = tokens[i];
    t.update();
    if (t.dead) { t.dispose(); tokens.splice(i, 1); }
  }

  // Spawn
  if (frame % 32 === 0 && tokens.length < 70) spawnToken();

  updateEdges();
  if (frame % 12 === 0) updateHUD();

  renderer.render(scene, camera);
}
animate();

// ── RESIZE ───────────────────────────────────────────────────────
window.addEventListener('resize', () => {
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(innerWidth, innerHeight);
});
</script>
</body>
</html>
```