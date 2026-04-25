/*
fieldRenderer drives the live field visualisation through WebGL2 with
instanced rendering. The whole scene — up to ~200k Values plus their
field anchors — collapses into a fixed number of draw calls per frame:
one for anchor halos, one for particle glyphs, one for selection-chain
edges, plus a 2D overlay for captions. Per-particle state lives in a
single interleaved Float32Array so resyncs touch contiguous memory and
the GPU upload is one bufferSubData. A dirty-frame loop keeps idle CPU
near zero between snapshot ticks.
*/

import type {
	FieldSnapshot,
	FieldValueSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";
import {
	categoryForProgram,
	PROGRAM_CATEGORIES,
	type ProgramCategory,
	type Shape,
} from "@/lib/programClassifier";

const TAU = Math.PI * 2;

const MAX_PARTICLES = 200_000;

/** Per-instance attribute layout (interleaved Float32).
 *
 * Animation (flash + resonance ease) lives entirely in the vertex shader,
 * driven by `u_time`. The CPU only writes start timestamps + endpoints when
 * a particle's state changes, so render frames cost zero buffer uploads.
 */
const STRIDE_F = 12;
//   0  posX
//   1  posY
//   2  size           (pixels at zoom=1)
//   3  shape          (Shape index, see SHAPE_INDEX)
//   4  r
//   5  g
//   6  b
//   7  resPrev        (resonance value at resStart, 0..1)
//   8  resTarget      (resonance target after ease, 0..1)
//   9  state          (bitfield: HYP|FAL|INTERV|SELECTED)
//  10  resStart       (ms timestamp when resonance ease began)
//  11  flashStart     (ms timestamp when last program-swap flash began)
const STRIDE_B = STRIDE_F * 4;

const STATE_OFFSET_F = 9;

const STATE_HYP = 1;
const STATE_FAL = 2;
const STATE_INTERV = 4;
const STATE_SELECTED = 8;
const STATE_PROMPT = 16;

const PARTICLE_BASE_SIZE = 7; // pixel half-size of glyph quad (before halo expansion)
const QUAD_HALO_FACTOR = 3.2; // expand quad to fit largest halo / flash

const MIN_FIELD_DISTANCE = 220;
const FIELD_REPEL_PASSES = 24;
const ANCHOR_BASE_RADIUS = 32; // world units before per-member growth
/*
ANCHOR_PER_MEMBER_SPIRAL is tuned to make the sunflower layout's
nearest-neighbour distance (~R/√N) exceed roughly 2·PARTICLE_BASE_SIZE,
so anchors with many members grow large enough that individual particles
stay clickable. 14 ≈ 2 × particle radius (PARTICLE_BASE_SIZE = 7) with
a small safety margin for the halo.
*/
const ANCHOR_PER_MEMBER_SPIRAL = 14;

const FLASH_DURATION_MS = 450; // flash fully decays in ~0.45s
const RES_EASE_DURATION_MS = 220; // resonance smoothstep eases over ~0.22s

const SPATIAL_CELL = 80; // world units per hit-test grid cell
// Power-of-2 bucket count so we can mask instead of modulo.
const SPATIAL_BUCKETS = 16384;
const SPATIAL_BUCKET_MASK = SPATIAL_BUCKETS - 1;
// Cell hash multipliers (Teschner et al., "Optimized Spatial Hashing").
const SPATIAL_HASH_X = 73856093;
const SPATIAL_HASH_Y = 19349663;

const MAX_ANCHORS = 4096;
const ANCHOR_STRIDE_F = 8;
//   0 posX  1 posY  2 radius  3 r  4 g  5 b  6 alpha  7 reserved
const ANCHOR_STRIDE_B = ANCHOR_STRIDE_F * 4;

/** Stable shape → integer for the fragment shader switch. */
const SHAPE_INDEX: Record<Shape, number> = {
	square: 0,
	circle: 1,
	ring: 2,
	triangle_up: 3,
	triangle_down: 4,
	diamond: 5,
	pentagon: 6,
	hourglass: 7,
	asterisk: 8,
	bar: 9,
	concentric: 10,
};

function sortByPosX(a: { posX: number }, b: { posX: number }): number {
	return a.posX - b.posX;
}

/*
drawCaptionPill paints a small rounded backdrop behind the next text
draw call so labels stay legible against bright clusters of particles.
The caller still owns the textAlign + font + fillStyle for the actual
text — we just render the pill, then the caller calls fillText on top.

baselineIsTop matches the canvas2d textBaseline the caller will use for
the subsequent fillText: "top" leaves the text origin at the top edge
of the glyphs, so the pill starts padY above (y - padY); other baselines
(alphabetic / bottom / middle) anchor the origin lower in the glyph box,
so we shift the pill up by the measured ascent first to keep the pill
wrapped around the actual rendered text rather than offset below it.
*/
function drawCaptionPill(
	ctx: CanvasRenderingContext2D,
	x: number,
	y: number,
	text: string,
	align: "left" | "center",
	baselineIsTop = true,
): void {
	const metrics = ctx.measureText(text);
	const padX = 4;
	const padY = 2;
	const ascent = metrics.actualBoundingBoxAscent || 8;
	const descent = metrics.actualBoundingBoxDescent || 2;
	const w = metrics.width + padX * 2;
	const h = ascent + descent + padY * 2;
	const px = align === "center" ? x - w / 2 : x - padX;
	const py = baselineIsTop ? y - padY : y - ascent - padY;
	ctx.fillStyle = "rgba(8, 10, 22, 0.62)";
	ctx.beginPath();
	const rr = Math.min(4, h / 2);
	ctx.moveTo(px + rr, py);
	ctx.lineTo(px + w - rr, py);
	ctx.quadraticCurveTo(px + w, py, px + w, py + rr);
	ctx.lineTo(px + w, py + h - rr);
	ctx.quadraticCurveTo(px + w, py + h, px + w - rr, py + h);
	ctx.lineTo(px + rr, py + h);
	ctx.quadraticCurveTo(px, py + h, px, py + h - rr);
	ctx.lineTo(px, py + rr);
	ctx.quadraticCurveTo(px, py, px + rr, py);
	ctx.closePath();
	ctx.fill();
}

function fnvHash(input: string, salt = 0): number {
	let h = 2166136261 ^ salt;
	for (let i = 0; i < input.length; i++) {
		h ^= input.charCodeAt(i);
		h = Math.imul(h, 16777619);
	}
	return (h >>> 0) / 4294967295;
}

/*
Offsets for values in a field. We use a Fibonacci sunflower (golden-angle
spiral) so N particles in a disk are spaced as evenly as a closed-form
layout allows: nearest-neighbour distance is approximately R / √N, and
area density is uniform. A small per-id angular jitter (≤ ¼ of one slot's
arc) breaks the "perfect rosette" look so it still reads as a population
rather than a printed pattern, but never enough to recreate overlap.

Singletons sit at the centre. With ≥ 2 members we compute:
    r_i     = R · √((i + 0.5) / N)
    theta_i = i · GOLDEN_ANGLE + jitter(id)

R is the anchor's spread cap (anchorRadius · 0.85); the +0.5 offset keeps
the innermost particle off the exact centre so it's still selectable.
*/
const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));
const PARTICLE_SPREAD_FRACTION = 0.85;

function fieldParticleOffset(
	id: string,
	memberIndex: number,
	memberCount: number,
	anchorRadius: number,
): { ox: number; oy: number } {
	if (memberCount <= 1) return { ox: 0, oy: 0 };
	const cap = anchorRadius * PARTICLE_SPREAD_FRACTION;
	const r = cap * Math.sqrt((memberIndex + 0.5) / memberCount);
	// Each slot covers ~GOLDEN_ANGLE of arc on average; jitter ≤ ¼ of that
	// keeps neighbours strictly closer to their own slot than to anyone
	// else's, so the sunflower's no-overlap guarantee still holds.
	const slotArc = GOLDEN_ANGLE;
	const jitter = (fnvHash(id, 31) - 0.5) * slotArc * 0.5;
	const theta = memberIndex * GOLDEN_ANGLE + jitter;
	return { ox: Math.cos(theta) * r, oy: Math.sin(theta) * r };
}

function categoryForMember(member: { program: string }): ProgramCategory {
	return categoryForProgram(member.program);
}

/* ------------------------------------------------------------------------- *
 * Shader sources
 * ------------------------------------------------------------------------- */

const PARTICLE_VS = `#version 300 es
precision highp float;

layout(location = 0) in vec2 a_corner;        // unit quad corner in [-1, 1]
layout(location = 1) in vec2 a_pos;           // world-space center
layout(location = 2) in float a_size;         // half-size in pixels (before halo)
layout(location = 3) in float a_shape;
layout(location = 4) in vec3 a_color;         // 0..1
layout(location = 5) in float a_resPrev;      // resonance at resStart
layout(location = 6) in float a_resTarget;    // resonance target after ease
layout(location = 7) in float a_state;        // bitfield as float
layout(location = 8) in float a_resStart;     // ms timestamp resonance ease began
layout(location = 9) in float a_flashStart;   // ms timestamp last flash began

uniform vec2 u_camera;          // world-space camera center
uniform vec2 u_viewport;        // canvas size in CSS pixels
uniform float u_zoom;
uniform float u_time;           // performance.now() in ms
uniform float u_resEaseDur;     // resonance ease duration in ms
uniform float u_flashDur;       // flash decay duration in ms

out vec2 v_uv;                  // -halo..halo on each axis
out vec3 v_color;
out float v_resonance;
out float v_flash;
out float v_state;
out float v_shape;

void main() {
    float halo = ${QUAD_HALO_FACTOR.toFixed(2)};
    vec2 worldOffset = (a_pos - u_camera) * u_zoom;
    vec2 pixelPos = worldOffset + a_corner * a_size * halo;
    vec2 ndc = vec2(
        ( (pixelPos.x + u_viewport.x * 0.5) / u_viewport.x ) * 2.0 - 1.0,
        1.0 - ( (pixelPos.y + u_viewport.y * 0.5) / u_viewport.y ) * 2.0
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
    v_uv = a_corner * halo;
    v_color = a_color;

    // GPU-side animation. CPU only updates start timestamps when a particle
    // changes state, so the per-frame buffer upload cost is zero.
    float resT = clamp((u_time - a_resStart) / max(u_resEaseDur, 1.0), 0.0, 1.0);
    resT = resT * resT * (3.0 - 2.0 * resT); // smoothstep
    v_resonance = mix(a_resPrev, a_resTarget, resT);

    float flashT = (u_time - a_flashStart) / max(u_flashDur, 1.0);
    v_flash = clamp(1.0 - flashT, 0.0, 1.0);

    v_state = a_state;
    v_shape = a_shape;
}
`;

const PARTICLE_FS = `#version 300 es
precision highp float;

in vec2 v_uv;
in vec3 v_color;
in float v_resonance;
in float v_flash;
in float v_state;
in float v_shape;

out vec4 outColor;

float sdBox(vec2 p, vec2 b) {
    vec2 d = abs(p) - b;
    return length(max(d, 0.0)) + min(max(d.x, d.y), 0.0);
}

float sdCircle(vec2 p, float r) {
    return length(p) - r;
}

float sdRing(vec2 p, float r, float w) {
    return abs(length(p) - r) - w;
}

float sdSegment(vec2 p, vec2 a, vec2 b) {
    vec2 pa = p - a;
    vec2 ba = b - a;
    float h = clamp(dot(pa, ba) / dot(ba, ba), 0.0, 1.0);
    return length(pa - ba * h);
}

float sdTriangleIso(vec2 p, float h, float wHalf) {
    // upward isoceles, apex at (0, -h), base at y=h between -wHalf..wHalf
    p.x = abs(p.x);
    vec2 a = vec2(0.0, -h);
    vec2 b = vec2(wHalf, h);
    float dab = sdSegment(p, a, b);
    float base = p.y - h;
    return max(dab, base) * (p.y < h && p.x < wHalf * (1.0 - (p.y + h) / (2.0 * h)) ? -1.0 : 1.0);
}

// Regular polygon SDF (centered, n sides, radius r).
float sdPolygon(vec2 p, int n, float r) {
    float a = atan(p.x, p.y) + 3.14159265;
    float seg = 6.28318530 / float(n);
    float k = floor(a / seg + 0.5) * seg;
    vec2 c = vec2(cos(k), sin(k)) * r;
    vec2 d = c - vec2(sin(k), -cos(k)) * dot(c - p, vec2(sin(k), -cos(k)));
    // Simpler: distance from p to nearest edge midpoint chord.
    return length(p - c) - 0.001; // approximate; outline is enough
}

float glyphSdf(int shape, vec2 p) {
    if (shape == 0) {        // square
        return sdBox(p, vec2(0.62));
    } else if (shape == 1) { // circle
        return sdCircle(p, 0.6);
    } else if (shape == 2) { // ring
        return sdRing(p, 0.55, 0.08);
    } else if (shape == 3) { // triangle_up
        // Three segments forming an upward triangle.
        vec2 a = vec2(0.0, -0.7);
        vec2 b = vec2(0.65, 0.55);
        vec2 c = vec2(-0.65, 0.55);
        float d = sdSegment(p, a, b);
        d = min(d, sdSegment(p, b, c));
        d = min(d, sdSegment(p, c, a));
        return d - 0.04;
    } else if (shape == 4) { // triangle_down
        vec2 a = vec2(0.0, 0.7);
        vec2 b = vec2(0.65, -0.55);
        vec2 c = vec2(-0.65, -0.55);
        float d = sdSegment(p, a, b);
        d = min(d, sdSegment(p, b, c));
        d = min(d, sdSegment(p, c, a));
        return d - 0.04;
    } else if (shape == 5) { // diamond
        return abs(p.x) + abs(p.y) - 0.7;
    } else if (shape == 6) { // pentagon — approximate via 5 segments
        float r = 0.7;
        float minD = 1e9;
        for (int i = 0; i < 5; i++) {
            float a0 = -1.5707963 + float(i) * 1.2566371;
            float a1 = -1.5707963 + float(i + 1) * 1.2566371;
            vec2 p0 = vec2(cos(a0), sin(a0)) * r;
            vec2 p1 = vec2(cos(a1), sin(a1)) * r;
            minD = min(minD, sdSegment(p, p0, p1));
        }
        return minD - 0.045;
    } else if (shape == 7) { // hourglass: two crossed segments
        float d = sdSegment(p, vec2(-0.6, -0.6), vec2(0.6, 0.6));
        d = min(d, sdSegment(p, vec2(-0.6, 0.6), vec2(0.6, -0.6)));
        d = min(d, sdSegment(p, vec2(-0.6, -0.6), vec2(0.6, -0.6)));
        d = min(d, sdSegment(p, vec2(-0.6, 0.6), vec2(0.6, 0.6)));
        return d - 0.05;
    } else if (shape == 8) { // asterisk: 6 short segments
        float minD = 1e9;
        for (int i = 0; i < 6; i++) {
            float a0 = float(i) * 1.04719755;
            vec2 dir = vec2(cos(a0), sin(a0)) * 0.7;
            minD = min(minD, sdSegment(p, vec2(0.0), dir));
        }
        return minD - 0.05;
    } else if (shape == 9) { // bar
        return sdBox(p, vec2(0.75, 0.16));
    } else if (shape == 10) { // concentric: three nested rings (vote_swarm)
        float d = sdRing(p, 0.62, 0.045);
        d = min(d, sdRing(p, 0.40, 0.045));
        d = min(d, sdRing(p, 0.18, 0.045));
        return d;
    }
    return sdCircle(p, 0.6);
}

void main() {
    vec2 p = v_uv;
    float r = length(p);
    int s = int(v_state + 0.5);
    int shape = int(v_shape + 0.5);

    vec3 col = vec3(0.0);
    float a = 0.0;

    // Aura (resonance + selection): soft inner glow with a longer outer
    // tail so bright Values bleed slightly into their neighbourhood
    // instead of stopping at a hard edge.
    bool selected = (s & ${STATE_SELECTED}) != 0;
    if (v_resonance > 0.05 || selected) {
        float aurR = 1.5 + v_resonance * 0.7 + (selected ? 0.3 : 0.0);
        float aura = smoothstep(aurR, 0.25, r) * (v_resonance * 0.36 + (selected ? 0.26 : 0.0));
        col += v_color * aura;
        a += aura;
    }

    // Causal halos — independent rings at fixed radii so combinations stay legible.
    if ((s & ${STATE_HYP}) != 0) {
        float halo = 1.0 - smoothstep(0.04, 0.09, abs(r - 1.05));
        col += vec3(1.0, 0.88, 0.4) * halo * 0.85;
        a += halo * 0.6;
    }
    if ((s & ${STATE_FAL}) != 0) {
        float halo = 1.0 - smoothstep(0.05, 0.10, abs(r - 1.45));
        col += vec3(1.0, 0.36, 0.36) * halo * 0.95;
        a += halo * 0.7;
    }
    if ((s & ${STATE_INTERV}) != 0) {
        // dashed-ish: modulate around the ring by angle
        float ang = atan(p.y, p.x);
        float dash = step(0.5, fract(ang * 2.5466));
        float halo = (1.0 - smoothstep(0.05, 0.10, abs(r - 1.85))) * dash;
        col += vec3(1.0, 0.45, 0.85) * halo * 0.75;
        a += halo * 0.55;
    }

    // Prompt provenance: a thin outer ring at r ≈ 2.35 in cyan-white.
    // Sits well outside every causal halo (max 1.85 + 0.10 width = 1.95)
    // and outside the resonance aura's fade-out (capped near 2.2), so it
    // reads as a "frame" around prompt Values without competing with the
    // existing visual language. One smoothstep, only when the bit is set.
    if ((s & ${STATE_PROMPT}) != 0) {
        float halo = 1.0 - smoothstep(0.04, 0.09, abs(r - 2.35));
        col += vec3(0.55, 0.95, 1.0) * halo * 0.95;
        a += halo * 0.75;
    }

    // Program-swap flash: expanding ring, opacity = flash, radius grows as flash decays.
    if (v_flash > 0.01) {
        float t = 1.0 - v_flash;
        float fr = 0.45 + t * 1.8;
        float halo = 1.0 - smoothstep(0.05, 0.12, abs(r - fr));
        col += v_color * halo * v_flash;
        a += halo * v_flash;
    }

    // Glyph itself — drawn as outline.
    float d = glyphSdf(shape, p);
    float glyphAlpha = (1.0 - smoothstep(0.04, 0.09, abs(d)));
    float coreBrightness = selected ? 1.0 : 0.6 + v_resonance * 0.4;
    col += v_color * glyphAlpha * coreBrightness;
    a = max(a, glyphAlpha * coreBrightness);

    if (a <= 0.001) discard;
    outColor = vec4(col, clamp(a, 0.0, 1.0));
}
`;

const ANCHOR_VS = `#version 300 es
precision highp float;

layout(location = 0) in vec2 a_corner;
layout(location = 1) in vec2 a_pos;
layout(location = 2) in float a_radius;   // world units
layout(location = 3) in vec3 a_color;
layout(location = 4) in float a_alpha;

uniform vec2 u_camera;
uniform vec2 u_viewport;
uniform float u_zoom;

out vec2 v_uv;
out vec3 v_color;
out float v_alpha;

void main() {
    float pixelRadius = a_radius * u_zoom;
    vec2 worldOffset = (a_pos - u_camera) * u_zoom;
    vec2 pixelPos = worldOffset + a_corner * pixelRadius * 1.4;
    vec2 ndc = vec2(
        ( (pixelPos.x + u_viewport.x * 0.5) / u_viewport.x ) * 2.0 - 1.0,
        1.0 - ( (pixelPos.y + u_viewport.y * 0.5) / u_viewport.y ) * 2.0
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
    v_uv = a_corner * 1.4;
    v_color = a_color;
    v_alpha = a_alpha;
}
`;

const ANCHOR_FS = `#version 300 es
precision highp float;

in vec2 v_uv;
in vec3 v_color;
in float v_alpha;

out vec4 outColor;

void main() {
    float r = length(v_uv);
    // Soft inner tint that picks up the dominant category color so each
    // field reads as a coloured disk rather than a uniform grey ring.
    float innerGlow = smoothstep(1.0, 0.0, r) * 0.10;
    // Two concentric rings give a "double stroke" that scans more clearly
    // than a single 1px-wide ring at any zoom level. Both rings stay thin
    // so we never become a solid disk that hides the particles inside.
    float outerRing = 1.0 - smoothstep(0.02, 0.06, abs(r - 1.0));
    float innerRing = 1.0 - smoothstep(0.02, 0.05, abs(r - 0.92));
    float a = (innerGlow + outerRing * 0.55 + innerRing * 0.30) * v_alpha;
    if (a <= 0.001) discard;
    outColor = vec4(v_color, a);
}
`;

/*
Background pass: one fullscreen triangle pair drawn before anything else.
A radial gradient (warm-dark centre → near-black at the edges) plus a
soft vignette gives the canvas depth the flat clearColor never had. Zero
per-particle cost — the fragment shader runs once per screen pixel and
contains a single length() + two smoothsteps.
*/
const BG_VS = `#version 300 es
precision highp float;

layout(location = 0) in vec2 a_corner;

out vec2 v_uv;

void main() {
    gl_Position = vec4(a_corner, 0.0, 1.0);
    v_uv = a_corner;
}
`;

const BG_FS = `#version 300 es
precision highp float;

in vec2 v_uv;

out vec4 outColor;

uniform float u_aspect; // viewport.x / viewport.y, so the gradient stays circular

void main() {
    vec2 p = vec2(v_uv.x * u_aspect, v_uv.y);
    float r = length(p);
    vec3 centre = vec3(0.045, 0.055, 0.105);
    vec3 edge   = vec3(0.012, 0.012, 0.030);
    vec3 col = mix(centre, edge, smoothstep(0.0, 1.4, r));
    // Subtle vignette so the corners drop another notch without going pure black.
    col *= 1.0 - smoothstep(0.85, 1.5, r) * 0.35;
    outColor = vec4(col, 1.0);
}
`;

const EDGE_VS = `#version 300 es
precision highp float;

layout(location = 0) in vec2 a_pos;

uniform vec2 u_camera;
uniform vec2 u_viewport;
uniform float u_zoom;

void main() {
    vec2 worldOffset = (a_pos - u_camera) * u_zoom;
    vec2 ndc = vec2(
        ( (worldOffset.x + u_viewport.x * 0.5) / u_viewport.x ) * 2.0 - 1.0,
        1.0 - ( (worldOffset.y + u_viewport.y * 0.5) / u_viewport.y ) * 2.0
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
}
`;

const EDGE_FS = `#version 300 es
precision highp float;
uniform vec4 u_color;
out vec4 outColor;
void main() { outColor = u_color; }
`;

/* ------------------------------------------------------------------------- *
 * GL helpers
 * ------------------------------------------------------------------------- */

function mustCreate<T>(value: T | null, what: string): T {
	if (value === null) throw new Error(`gl.create${what} returned null`);
	return value;
}

function compileShader(
	gl: WebGL2RenderingContext,
	type: number,
	source: string,
): WebGLShader {
	const sh = mustCreate(gl.createShader(type), "Shader");
	gl.shaderSource(sh, source);
	gl.compileShader(sh);
	if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
		const log = gl.getShaderInfoLog(sh) ?? "(no log)";
		gl.deleteShader(sh);
		throw new Error(`shader compile error: ${log}`);
	}
	return sh;
}

function linkProgram(
	gl: WebGL2RenderingContext,
	vsSource: string,
	fsSource: string,
): WebGLProgram {
	const vs = compileShader(gl, gl.VERTEX_SHADER, vsSource);
	const fs = compileShader(gl, gl.FRAGMENT_SHADER, fsSource);
	const prog = mustCreate(gl.createProgram(), "Program");
	gl.attachShader(prog, vs);
	gl.attachShader(prog, fs);
	gl.linkProgram(prog);
	gl.deleteShader(vs);
	gl.deleteShader(fs);
	if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
		const log = gl.getProgramInfoLog(prog) ?? "(no log)";
		gl.deleteProgram(prog);
		throw new Error(`program link error: ${log}`);
	}
	return prog;
}

function mustGetUniform(
	gl: WebGL2RenderingContext,
	prog: WebGLProgram,
	name: string,
): WebGLUniformLocation {
	const loc = gl.getUniformLocation(prog, name);
	if (loc === null) throw new Error(`uniform ${name} not found`);
	return loc;
}

/* ------------------------------------------------------------------------- *
 * Renderer
 * ------------------------------------------------------------------------- */

export interface FieldRendererHandlers {
	onSelectField?: (field: FieldSnapshot | null) => void;
	onSelectValue?: (id: string) => void;
}

interface AnchorRecord {
	id: number;
	posX: number;
	posY: number;
	memberCount: number;
	dominantCategory: ProgramCategory;
	dominantProgram: string;
	color: [number, number, number];
	resonanceLevel: number;
	fieldRef: FieldSnapshot;
	radius: number; // world units; cached for hit testing
}

export class FieldRenderer {
	private readonly gl: WebGL2RenderingContext;
	private readonly overlay: CanvasRenderingContext2D | null;
	private readonly overlayCanvas: HTMLCanvasElement | null;

	private cssWidth = 0;
	private cssHeight = 0;
	private dpr = window.devicePixelRatio || 1;

	// Particle pool.
	private readonly instanceData = new Float32Array(MAX_PARTICLES * STRIDE_F);
	private readonly fieldId = new Int32Array(MAX_PARTICLES);
	// memberIndex / memberCount are the particle's slot inside its anchor's
	// Fibonacci-sunflower layout. Tracking them per-slot lets layoutParticles
	// recompute positions every snapshot without going back through field.members.
	private readonly memberIndex = new Int32Array(MAX_PARTICLES);
	private readonly memberCount = new Int32Array(MAX_PARTICLES);
	private readonly nextIdx = new Int32Array(MAX_PARTICLES);
	// Reverse adjacency: prevIdx[i] = j s.t. nextIdx[j] === i, or -1.
	// Maintained alongside nextIdx so backward chain walks are O(1) per step.
	private readonly prevIdx = new Int32Array(MAX_PARTICLES);
	private readonly currentProgram: string[] = new Array(MAX_PARTICLES);
	private readonly idByIndex: string[] = new Array(MAX_PARTICLES);
	// Pre-computed orbit offsets (cos*r, sin*r) per particle, so layout becomes
	// two adds per particle instead of two trig calls.
	private readonly indexById = new Map<string, number>();
	private readonly freeList: number[] = [];
	private highWater = 0; // index past the last possible live slot

	// Field anchor pool.
	private readonly anchorInstanceData = new Float32Array(
		MAX_ANCHORS * ANCHOR_STRIDE_F,
	);
	private readonly anchorRecords: AnchorRecord[] = [];
	private readonly anchorById = new Map<number, AnchorRecord>();

	// Edge buffer (for selection lineage only).
	private edgeData = new Float32Array(0);
	private edgeVertexCount = 0;

	// Camera + interaction.
	private camX = 0;
	private camY = 0;
	private zoom = 1;
	private dragging = false;
	private didDrag = false;
	private dragStartX = 0;
	private dragStartY = 0;
	private camStartX = 0;
	private camStartY = 0;
	private mouseX = 0;
	private mouseY = 0;

	// Spatial grid for hit testing (built on resync). Implemented as a
	// fixed-size hash bucket array with a linked list per bucket — zero
	// allocations on rebuild, just integer writes.
	//   spatialHead[bucket]    → first particle slot in that bucket, or -1
	//   spatialNext[idx]       → next particle slot in the same bucket, or -1
	//   spatialKey[idx]        → full cell key (for collision filtering on lookup)
	private readonly spatialHead = new Int32Array(SPATIAL_BUCKETS);
	private readonly spatialNext = new Int32Array(MAX_PARTICLES);
	private readonly spatialKey = new Int32Array(MAX_PARTICLES);

	// Snapshot tracking.
	private lastSnapshot: VizGraphSnapshot | null = null;
	private lastGraphSeq = -1;
	private selectedId: string | null = null;
	private handlers: FieldRendererHandlers = {};

	// Frame loop.
	private rafId = 0;
	private running = false;
	private dirty = true;
	// Latest timestamp at which a GPU-side animation will end. We keep
	// rendering until now > nextAnimEndTime, after which the frame loop
	// drops back to idle (only camera/snapshot dirty triggers a redraw).
	private nextAnimEndTime = 0;

	// GL programs / buffers.
	private particleProgram!: WebGLProgram;
	private particleVao!: WebGLVertexArrayObject;
	private particleQuadVbo!: WebGLBuffer;
	private particleInstanceVbo!: WebGLBuffer;
	private uParticleCamera!: WebGLUniformLocation;
	private uParticleViewport!: WebGLUniformLocation;
	private uParticleZoom!: WebGLUniformLocation;
	private uParticleTime!: WebGLUniformLocation;
	private uParticleResEase!: WebGLUniformLocation;
	private uParticleFlashDur!: WebGLUniformLocation;

	private anchorProgram!: WebGLProgram;
	private anchorVao!: WebGLVertexArrayObject;
	private anchorQuadVbo!: WebGLBuffer;
	private anchorInstanceVbo!: WebGLBuffer;
	private uAnchorCamera!: WebGLUniformLocation;
	private uAnchorViewport!: WebGLUniformLocation;
	private uAnchorZoom!: WebGLUniformLocation;

	private edgeProgram!: WebGLProgram;
	private edgeVao!: WebGLVertexArrayObject;
	private edgeVbo!: WebGLBuffer;
	private uEdgeCamera!: WebGLUniformLocation;
	private uEdgeViewport!: WebGLUniformLocation;
	private uEdgeZoom!: WebGLUniformLocation;
	private uEdgeColor!: WebGLUniformLocation;

	private bgProgram!: WebGLProgram;
	private bgVao!: WebGLVertexArrayObject;
	private bgVbo!: WebGLBuffer;
	private uBgAspect!: WebGLUniformLocation;

	// Bound listeners (kept so dispose() can detach).
	private readonly onMouseDown: (e: MouseEvent) => void;
	private readonly onMouseMove: (e: MouseEvent) => void;
	private readonly onMouseUp: (e: MouseEvent) => void;
	private readonly onMouseLeave: (e: MouseEvent) => void;
	private readonly onWheel: (e: WheelEvent) => void;
	private readonly tick: (now: number) => void;

	constructor(
		canvas: HTMLCanvasElement,
		overlayCanvas: HTMLCanvasElement | null,
	) {
		const gl = canvas.getContext("webgl2", {
			antialias: true,
			alpha: false,
			premultipliedAlpha: false,
			powerPreference: "high-performance",
		});
		if (!gl) {
			throw new Error("WebGL2 unavailable");
		}
		this.gl = gl;
		this.overlayCanvas = overlayCanvas;
		this.overlay = overlayCanvas?.getContext("2d") ?? null;

		// Linked-list sentinels: -1 means "no entry".
		this.spatialHead.fill(-1);
		this.spatialNext.fill(-1);
		this.nextIdx.fill(-1);
		this.prevIdx.fill(-1);
		this.fieldId.fill(-1);

		this.initGl(canvas);

		this.onMouseDown = (e) => this.handleMouseDown(e);
		this.onMouseMove = (e) => this.handleMouseMove(e);
		this.onMouseUp = (e) => this.handleMouseUp(e);
		this.onMouseLeave = (_e) => {
			this.dragging = false;
			this.dirty = true;
		};
		this.onWheel = (e) => this.handleWheel(e);
		this.tick = (now) => this.frame(now);

		canvas.addEventListener("mousedown", this.onMouseDown);
		canvas.addEventListener("mousemove", this.onMouseMove);
		window.addEventListener("mouseup", this.onMouseUp);
		canvas.addEventListener("mouseleave", this.onMouseLeave);
		canvas.addEventListener("wheel", this.onWheel, { passive: false });
	}

	dispose(): void {
		this.running = false;
		if (this.rafId) {
			cancelAnimationFrame(this.rafId);
			this.rafId = 0;
		}
		const gl = this.gl;
		gl.deleteProgram(this.particleProgram);
		gl.deleteProgram(this.anchorProgram);
		gl.deleteProgram(this.edgeProgram);
		gl.deleteProgram(this.bgProgram);
		gl.deleteVertexArray(this.particleVao);
		gl.deleteVertexArray(this.anchorVao);
		gl.deleteVertexArray(this.edgeVao);
		gl.deleteVertexArray(this.bgVao);
		gl.deleteBuffer(this.particleQuadVbo);
		gl.deleteBuffer(this.particleInstanceVbo);
		gl.deleteBuffer(this.anchorQuadVbo);
		gl.deleteBuffer(this.anchorInstanceVbo);
		gl.deleteBuffer(this.edgeVbo);
		gl.deleteBuffer(this.bgVbo);

		const canvas = gl.canvas as HTMLCanvasElement;
		canvas.removeEventListener("mousedown", this.onMouseDown);
		canvas.removeEventListener("mousemove", this.onMouseMove);
		window.removeEventListener("mouseup", this.onMouseUp);
		canvas.removeEventListener("mouseleave", this.onMouseLeave);
		canvas.removeEventListener("wheel", this.onWheel);
	}

	start(): void {
		if (this.running) return;
		this.running = true;
		this.rafId = requestAnimationFrame(this.tick);
	}

	stop(): void {
		this.running = false;
		if (this.rafId) {
			cancelAnimationFrame(this.rafId);
			this.rafId = 0;
		}
	}

	resize(cssWidth: number, cssHeight: number): void {
		this.cssWidth = cssWidth;
		this.cssHeight = cssHeight;
		this.dpr = window.devicePixelRatio || 1;
		const w = Math.max(1, Math.floor(cssWidth * this.dpr));
		const h = Math.max(1, Math.floor(cssHeight * this.dpr));
		const canvas = this.gl.canvas as HTMLCanvasElement;
		canvas.width = w;
		canvas.height = h;
		canvas.style.width = `${cssWidth}px`;
		canvas.style.height = `${cssHeight}px`;
		this.gl.viewport(0, 0, w, h);
		if (this.overlayCanvas) {
			this.overlayCanvas.width = w;
			this.overlayCanvas.height = h;
			this.overlayCanvas.style.width = `${cssWidth}px`;
			this.overlayCanvas.style.height = `${cssHeight}px`;
			if (this.overlay) {
				this.overlay.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
			}
		}
		this.dirty = true;
	}

	setHandlers(handlers: FieldRendererHandlers): void {
		this.handlers = handlers;
	}

	setSelected(id: string | null): void {
		if (this.selectedId === id) return;
		const prev = this.selectedId;
		this.selectedId = id;

		if (prev) {
			const idx = this.indexById.get(prev);
			if (idx !== undefined) {
				const off = idx * STRIDE_F;
				const state = this.instanceData[off + STATE_OFFSET_F] | 0;
				this.instanceData[off + STATE_OFFSET_F] = state & ~STATE_SELECTED;
				this.patchInstanceState(idx);
			}
		}
		if (id) {
			const idx = this.indexById.get(id);
			if (idx !== undefined) {
				const off = idx * STRIDE_F;
				const state = this.instanceData[off + STATE_OFFSET_F] | 0;
				this.instanceData[off + STATE_OFFSET_F] = state | STATE_SELECTED;
				this.patchInstanceState(idx);
			}
		}
		this.rebuildEdgeBufferForSelection();
		this.dirty = true;
	}

	/** Upload only the single 4-byte state word for one particle. */
	private patchInstanceState(idx: number): void {
		const gl = this.gl;
		const off = idx * STRIDE_F;
		const byteOff = idx * STRIDE_B + STATE_OFFSET_F * 4;
		gl.bindBuffer(gl.ARRAY_BUFFER, this.particleInstanceVbo);
		gl.bufferSubData(
			gl.ARRAY_BUFFER,
			byteOff,
			this.instanceData.subarray(off + STATE_OFFSET_F, off + STATE_OFFSET_F + 1),
		);
	}

	setSnapshot(snap: VizGraphSnapshot | null): void {
		this.lastSnapshot = snap;
		// Resync runs in frame() so we don't pay for it on every React render.
		this.dirty = true;
	}

	/* --------------------------------------------------------------------- *
	 * GL setup
	 * --------------------------------------------------------------------- */

	private initGl(_canvas: HTMLCanvasElement): void {
		const gl = this.gl;
		gl.clearColor(5 / 255, 5 / 255, 15 / 255, 1.0);
		gl.enable(gl.BLEND);
		gl.blendFuncSeparate(
			gl.SRC_ALPHA,
			gl.ONE_MINUS_SRC_ALPHA,
			gl.ONE,
			gl.ONE_MINUS_SRC_ALPHA,
		);
		gl.disable(gl.DEPTH_TEST);

		const quad = new Float32Array([-1, -1, 1, -1, -1, 1, -1, 1, 1, -1, 1, 1]);

		// Particle program ------------------------------------------------
		this.particleProgram = linkProgram(gl, PARTICLE_VS, PARTICLE_FS);
		const pp = this.particleProgram;
		this.particleVao = mustCreate(gl.createVertexArray(), "VertexArray");
		gl.bindVertexArray(this.particleVao);

		this.particleQuadVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.particleQuadVbo);
		gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);
		gl.enableVertexAttribArray(0);
		gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);

		this.particleInstanceVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.particleInstanceVbo);
		gl.bufferData(gl.ARRAY_BUFFER, MAX_PARTICLES * STRIDE_B, gl.DYNAMIC_DRAW);
		// Layout matches STRIDE_F comments above: 12 floats (48 bytes) per particle.
		// pos
		gl.enableVertexAttribArray(1);
		gl.vertexAttribPointer(1, 2, gl.FLOAT, false, STRIDE_B, 0);
		gl.vertexAttribDivisor(1, 1);
		// size
		gl.enableVertexAttribArray(2);
		gl.vertexAttribPointer(2, 1, gl.FLOAT, false, STRIDE_B, 8);
		gl.vertexAttribDivisor(2, 1);
		// shape
		gl.enableVertexAttribArray(3);
		gl.vertexAttribPointer(3, 1, gl.FLOAT, false, STRIDE_B, 12);
		gl.vertexAttribDivisor(3, 1);
		// color
		gl.enableVertexAttribArray(4);
		gl.vertexAttribPointer(4, 3, gl.FLOAT, false, STRIDE_B, 16);
		gl.vertexAttribDivisor(4, 1);
		// resPrev
		gl.enableVertexAttribArray(5);
		gl.vertexAttribPointer(5, 1, gl.FLOAT, false, STRIDE_B, 28);
		gl.vertexAttribDivisor(5, 1);
		// resTarget
		gl.enableVertexAttribArray(6);
		gl.vertexAttribPointer(6, 1, gl.FLOAT, false, STRIDE_B, 32);
		gl.vertexAttribDivisor(6, 1);
		// state
		gl.enableVertexAttribArray(7);
		gl.vertexAttribPointer(7, 1, gl.FLOAT, false, STRIDE_B, 36);
		gl.vertexAttribDivisor(7, 1);
		// resStart
		gl.enableVertexAttribArray(8);
		gl.vertexAttribPointer(8, 1, gl.FLOAT, false, STRIDE_B, 40);
		gl.vertexAttribDivisor(8, 1);
		// flashStart
		gl.enableVertexAttribArray(9);
		gl.vertexAttribPointer(9, 1, gl.FLOAT, false, STRIDE_B, 44);
		gl.vertexAttribDivisor(9, 1);

		gl.bindVertexArray(null);

		gl.useProgram(pp);
		this.uParticleCamera = mustGetUniform(gl, pp, "u_camera");
		this.uParticleViewport = mustGetUniform(gl, pp, "u_viewport");
		this.uParticleZoom = mustGetUniform(gl, pp, "u_zoom");
		this.uParticleTime = mustGetUniform(gl, pp, "u_time");
		this.uParticleResEase = mustGetUniform(gl, pp, "u_resEaseDur");
		this.uParticleFlashDur = mustGetUniform(gl, pp, "u_flashDur");

		// Anchor program --------------------------------------------------
		this.anchorProgram = linkProgram(gl, ANCHOR_VS, ANCHOR_FS);
		const ap = this.anchorProgram;
		this.anchorVao = mustCreate(gl.createVertexArray(), "VertexArray");
		gl.bindVertexArray(this.anchorVao);

		this.anchorQuadVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.anchorQuadVbo);
		gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);
		gl.enableVertexAttribArray(0);
		gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);

		this.anchorInstanceVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.anchorInstanceVbo);
		gl.bufferData(
			gl.ARRAY_BUFFER,
			MAX_ANCHORS * ANCHOR_STRIDE_B,
			gl.DYNAMIC_DRAW,
		);
		// pos
		gl.enableVertexAttribArray(1);
		gl.vertexAttribPointer(1, 2, gl.FLOAT, false, ANCHOR_STRIDE_B, 0);
		gl.vertexAttribDivisor(1, 1);
		// radius
		gl.enableVertexAttribArray(2);
		gl.vertexAttribPointer(2, 1, gl.FLOAT, false, ANCHOR_STRIDE_B, 8);
		gl.vertexAttribDivisor(2, 1);
		// color
		gl.enableVertexAttribArray(3);
		gl.vertexAttribPointer(3, 3, gl.FLOAT, false, ANCHOR_STRIDE_B, 12);
		gl.vertexAttribDivisor(3, 1);
		// alpha
		gl.enableVertexAttribArray(4);
		gl.vertexAttribPointer(4, 1, gl.FLOAT, false, ANCHOR_STRIDE_B, 24);
		gl.vertexAttribDivisor(4, 1);

		gl.bindVertexArray(null);
		gl.useProgram(ap);
		this.uAnchorCamera = mustGetUniform(gl, ap, "u_camera");
		this.uAnchorViewport = mustGetUniform(gl, ap, "u_viewport");
		this.uAnchorZoom = mustGetUniform(gl, ap, "u_zoom");

		// Edge program ----------------------------------------------------
		this.edgeProgram = linkProgram(gl, EDGE_VS, EDGE_FS);
		const ep = this.edgeProgram;
		this.edgeVao = mustCreate(gl.createVertexArray(), "VertexArray");
		gl.bindVertexArray(this.edgeVao);

		this.edgeVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.edgeVbo);
		gl.bufferData(gl.ARRAY_BUFFER, 1024, gl.DYNAMIC_DRAW);
		gl.enableVertexAttribArray(0);
		gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
		gl.bindVertexArray(null);

		gl.useProgram(ep);
		this.uEdgeCamera = mustGetUniform(gl, ep, "u_camera");
		this.uEdgeViewport = mustGetUniform(gl, ep, "u_viewport");
		this.uEdgeZoom = mustGetUniform(gl, ep, "u_zoom");
		this.uEdgeColor = mustGetUniform(gl, ep, "u_color");

		// Background program -------------------------------------------
		this.bgProgram = linkProgram(gl, BG_VS, BG_FS);
		this.bgVao = mustCreate(gl.createVertexArray(), "VertexArray");
		gl.bindVertexArray(this.bgVao);
		this.bgVbo = mustCreate(gl.createBuffer(), "Buffer");
		gl.bindBuffer(gl.ARRAY_BUFFER, this.bgVbo);
		gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);
		gl.enableVertexAttribArray(0);
		gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
		gl.bindVertexArray(null);
		gl.useProgram(this.bgProgram);
		this.uBgAspect = mustGetUniform(gl, this.bgProgram, "u_aspect");
	}

	/* --------------------------------------------------------------------- *
	 * Snapshot sync — runs only when graphSeq changes
	 * --------------------------------------------------------------------- */

	private syncSnapshot(): void {
		const snap = this.lastSnapshot;
		if (!snap) {
			this.highWater = 0;
			this.indexById.clear();
			this.freeList.length = 0;
			this.anchorRecords.length = 0;
			this.anchorById.clear();
			this.spatialHead.fill(-1);
			this.dirty = true;
			return;
		}
		if (snap.graphSeq === this.lastGraphSeq) return;
		this.lastGraphSeq = snap.graphSeq;
		const nowMs = performance.now();

		const seen = new Set<string>();
		const liveFieldIds = new Set<number>();

		// Anchors first so particles can position around them.
		for (const field of snap.fields) {
			liveFieldIds.add(field.id);
			let anchor = this.anchorById.get(field.id);
			if (!anchor) {
				const angle = fnvHash(String(field.id), 100) * TAU;
				const ring = 220 + fnvHash(String(field.id), 101) * 360;
				anchor = {
					id: field.id,
					posX: Math.cos(angle) * ring,
					posY: Math.sin(angle) * ring,
					memberCount: 0,
					dominantCategory: "unknown",
					dominantProgram: "",
					color: PROGRAM_CATEGORIES.unknown.color,
					resonanceLevel: 0.4,
					fieldRef: field,
					radius: ANCHOR_BASE_RADIUS,
				};
				this.anchorById.set(field.id, anchor);
				this.anchorRecords.push(anchor);
			}
			anchor.fieldRef = field;
			anchor.memberCount = field.members.length;

			let dominantCat: ProgramCategory = "unknown";
			let dominantCatCount = -1;
			let dominantProgram = "";
			let dominantProgramCount = -1;

			const catCounts = new Map<ProgramCategory, number>();
			const progCounts = new Map<string, number>();
			for (let i = 0; i < field.members.length; i++) {
				const m = field.members[i];
				const c = categoryForMember(m);
				catCounts.set(c, (catCounts.get(c) ?? 0) + 1);
				if (m.program) {
					progCounts.set(m.program, (progCounts.get(m.program) ?? 0) + 1);
				}
			}
			for (const [c, n] of catCounts) {
				if (c === "unknown" || c === "plumbing") continue;
				if (n > dominantCatCount) {
					dominantCatCount = n;
					dominantCat = c;
				}
			}
			for (const [p, n] of progCounts) {
				if (n > dominantProgramCount) {
					dominantProgramCount = n;
					dominantProgram = p;
				}
			}

			anchor.dominantCategory = dominantCat;
			anchor.dominantProgram = dominantProgram;
			anchor.color = PROGRAM_CATEGORIES[dominantCat].color;
			anchor.resonanceLevel = Math.max(
				0.35,
				Math.min(1, field.concentration + 0.35),
			);
			// Sunflower packing wants R ≥ s·√N to give each particle ~s of
			// nearest-neighbour clearance. We use s = ANCHOR_PER_MEMBER_SPIRAL
			// so even very full fields stay individually selectable.
			anchor.radius =
				ANCHOR_BASE_RADIUS +
				Math.sqrt(Math.max(1, anchor.memberCount)) *
					ANCHOR_PER_MEMBER_SPIRAL;
		}

		// Drop dead anchors.
		for (let i = this.anchorRecords.length - 1; i >= 0; i--) {
			if (!liveFieldIds.has(this.anchorRecords[i].id)) {
				this.anchorById.delete(this.anchorRecords[i].id);
				this.anchorRecords.splice(i, 1);
			}
		}

		// Settle anchor layout (one batch of repulsion passes per resync).
		this.settleAnchors();

		// Particles. We diff against indexById and reuse slots via freeList.
		for (const field of snap.fields) {
			const anchor = this.anchorById.get(field.id);
			if (!anchor) continue;
			const total = field.members.length;
			for (let mi = 0; mi < total; mi++) {
				const m = field.members[mi];
				seen.add(m.id);
				this.upsertParticle(m, field.id, anchor, nowMs, mi, total);
			}
		}
		for (let oi = 0; oi < snap.orphanValues.length; oi++) {
			const o = snap.orphanValues[oi];
			seen.add(o.id);
			this.upsertParticle(o, -1, null, nowMs, 0, 0);
		}

		// Drop particles no longer present.
		for (const [id, idx] of this.indexById) {
			if (!seen.has(id)) {
				this.releaseParticle(id, idx);
			}
		}

		// Wire nextIdx + prevIdx in a single pass. Reset only the active prefix.
		for (let i = 0; i < this.highWater; i++) {
			this.nextIdx[i] = -1;
			this.prevIdx[i] = -1;
		}
		for (const field of snap.fields) {
			for (let mi = 0; mi < field.members.length; mi++) {
				const m = field.members[mi];
				const idx = this.indexById.get(m.id);
				if (idx === undefined) continue;
				const ni = m.nextId ? this.indexById.get(m.nextId) : undefined;
				if (ni !== undefined) {
					this.nextIdx[idx] = ni;
					this.prevIdx[ni] = idx;
				}
			}
		}
		for (let oi = 0; oi < snap.orphanValues.length; oi++) {
			const o = snap.orphanValues[oi];
			const idx = this.indexById.get(o.id);
			if (idx === undefined) continue;
			const ni = o.nextId ? this.indexById.get(o.nextId) : undefined;
			if (ni !== undefined) {
				this.nextIdx[idx] = ni;
				this.prevIdx[ni] = idx;
			}
		}

		// Re-derive selection bit (position may have moved across slots).
		if (this.selectedId) {
			const idx = this.indexById.get(this.selectedId);
			if (idx !== undefined) {
				this.instanceData[idx * STRIDE_F + STATE_OFFSET_F] =
					(this.instanceData[idx * STRIDE_F + STATE_OFFSET_F] | 0) |
					STATE_SELECTED;
			}
		}

		this.layoutParticles();
		this.uploadAnchors();
		this.uploadParticles();
		this.rebuildSpatialGrid();
		this.rebuildEdgeBufferForSelection();
		this.dirty = true;
	}

	/** Push the active prefix of instanceData to the GPU. Called only when
	 *  particle data actually changed (snapshot resync), never per-frame. */
	private uploadParticles(): void {
		const gl = this.gl;
		gl.bindBuffer(gl.ARRAY_BUFFER, this.particleInstanceVbo);
		gl.bufferSubData(
			gl.ARRAY_BUFFER,
			0,
			this.instanceData.subarray(0, this.highWater * STRIDE_F),
		);
	}

	private upsertParticle(
		m: FieldValueSnapshot,
		fieldId: number,
		anchor: AnchorRecord | null,
		nowMs: number,
		memberIndex: number,
		memberCount: number,
	): void {
		let idx = this.indexById.get(m.id);
		const isNew = idx === undefined;
		const newTarget = Math.min(1, m.resonance || 0);

		if (isNew) {
			idx = this.allocateSlot();
			if (idx < 0) return; // pool exhausted
			this.indexById.set(m.id, idx);
			this.idByIndex[idx] = m.id;
			this.currentProgram[idx] = m.program;

			const off = idx * STRIDE_F;
			this.instanceData[off + 2] = PARTICLE_BASE_SIZE;
			// Resonance starts already at the target value (no in-flight ease).
			this.instanceData[off + 7] = newTarget; // resPrev
			this.instanceData[off + 8] = newTarget; // resTarget
			this.instanceData[off + 10] = nowMs - RES_EASE_DURATION_MS; // resStart in past
			// Brand-new particle: brief intro flash.
			if (m.program) {
				this.instanceData[off + 11] = nowMs;
				if (nowMs + FLASH_DURATION_MS > this.nextAnimEndTime) {
					this.nextAnimEndTime = nowMs + FLASH_DURATION_MS;
				}
			} else {
				this.instanceData[off + 11] = nowMs - FLASH_DURATION_MS; // already done
			}
		}

		const i = idx as number;
		this.fieldId[i] = fieldId;
		this.memberIndex[i] = memberIndex;
		this.memberCount[i] = memberCount;

		const cat = categoryForMember(m);
		const style = PROGRAM_CATEGORIES[cat];
		const off = i * STRIDE_F;

		if (!isNew && this.currentProgram[i] !== m.program) {
			this.currentProgram[i] = m.program;
			this.instanceData[off + 11] = nowMs; // flashStart
			if (nowMs + FLASH_DURATION_MS > this.nextAnimEndTime) {
				this.nextAnimEndTime = nowMs + FLASH_DURATION_MS;
			}
		}

		this.instanceData[off + 3] = SHAPE_INDEX[style.shape];
		this.instanceData[off + 4] = style.color[0] / 255;
		this.instanceData[off + 5] = style.color[1] / 255;
		this.instanceData[off + 6] = style.color[2] / 255;

		// Resonance ease: snap "current visible" value into resPrev, then
		// retarget. The shader interpolates from resStart over RES_EASE_DURATION_MS.
		if (!isNew) {
			const oldPrev = this.instanceData[off + 7];
			const oldTarget = this.instanceData[off + 8];
			if (oldTarget !== newTarget) {
				const oldStart = this.instanceData[off + 10];
				let t = (nowMs - oldStart) / RES_EASE_DURATION_MS;
				if (t < 0) t = 0;
				else if (t > 1) t = 1;
				const eased = t * t * (3 - 2 * t); // smoothstep, matches shader
				const current = oldPrev + (oldTarget - oldPrev) * eased;
				this.instanceData[off + 7] = current;
				this.instanceData[off + 8] = newTarget;
				this.instanceData[off + 10] = nowMs;
				if (nowMs + RES_EASE_DURATION_MS > this.nextAnimEndTime) {
					this.nextAnimEndTime = nowMs + RES_EASE_DURATION_MS;
				}
			}
		}

		let state = 0;
		if (m.causal?.hypothesizing) state |= STATE_HYP;
		if (m.causal?.falsified) state |= STATE_FAL;
		if (m.causal?.intervening) state |= STATE_INTERV;
		if (m.role === "prompt") state |= STATE_PROMPT;
		if (this.selectedId && this.selectedId === m.id) state |= STATE_SELECTED;
		this.instanceData[off + STATE_OFFSET_F] = state;

		// Initial position for new particles (final pos written in layoutParticles).
		if (isNew) {
			if (anchor) {
				const { ox, oy } = fieldParticleOffset(
					m.id,
					memberIndex,
					memberCount,
					anchor.radius,
				);
				this.instanceData[off + 0] = anchor.posX + ox;
				this.instanceData[off + 1] = anchor.posY + oy;
			} else {
				this.instanceData[off + 0] = (fnvHash(m.id, 20) - 0.5) * 1200;
				this.instanceData[off + 1] = (fnvHash(m.id, 21) - 0.5) * 1200;
			}
		}
	}

	private releaseParticle(id: string, idx: number): void {
		this.indexById.delete(id);
		this.idByIndex[idx] = "";
		this.currentProgram[idx] = "";
		this.fieldId[idx] = -1;
		this.memberIndex[idx] = 0;
		this.memberCount[idx] = 0;
		this.nextIdx[idx] = -1;
		this.prevIdx[idx] = -1;
		// Zero the whole row so it draws nothing if rendered out of range.
		const off = idx * STRIDE_F;
		for (let k = 0; k < STRIDE_F; k++) this.instanceData[off + k] = 0;
		this.freeList.push(idx);
	}

	private allocateSlot(): number {
		const reused = this.freeList.pop();
		if (reused !== undefined) return reused;
		if (this.highWater >= MAX_PARTICLES) return -1;
		const idx = this.highWater++;
		return idx;
	}

	private settleAnchors(): void {
		// Sweep-and-prune on X: sort anchors by posX, then for each anchor
		// only test pairs whose X-distance is within (rA + rB + minSep). This
		// turns the worst case from O(M² * passes) into O(M log M + Mk).
		// On nearly-sorted data (common between passes since positions only
		// move slightly), the sort is effectively linear.
		const anchors = this.anchorRecords;
		const n = anchors.length;
		if (n < 2) return;

		anchors.sort(sortByPosX);

		for (let pass = 0; pass < FIELD_REPEL_PASSES; pass++) {
			let any = false;
			for (let i = 0; i < n; i++) {
				const a = anchors[i];
				const aR = a.radius;
				const minDistI = MIN_FIELD_DISTANCE + aR;
				const aPosX = a.posX;
				for (let j = i + 1; j < n; j++) {
					const b = anchors[j];
					const dx = b.posX - aPosX;
					const minDist = minDistI + b.radius;
					// Sweep pruning: anchors past this X cannot collide with `a`.
					if (dx > minDist) break;
					const dy = b.posY - a.posY;
					const d2 = dx * dx + dy * dy;
					if (d2 < minDist * minDist) {
						const d = Math.sqrt(d2) || 1;
						const overlap = (minDist - d) * 0.5;
						const nx = dx / d;
						const ny = dy / d;
						a.posX -= nx * overlap;
						a.posY -= ny * overlap;
						b.posX += nx * overlap;
						b.posY += ny * overlap;
						any = true;
					}
				}
			}
			if (!any) break;
			// Resort: positions shifted on X. Timsort handles nearly-sorted
			// arrays in ~O(n).
			anchors.sort(sortByPosX);
		}
	}

	private layoutParticles(): void {
		// Recompute positions from the stored sunflower slot every resync so
		// radii track anchor growth (and member-count changes) without going
		// back through field.members.
		for (let idx = 0; idx < this.highWater; idx++) {
			if (this.idByIndex[idx] === "") continue;
			const fid = this.fieldId[idx];
			if (fid < 0) continue;
			const anchor = this.anchorById.get(fid);
			if (!anchor) continue;
			const off = idx * STRIDE_F;
			const { ox, oy } = fieldParticleOffset(
				this.idByIndex[idx],
				this.memberIndex[idx],
				this.memberCount[idx],
				anchor.radius,
			);
			this.instanceData[off + 0] = anchor.posX + ox;
			this.instanceData[off + 1] = anchor.posY + oy;
		}

		// Orphans — values not bound to any anchor — get laid out along their
		// prev/next chain order instead of random hash positions, so adjacent
		// values in the linked list end up adjacent in space and edges become
		// short local segments instead of full-canvas diagonals.
		this.layoutOrphansAlongChains();
	}

	/*
	Walk every chain head (prevIdx < 0) forward and place its values on a
	serpentine grid: each row alternates direction so the end of one row sits
	right above the start of the next. Disconnected singletons are appended
	last on their own row. The grid is sized to roughly square the visible
	footprint regardless of population.
	*/
	private layoutOrphansAlongChains(): void {
		const SPACING = 32;
		const orphans: number[] = [];

		for (let idx = 0; idx < this.highWater; idx++) {
			if (this.idByIndex[idx] === "") continue;
			if (this.fieldId[idx] >= 0) continue;
			orphans.push(idx);
		}
		if (orphans.length === 0) return;

		const placed = new Uint8Array(this.highWater);
		const order: number[] = [];

		for (const idx of orphans) {
			if (this.prevIdx[idx] >= 0) continue;
			let cur = idx;
			let guard = 0;
			while (cur >= 0 && !placed[cur] && guard++ < this.highWater) {
				placed[cur] = 1;
				order.push(cur);
				cur = this.nextIdx[cur];
			}
		}
		// Catch cycles or any orphan we missed.
		for (const idx of orphans) {
			if (placed[idx]) continue;
			let cur = idx;
			let guard = 0;
			while (cur >= 0 && !placed[cur] && guard++ < this.highWater) {
				placed[cur] = 1;
				order.push(cur);
				cur = this.nextIdx[cur];
			}
		}

		const cols = Math.max(8, Math.ceil(Math.sqrt(order.length)));
		const halfW = ((cols - 1) * SPACING) / 2;
		const rows = Math.ceil(order.length / cols);
		const halfH = ((rows - 1) * SPACING) / 2;

		for (let i = 0; i < order.length; i++) {
			const idx = order[i];
			const row = Math.floor(i / cols);
			const colInRow = i % cols;
			// Serpentine: even rows left→right, odd rows right→left, so the
			// chain step from end-of-row to start-of-next-row is one cell down.
			const col = row % 2 === 0 ? colInRow : cols - 1 - colInRow;
			const off = idx * STRIDE_F;
			this.instanceData[off + 0] = col * SPACING - halfW;
			this.instanceData[off + 1] = row * SPACING - halfH;
		}
	}

	private uploadAnchors(): void {
		for (let i = 0; i < this.anchorRecords.length && i < MAX_ANCHORS; i++) {
			const a = this.anchorRecords[i];
			const off = i * ANCHOR_STRIDE_F;
			this.anchorInstanceData[off + 0] = a.posX;
			this.anchorInstanceData[off + 1] = a.posY;
			this.anchorInstanceData[off + 2] = a.radius;
			this.anchorInstanceData[off + 3] = a.color[0] / 255;
			this.anchorInstanceData[off + 4] = a.color[1] / 255;
			this.anchorInstanceData[off + 5] = a.color[2] / 255;
			this.anchorInstanceData[off + 6] = a.resonanceLevel;
			this.anchorInstanceData[off + 7] = 0;
		}
		const gl = this.gl;
		gl.bindBuffer(gl.ARRAY_BUFFER, this.anchorInstanceVbo);
		gl.bufferSubData(
			gl.ARRAY_BUFFER,
			0,
			this.anchorInstanceData.subarray(
				0,
				Math.min(this.anchorRecords.length, MAX_ANCHORS) * ANCHOR_STRIDE_F,
			),
		);
	}

	private rebuildSpatialGrid(): void {
		// Flat-array linked list: head[bucket] → idx, next[idx] → idx, key[idx]
		// keeps the full hash so lookup can disambiguate bucket collisions.
		// Zero allocations per rebuild — just integer writes into typed arrays.
		this.spatialHead.fill(-1);
		const stride = STRIDE_F;
		const cell = SPATIAL_CELL;
		const head = this.spatialHead;
		const next = this.spatialNext;
		const keyArr = this.spatialKey;
		const data = this.instanceData;
		for (let idx = 0; idx < this.highWater; idx++) {
			if (this.idByIndex[idx] === "") continue;
			const x = data[idx * stride + 0];
			const y = data[idx * stride + 1];
			const cx = Math.floor(x / cell);
			const cy = Math.floor(y / cell);
			const key = (Math.imul(cx, SPATIAL_HASH_X) ^ Math.imul(cy, SPATIAL_HASH_Y)) | 0;
			const bucket = key & SPATIAL_BUCKET_MASK;
			next[idx] = head[bucket];
			head[bucket] = idx;
			keyArr[idx] = key;
		}
	}

	private rebuildEdgeBufferForSelection(): void {
		// Walk forward via nextIdx and backward via the precomputed prevIdx,
		// both O(chain length). No O(N) scans, no allocations beyond the
		// small edge vertex buffer itself.
		this.edgeVertexCount = 0;
		const id = this.selectedId;
		if (!id) {
			this.edgeData = new Float32Array(0);
			return;
		}
		const startIdx = this.indexById.get(id);
		if (startIdx === undefined) return;

		const verts: number[] = [];
		// Cycle guard: a Value cannot appear twice in a single chain walk, so
		// highWater is an absolute upper bound on chain length. Using it as
		// the iteration cap means the full chain always renders (no
		// artificial truncation) while still terminating on cycles.
		const visited = new Uint8Array(this.highWater);

		let cur = startIdx;
		visited[cur] = 1;
		while (true) {
			const nx = this.nextIdx[cur];
			if (nx < 0 || nx === cur || visited[nx]) break;
			verts.push(
				this.instanceData[cur * STRIDE_F + 0],
				this.instanceData[cur * STRIDE_F + 1],
				this.instanceData[nx * STRIDE_F + 0],
				this.instanceData[nx * STRIDE_F + 1],
			);
			visited[nx] = 1;
			cur = nx;
		}

		cur = startIdx;
		while (true) {
			const pv = this.prevIdx[cur];
			if (pv < 0 || pv === cur || visited[pv]) break;
			verts.push(
				this.instanceData[pv * STRIDE_F + 0],
				this.instanceData[pv * STRIDE_F + 1],
				this.instanceData[cur * STRIDE_F + 0],
				this.instanceData[cur * STRIDE_F + 1],
			);
			visited[pv] = 1;
			cur = pv;
		}

		this.edgeData = new Float32Array(verts);
		this.edgeVertexCount = verts.length / 2;
		const gl = this.gl;
		gl.bindBuffer(gl.ARRAY_BUFFER, this.edgeVbo);
		gl.bufferData(gl.ARRAY_BUFFER, this.edgeData, gl.DYNAMIC_DRAW);
	}

	/* --------------------------------------------------------------------- *
	 * Frame loop
	 * --------------------------------------------------------------------- */

	private frame(now: number): void {
		if (!this.running) return;
		this.rafId = requestAnimationFrame(this.tick);

		// Apply pending snapshot first (cheap if seq unchanged).
		if (this.lastSnapshot && this.lastSnapshot.graphSeq !== this.lastGraphSeq) {
			this.syncSnapshot();
		}

		// GPU drives the per-frame animation; we just need to keep redrawing
		// while any animation is still in flight.
		if (now < this.nextAnimEndTime) this.dirty = true;

		if (!this.dirty) return;
		this.dirty = false;
		this.render(now);
	}

	private render(nowMs: number): void {
		const gl = this.gl;
		gl.clear(gl.COLOR_BUFFER_BIT);

		// NOTE: per-frame `bufferSubData` for instance data is gone. Animation
		// state lives in the vertex shader (driven by u_time) and the CPU only
		// uploads when the snapshot resyncs (uploadParticles) or selection
		// toggles (patchInstanceState).

		// Bracket access avoids a false-positive React-hook lint that flags
		// any identifier starting with `use` when called inside a conditional.
		const bindProgram = gl["useProgram"].bind(gl);

		// Background gradient first — one fullscreen quad. Drawing it
		// unconditionally (even when there's nothing else on screen) keeps
		// the canvas from flashing the clearColor between snapshots.
		bindProgram(this.bgProgram);
		gl.uniform1f(
			this.uBgAspect,
			this.cssHeight > 0 ? this.cssWidth / this.cssHeight : 1,
		);
		gl.bindVertexArray(this.bgVao);
		gl.drawArrays(gl.TRIANGLES, 0, 6);

		const liveCount = this.highWater;
		if (liveCount === 0 && this.anchorRecords.length === 0) return;

		// Anchors first (under particles).
		if (this.anchorRecords.length > 0) {
			bindProgram(this.anchorProgram);
			gl.uniform2f(this.uAnchorCamera, this.camX, this.camY);
			gl.uniform2f(this.uAnchorViewport, this.cssWidth, this.cssHeight);
			gl.uniform1f(this.uAnchorZoom, this.zoom);
			gl.bindVertexArray(this.anchorVao);
			gl.drawArraysInstanced(
				gl.TRIANGLES,
				0,
				6,
				Math.min(this.anchorRecords.length, MAX_ANCHORS),
			);
		}

		// Particles.
		if (liveCount > 0) {
			bindProgram(this.particleProgram);
			gl.uniform2f(this.uParticleCamera, this.camX, this.camY);
			gl.uniform2f(this.uParticleViewport, this.cssWidth, this.cssHeight);
			gl.uniform1f(this.uParticleZoom, this.zoom);
			gl.uniform1f(this.uParticleTime, nowMs);
			gl.uniform1f(this.uParticleResEase, RES_EASE_DURATION_MS);
			gl.uniform1f(this.uParticleFlashDur, FLASH_DURATION_MS);
			gl.bindVertexArray(this.particleVao);
			gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, liveCount);
		}

		// Selection-chain edges.
		if (this.edgeVertexCount > 0) {
			bindProgram(this.edgeProgram);
			gl.uniform2f(this.uEdgeCamera, this.camX, this.camY);
			gl.uniform2f(this.uEdgeViewport, this.cssWidth, this.cssHeight);
			gl.uniform1f(this.uEdgeZoom, this.zoom);
			gl.uniform4f(this.uEdgeColor, 1.0, 1.0, 1.0, 0.45);
			gl.bindVertexArray(this.edgeVao);
			gl.drawArrays(gl.LINES, 0, this.edgeVertexCount);
		}

		this.renderOverlay();
	}

	private renderOverlay(): void {
		const ctx = this.overlay;
		if (!ctx) return;
		ctx.clearRect(0, 0, this.cssWidth, this.cssHeight);
		ctx.font = "9px ui-monospace, monospace";
		ctx.textAlign = "center";
		ctx.textBaseline = "top";
		for (let i = 0; i < this.anchorRecords.length; i++) {
			const a = this.anchorRecords[i];
			if (a.memberCount < 1) continue;
			const sx = (a.posX - this.camX) * this.zoom + this.cssWidth / 2;
			const sy = (a.posY - this.camY) * this.zoom + this.cssHeight / 2;
			if (
				sx < -200 ||
				sx > this.cssWidth + 200 ||
				sy < -100 ||
				sy > this.cssHeight + 100
			)
				continue;
			const r = a.radius * this.zoom;
			const caption =
				a.dominantProgram || PROGRAM_CATEGORIES[a.dominantCategory].label;
			if (!caption) continue;
			const [cr, cg, cb] = a.color;
			const labelX = sx;
			const labelY = sy + r + 6;
			drawCaptionPill(ctx, labelX, labelY, caption, "center");
			ctx.fillStyle = `rgba(${cr},${cg},${cb},${Math.min(1, a.resonanceLevel * 0.85 + 0.15)})`;
			ctx.fillText(caption, labelX, labelY);
		}

		// Selected particle label.
		if (this.selectedId) {
			const idx = this.indexById.get(this.selectedId);
			if (idx !== undefined) {
				const off = idx * STRIDE_F;
				const sx =
					(this.instanceData[off + 0] - this.camX) * this.zoom +
					this.cssWidth / 2;
				const sy =
					(this.instanceData[off + 1] - this.camY) * this.zoom +
					this.cssHeight / 2;
				const prog = this.currentProgram[idx];
				if (prog) {
					ctx.font = "10px ui-monospace, monospace";
					ctx.textAlign = "left";
					const r = this.instanceData[off + 4] * 255;
					const g = this.instanceData[off + 5] * 255;
					const b = this.instanceData[off + 6] * 255;
					const labelX = sx + 10;
					const labelY = sy - 6;
					drawCaptionPill(ctx, labelX, labelY, prog, "left");
					ctx.fillStyle = `rgba(${r | 0},${g | 0},${b | 0},0.95)`;
					ctx.fillText(prog, labelX, labelY);
				}
			}
		}
	}

	/* --------------------------------------------------------------------- *
	 * Input handlers
	 * --------------------------------------------------------------------- */

	private handleMouseDown(e: MouseEvent): void {
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		this.mouseX = e.clientX - rect.left;
		this.mouseY = e.clientY - rect.top;
		this.dragging = true;
		this.didDrag = false;
		this.dragStartX = this.mouseX;
		this.dragStartY = this.mouseY;
		this.camStartX = this.camX;
		this.camStartY = this.camY;
	}

	private handleMouseMove(e: MouseEvent): void {
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		this.mouseX = e.clientX - rect.left;
		this.mouseY = e.clientY - rect.top;
		if (!this.dragging) return;
		const dx = this.mouseX - this.dragStartX;
		const dy = this.mouseY - this.dragStartY;
		if (Math.abs(dx) > 2 || Math.abs(dy) > 2) this.didDrag = true;
		this.camX = this.camStartX - dx / this.zoom;
		this.camY = this.camStartY - dy / this.zoom;
		this.dirty = true;
	}

	private handleMouseUp(_e: MouseEvent): void {
		if (!this.dragging) return;
		this.dragging = false;
		if (this.didDrag) return;

		// Click without drag → hit test.
		const world = this.screenToWorld(this.mouseX, this.mouseY);

		// Particles (flat-array spatial hash).
		const cellRadius =
			Math.ceil(20 / Math.max(0.0001, this.zoom) / SPATIAL_CELL) + 1;
		const cx = Math.floor(world.x / SPATIAL_CELL);
		const cy = Math.floor(world.y / SPATIAL_CELL);
		const hitR = 12 / Math.max(0.0001, this.zoom);
		let bestIdx = -1;
		let bestD2 = hitR * hitR;
		const head = this.spatialHead;
		const next = this.spatialNext;
		const keyArr = this.spatialKey;
		for (let oy = -cellRadius; oy <= cellRadius; oy++) {
			for (let ox = -cellRadius; ox <= cellRadius; ox++) {
				const key =
					(Math.imul(cx + ox, SPATIAL_HASH_X) ^
						Math.imul(cy + oy, SPATIAL_HASH_Y)) |
					0;
				const bucket = key & SPATIAL_BUCKET_MASK;
				for (let i = head[bucket]; i >= 0; i = next[i]) {
					// Disambiguate hash collisions: only accept slots whose
					// stored key matches the cell we're querying.
					if (keyArr[i] !== key) continue;
					const px = this.instanceData[i * STRIDE_F + 0];
					const py = this.instanceData[i * STRIDE_F + 1];
					const dxw = px - world.x;
					const dyw = py - world.y;
					const d2 = dxw * dxw + dyw * dyw;
					if (d2 < bestD2) {
						bestD2 = d2;
						bestIdx = i;
					}
				}
			}
		}
		if (bestIdx >= 0) {
			this.handlers.onSelectValue?.(this.idByIndex[bestIdx]);
			return;
		}

		// Field anchors.
		for (let i = 0; i < this.anchorRecords.length; i++) {
			const a = this.anchorRecords[i];
			const dxw = a.posX - world.x;
			const dyw = a.posY - world.y;
			const r = a.radius;
			if (dxw * dxw + dyw * dyw <= r * r) {
				this.handlers.onSelectField?.(a.fieldRef);
				return;
			}
		}

		// Background click clears selection.
		this.handlers.onSelectValue?.("");
		this.handlers.onSelectField?.(null);
	}

	private handleWheel(e: WheelEvent): void {
		e.preventDefault();
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		const sx = e.clientX - rect.left;
		const sy = e.clientY - rect.top;
		const before = this.screenToWorld(sx, sy);
		const scale = Math.exp(-e.deltaY * 0.0015);
		this.zoom = Math.min(8, Math.max(0.05, this.zoom * scale));
		const after = this.screenToWorld(sx, sy);
		this.camX += before.x - after.x;
		this.camY += before.y - after.y;
		this.dirty = true;
	}

	private screenToWorld(sx: number, sy: number): { x: number; y: number } {
		return {
			x: (sx - this.cssWidth / 2) / this.zoom + this.camX,
			y: (sy - this.cssHeight / 2) / this.zoom + this.camY,
		};
	}
}
