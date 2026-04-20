import p5 from "p5";
import { useEffect, useRef } from "react";
import type {
	FieldSnapshot,
	FieldValueSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";
import {
	PROGRAM_CATEGORIES,
	type ProgramCategory,
	type Shape,
} from "@/lib/programClassifier";

const TAU = Math.PI * 2;
const MIN_FIELD_DISTANCE = 180;
const FIELD_REPEL_STRENGTH = 0.4;
const PARTICLE_ARRIVE_STRENGTH = 0.08;
const ORBIT_RADIUS_MIN = 20;
const ORBIT_RADIUS_MAX = 70;
const ORBIT_TANGENT_FORCE = 0.005;
const MAX_PARTICLES = 300;
const TRAIL_LENGTH = 18;

function fnvHash(input: string, salt = 0) {
	let h = 2166136261 ^ salt;
	for (let i = 0; i < input.length; i++) {
		h ^= input.charCodeAt(i);
		h = Math.imul(h, 16777619);
	}
	return (h >>> 0) / 4294967295;
}

/*
Particle is the visualiser-side mirror of a live Value. Shape and colour
come from the program category the classifier pulled out of the wire
frame; trails and flashes key off category too so the operator sees
"this is a beam step" at a glance, not a blob of equally weighted dots.
*/
interface Particle {
	id: string;
	pos: p5.Vector;
	vel: p5.Vector;
	role: string;
	program: string;
	category: ProgramCategory;
	shape: Shape;
	color: [number, number, number];
	resonance: number;
	nextId: string;
	communityId: number;
	label: string;
	orbitRadius: number;
	orbitAngle: number;
	/** Program name as of the previous frame sync. When it changes we fire a transition flash. */
	previousProgram: string;
	/** Remaining frames of an expanding-ring flash triggered by a program swap. */
	flashTtl: number;
	/** Short rolling buffer of recent screen-space positions for the beam/inference trail. */
	trail: Array<{ x: number; y: number }>;
	/*
	Causal residues lifted off the wire frame. Rendered as independent
	halos in drawValues so an operator can see "hypothesis armed" and
	"falsified" concurrently — the normal post-refutation state of a
	causal-explore Value.
	*/
	causalHypothesizing: boolean;
	causalFalsified: boolean;
	causalIntervening: boolean;
}

interface FieldAnchor {
	id: number;
	pos: p5.Vector;
	vel: p5.Vector;
	memberCount: number;
	dominantCategory: ProgramCategory;
	dominantProgram: string;
	color: [number, number, number];
	resonanceLevel: number;
	fieldRef: FieldSnapshot;
}

interface FieldLiveCanvasProps {
	className?: string;
	selectedId?: string | null;
	snapshot: VizGraphSnapshot | null;
	onSelectField?: (field: FieldSnapshot | null) => void;
	onSelectValue?: (id: string) => void;
}

/*
categoryForMember pulls the category off the FieldValueSnapshot. The
value-store populates program names matching the classifier's table, so
we just look the name up. Anything unexpected (or an empty string for
an unclassified Value) drops back to "unknown" which the canvas renders
as the historical data square.
*/
function categoryForMember(member: FieldValueSnapshot): ProgramCategory {
	if (!member.program) return "unknown";

	switch (member.program) {
		case "link":
		case "affinity":
			return "plumbing";
		case "beam_swarm_step":
			return "beam";
		case "active_inference":
			return "inference";
		case "classify_readout":
			return "classify";
		case "peer_gap":
			return "peer_gap";
		case "intervene":
			return "intervene";
		case "gap_probe":
			return "gap_probe";
		case "measure_field":
			return "resident";
		case "popcount":
		case "coupling":
		case "temperature":
			return "util";
		default:
			return "unknown";
	}
}

export function FieldLiveCanvas({
	className,
	selectedId,
	snapshot,
	onSelectField,
	onSelectValue,
}: FieldLiveCanvasProps) {
	const containerRef = useRef<HTMLDivElement>(null);
	const snapshotRef = useRef(snapshot);
	snapshotRef.current = snapshot;
	const selectedIdRef = useRef(selectedId ?? null);
	selectedIdRef.current = selectedId ?? null;
	const fieldSelectRef = useRef(onSelectField);
	fieldSelectRef.current = onSelectField;
	const valueSelectRef = useRef(onSelectValue);
	valueSelectRef.current = onSelectValue;

	useEffect(() => {
		if (!containerRef.current) return;
		const container = containerRef.current;

		const particles = new Map<string, Particle>();
		const fieldAnchors = new Map<number, FieldAnchor>();
		const particleToField = new Map<string, number>();
		let lastSyncedSnapshotTime: number | undefined;
		let cameraX = 0;
		let cameraY = 0;
		let zoom = 1;
		let dragging = false;
		let didDrag = false;
		let dragStartX = 0;
		let dragStartY = 0;
		let camStartX = 0;
		let camStartY = 0;

		const sketch = (p: p5) => {
			p.setup = () => {
				const canvas = p.createCanvas(
					container.clientWidth,
					container.clientHeight,
				);
				canvas.parent(container);
				p.textFont("monospace");
			};

			function w2s(wx: number, wy: number) {
				return {
					x: (wx - cameraX) * zoom + p.width / 2,
					y: (wy - cameraY) * zoom + p.height / 2,
				};
			}

			function s2w(sx: number, sy: number) {
				return {
					x: (sx - p.width / 2) / zoom + cameraX,
					y: (sy - p.height / 2) / zoom + cameraY,
				};
			}

			/*
			syncFromTelemetry is the only place the simulation mirrors
			the store. Per-frame category refresh picks up programs that
			swapped on the Go side, and a mismatch against the previous
			category triggers a transition flash so the operator sees
			"this Value just started beam-searching" rather than having
			to squint for a silent glyph change.
			*/
			function syncFromTelemetry() {
				const snap = snapshotRef.current;
				if (!snap) return;

				if (snap.timestamp === lastSyncedSnapshotTime) {
					return;
				}

				lastSyncedSnapshotTime = snap.timestamp;

				const seen = new Set<string>();
				const liveFieldIds = new Set<number>();
				particleToField.clear();

				let budget = MAX_PARTICLES;

				for (const field of snap.fields) {
					liveFieldIds.add(field.id);

					let anchor = fieldAnchors.get(field.id);
					if (!anchor) {
						const angle = fnvHash(String(field.id), 100) * TAU;
						const ring = 200 + fnvHash(String(field.id), 101) * 300;
						anchor = {
							id: field.id,
							pos: p.createVector(
								Math.cos(angle) * ring,
								Math.sin(angle) * ring,
							),
							vel: p.createVector(0, 0),
							memberCount: 0,
							dominantCategory: "unknown",
							dominantProgram: "",
							color: PROGRAM_CATEGORIES.unknown.color,
							resonanceLevel: 0.4,
							fieldRef: field,
						};
						fieldAnchors.set(field.id, anchor);
					}

					anchor.fieldRef = field;
					anchor.memberCount = 0;

					/*
					The anchor inherits the most populous program
					category of its members: the ring colour tracks
					whatever this community is collectively doing right
					now. Ties break to the historical field action
					published on the snapshot when present.
					*/
					const categoryCounts = new Map<ProgramCategory, number>();
					const programCounts = new Map<string, number>();

					for (const member of field.members) {
						const cat = categoryForMember(member);
						categoryCounts.set(cat, (categoryCounts.get(cat) ?? 0) + 1);
						if (member.program) {
							programCounts.set(
								member.program,
								(programCounts.get(member.program) ?? 0) + 1,
							);
						}
					}

					let dominantCat: ProgramCategory = "unknown";
					let dominantCount = -1;

					for (const [cat, count] of categoryCounts) {
						if (cat === "unknown" || cat === "plumbing") continue;
						if (count > dominantCount) {
							dominantCount = count;
							dominantCat = cat;
						}
					}

					let dominantProgram = "";
					let dominantProgramCount = -1;

					for (const [name, count] of programCounts) {
						if (count > dominantProgramCount) {
							dominantProgramCount = count;
							dominantProgram = name;
						}
					}

					anchor.dominantCategory = dominantCat;
					anchor.dominantProgram = dominantProgram;
					anchor.color = PROGRAM_CATEGORIES[dominantCat].color;
					anchor.resonanceLevel = Math.max(
						0.35,
						Math.min(1, field.concentration + 0.35),
					);

					for (const member of field.members) {
						if (budget <= 0) break;
						budget--;
						seen.add(member.id);
						particleToField.set(member.id, field.id);
						anchor.memberCount++;

						const category = categoryForMember(member);
						const style = PROGRAM_CATEGORIES[category];

						let pt = particles.get(member.id);
						if (!pt) {
							pt = {
								id: member.id,
								pos: p.createVector(
									anchor.pos.x + (fnvHash(member.id, 20) - 0.5) * 60,
									anchor.pos.y + (fnvHash(member.id, 21) - 0.5) * 60,
								),
								vel: p5.Vector.random2D().mult(p.random(0.1, 0.25)),
								role: member.role,
								program: member.program,
								category,
								shape: style.shape,
								color: style.color,
								resonance: 0,
								nextId: member.nextId,
								communityId: field.id,
								label: "",
								orbitRadius:
									ORBIT_RADIUS_MIN +
									fnvHash(member.id, 30) *
										(ORBIT_RADIUS_MAX - ORBIT_RADIUS_MIN),
								orbitAngle: fnvHash(member.id, 31) * TAU,
								previousProgram: member.program,
								flashTtl: member.program ? 24 : 0,
								trail: [],
								causalHypothesizing: false,
								causalFalsified: false,
								causalIntervening: false,
							};
							particles.set(member.id, pt);
						}

						pt.role = member.role;
						pt.nextId = member.nextId;
						pt.communityId = field.id;

						if (pt.program !== member.program) {
							pt.previousProgram = pt.program;
							pt.program = member.program;
							pt.category = category;
							pt.shape = style.shape;
							pt.color = style.color;
							pt.flashTtl = 24;
						} else {
							pt.category = category;
							pt.shape = style.shape;
							pt.color = style.color;
						}

						const targetRes = Math.min(1, member.resonance || 0);
						pt.resonance += (targetRes - pt.resonance) * 0.15;

						pt.label = pt.program || "";

						pt.causalHypothesizing = member.causal.hypothesizing;
						pt.causalFalsified = member.causal.falsified;
						pt.causalIntervening = member.causal.intervening;

						budget--;
					}
				}

				for (const orphan of snap.orphanValues) {
					if (budget <= 0) break;
					budget--;
					seen.add(orphan.id);

					const category = categoryForMember(orphan);
					const style = PROGRAM_CATEGORIES[category];

					let pt = particles.get(orphan.id);
					if (!pt) {
						pt = {
							id: orphan.id,
							pos: p.createVector(
								(fnvHash(orphan.id, 20) - 0.5) * p.width * 0.8,
								(fnvHash(orphan.id, 21) - 0.5) * p.height * 0.8,
							),
							vel: p5.Vector.random2D().mult(p.random(0.1, 0.25)),
							role: orphan.role,
							program: orphan.program,
							category,
							shape: style.shape,
							color: style.color,
							resonance: 0,
							nextId: orphan.nextId,
							communityId: -1,
							label: "",
							orbitRadius: 0,
							orbitAngle: 0,
							previousProgram: orphan.program,
							flashTtl: orphan.program ? 24 : 0,
							trail: [],
							causalHypothesizing: false,
							causalFalsified: false,
							causalIntervening: false,
						};
						particles.set(orphan.id, pt);
					}

					pt.role = orphan.role;
					pt.nextId = orphan.nextId;
					pt.communityId = -1;

					if (pt.program !== orphan.program) {
						pt.previousProgram = pt.program;
						pt.program = orphan.program;
						pt.category = category;
						pt.shape = style.shape;
						pt.color = style.color;
						pt.flashTtl = 24;
					} else {
						pt.category = category;
						pt.shape = style.shape;
						pt.color = style.color;
					}

					const targetRes = Math.min(1, orphan.resonance || 0);
					pt.resonance += (targetRes - pt.resonance) * 0.15;
					pt.label = pt.program || "";

					pt.causalHypothesizing = orphan.causal?.hypothesizing ?? false;
					pt.causalFalsified = orphan.causal?.falsified ?? false;
					pt.causalIntervening = orphan.causal?.intervening ?? false;

					budget--;
				}

				for (const [id] of particles) {
					if (!seen.has(id)) particles.delete(id);
				}

				for (const [id] of fieldAnchors) {
					if (!liveFieldIds.has(id)) fieldAnchors.delete(id);
				}
			}

			function repelFieldAnchors() {
				const anchors = Array.from(fieldAnchors.values());

				for (let i = 0; i < anchors.length; i++) {
					for (let j = i + 1; j < anchors.length; j++) {
						const a = anchors[i];
						const b = anchors[j];
						const dx = b.pos.x - a.pos.x;
						const dy = b.pos.y - a.pos.y;
						const d = Math.sqrt(dx * dx + dy * dy) || 1;

						if (d < MIN_FIELD_DISTANCE) {
							const push =
								((MIN_FIELD_DISTANCE - d) * FIELD_REPEL_STRENGTH) / d;
							a.vel.x -= dx * push;
							a.vel.y -= dy * push;
							b.vel.x += dx * push;
							b.vel.y += dy * push;
						}
					}
				}

				for (const anchor of anchors) {
					anchor.vel.mult(0.85);
					anchor.pos.add(anchor.vel);
				}
			}

			function updateParticles() {
				const pointer = s2w(p.mouseX, p.mouseY);

				for (const [, pt] of particles) {
					pt.resonance *= 0.96;
					if (pt.flashTtl > 0) pt.flashTtl--;

					const fieldId = particleToField.get(pt.id);
					const anchor =
						fieldId !== undefined ? fieldAnchors.get(fieldId) : undefined;

					if (anchor) {
						const targetX =
							anchor.pos.x + Math.cos(pt.orbitAngle) * pt.orbitRadius;
						const targetY =
							anchor.pos.y + Math.sin(pt.orbitAngle) * pt.orbitRadius;
						const dx = targetX - pt.pos.x;
						const dy = targetY - pt.pos.y;

						pt.vel.x += dx * PARTICLE_ARRIVE_STRENGTH;
						pt.vel.y += dy * PARTICLE_ARRIVE_STRENGTH;

						const sweep =
							pt.category === "beam"
								? 0.012
								: pt.category === "inference"
									? 0.008
									: ORBIT_TANGENT_FORCE;
						pt.orbitAngle += sweep + fnvHash(pt.id, 40) * 0.003;
					}

					const dx = pointer.x - pt.pos.x;
					const dy = pointer.y - pt.pos.y;
					const d = Math.sqrt(dx * dx + dy * dy);
					if (d > 10) {
						const f = 0.008 / (1 + d * 0.01);
						pt.vel.x += (dx / d) * f;
						pt.vel.y += (dy / d) * f;
					}

					pt.vel.mult(0.88);
					pt.pos.add(pt.vel);
					pt.vel.limit(3);

					/*
					Beam / inference Values keep a short position trail
					so the operator can see directional exploration.
					Other categories don't — static squares with trails
					would just add noise.
					*/
					if (pt.category === "beam" || pt.category === "inference") {
						pt.trail.push({ x: pt.pos.x, y: pt.pos.y });
						if (pt.trail.length > TRAIL_LENGTH) pt.trail.shift();
					} else if (pt.trail.length > 0) {
						pt.trail.length = 0;
					}
				}
			}

			function drawPressureField() {
				if (particles.size === 0) {
					return;
				}

				p.stroke(255, 255, 255, 8);
				p.strokeWeight(1);
				const step = 96;
				for (let x = 0; x < p.width; x += step) {
					for (let y = 0; y < p.height; y += step) {
						const angle = Math.atan2(p.mouseY - y, p.mouseX - x);
						const d = p.dist(p.mouseX, p.mouseY, x, y);
						const len = p.map(d, 0, p.width, 12, 3);
						p.push();
						p.translate(x, y);
						p.rotate(angle);
						p.line(0, 0, len, 0);
						p.pop();
					}
				}
			}

			function drawEdges() {
				p.stroke(255, 255, 255, 10);
				p.strokeWeight(0.5);
				for (const [, pt] of particles) {
					if (!pt.nextId) continue;
					const target = particles.get(pt.nextId);
					if (!target) continue;
					const a = w2s(pt.pos.x, pt.pos.y);
					const b = w2s(target.pos.x, target.pos.y);
					p.line(a.x, a.y, b.x, b.y);
				}
			}

			function drawTrails() {
				for (const [, pt] of particles) {
					if (pt.trail.length < 2) continue;
					const [r, g, b] = pt.color;
					for (let i = 1; i < pt.trail.length; i++) {
						const alpha = (i / pt.trail.length) * 120;
						const a = w2s(pt.trail[i - 1].x, pt.trail[i - 1].y);
						const bw = w2s(pt.trail[i].x, pt.trail[i].y);
						p.stroke(r, g, b, alpha);
						p.strokeWeight(1);
						p.line(a.x, a.y, bw.x, bw.y);
					}
				}
			}

			function drawCommunities() {
				for (const [, anchor] of fieldAnchors) {
					if (anchor.memberCount < 1) continue;

					const [r, g, b] = anchor.color;
					const alpha = anchor.resonanceLevel * 200;
					const cs = w2s(anchor.pos.x, anchor.pos.y);
					const radius =
						(40 + anchor.memberCount * 10 + Math.sin(p.frameCount * 0.08) * 5) *
						zoom;

					p.noFill();
					p.stroke(r, g, b, alpha * 0.4);
					p.strokeWeight(1);
					p.ellipse(cs.x, cs.y, radius * 2);

					p.stroke(r, g, b, alpha * 0.2);
					p.strokeWeight(0.5);
					for (const [, pt] of particles) {
						if (pt.communityId !== anchor.id) continue;
						const ms = w2s(pt.pos.x, pt.pos.y);
						p.line(cs.x, cs.y, ms.x, ms.y);
					}

					p.noStroke();
					p.fill(r, g, b, alpha * 0.08);
					p.ellipse(cs.x, cs.y, radius * 2.8);

					const caption =
						anchor.dominantProgram ||
						PROGRAM_CATEGORIES[anchor.dominantCategory].label;
					if (caption) {
						p.fill(r, g, b, alpha * 0.5);
						p.textSize(9);
						p.textAlign(p.CENTER);
						p.text(caption, cs.x, cs.y + radius + 12);
						p.textAlign(p.LEFT);
					}
				}
			}

			/*
			drawGlyph renders a Value at world position (0,0) in the
			current p5 matrix. The glyph vocabulary deliberately mirrors
			the user's reference sketch — squares for data, triangles
			for actions / reactions — and extends with diamonds, rings,
			pentagons, hourglasses and asterisks for the richer program
			palette. Each shape is kept tiny (~10px) so high-density
			communities stay legible.
			*/
			function drawGlyph(shape: Shape) {
				switch (shape) {
					case "square":
						p.rect(-4, -4, 8, 8);
						break;
					case "triangle_up":
						p.triangle(0, -5, 5, 5, -5, 5);
						break;
					case "triangle_down":
						p.triangle(0, 5, 5, -4, -5, -4);
						break;
					case "diamond":
						p.quad(0, -6, 6, 0, 0, 6, -6, 0);
						break;
					case "pentagon":
						p.beginShape();
						for (let i = 0; i < 5; i++) {
							const a = -Math.PI / 2 + (i * TAU) / 5;
							p.vertex(Math.cos(a) * 5.5, Math.sin(a) * 5.5);
						}
						p.endShape(p.CLOSE);
						break;
					case "hourglass":
						p.beginShape();
						p.vertex(-5, -5);
						p.vertex(5, -5);
						p.vertex(-5, 5);
						p.vertex(5, 5);
						p.endShape(p.CLOSE);
						break;
					case "asterisk":
						for (let i = 0; i < 6; i++) {
							const a = (i * TAU) / 6;
							p.line(0, 0, Math.cos(a) * 6, Math.sin(a) * 6);
						}
						break;
					case "ring":
						p.ellipse(0, 0, 10, 10);
						p.ellipse(0, 0, 5, 5);
						break;
					case "bar":
						p.rect(-6, -1.5, 12, 3);
						break;
					case "circle":
						p.ellipse(0, 0, 7, 7);
						break;
				}
			}

			function drawValues() {
				const selected = selectedIdRef.current;
				const pulse = Math.sin(p.frameCount * 0.2);

				for (const [, pt] of particles) {
					const s = w2s(pt.pos.x, pt.pos.y);
					const [r, g, b] = pt.color;
					const isSelected = pt.id === selected;
					const alpha = 120 + pt.resonance * 135;

					p.push();
					p.translate(s.x, s.y);

					// Soft aura proportional to signals-popcount resonance.
					if (pt.resonance > 0.15 || isSelected) {
						p.noStroke();
						p.fill(r, g, b, pt.resonance * 30 + (isSelected ? 20 : 0));
						p.ellipse(0, 0, 20 + pt.resonance * 28 + (isSelected ? 18 : 0));
					}

					/*
					Causal residue halos — rendered before the glyph so
					the program shape stays crisp on top. Independent
					rings for each residue let the operator see a Value
					that is hypothesising AND was just falsified (the
					usual post-refutation state) without ambiguity.
					*/
					if (pt.causalHypothesizing) {
						p.noFill();
						p.stroke(255, 224, 100, 140 + pulse * 40);
						p.strokeWeight(1);
						p.ellipse(0, 0, 16, 16);
					}

					if (pt.causalFalsified) {
						p.noFill();
						p.stroke(255, 90, 90, 170 + pulse * 30);
						p.strokeWeight(1.5);
						p.ellipse(0, 0, 22, 22);
					}

					if (pt.causalIntervening) {
						/*
						The dashed intervening halo distinguishes the do_intervention
						residue from hypothesis / falsified halos at a glance.
						p5 exposes drawingContext as a union that includes WebGL;
						setLineDash only exists on CanvasRenderingContext2D so the
						cast is required to satisfy tsc while keeping the runtime
						behaviour identical (we only run on the 2D renderer here).
						*/
						const ctx = p.drawingContext as CanvasRenderingContext2D;
						p.noFill();
						p.stroke(255, 110, 220, 160);
						p.strokeWeight(1);
						ctx.setLineDash([3, 3]);
						p.ellipse(0, 0, 28, 28);
						ctx.setLineDash([]);
					}

					// Program-swap flash: an expanding ring that fades
					// over 24 frames, so the instant a Value picks up a
					// new behaviour it's visually obvious.
					if (pt.flashTtl > 0) {
						const t = 1 - pt.flashTtl / 24;
						p.noFill();
						p.stroke(r, g, b, (1 - t) * 180);
						p.strokeWeight(1);
						p.ellipse(0, 0, 8 + t * 38);
					}

					p.stroke(r, g, b, alpha);
					p.strokeWeight(isSelected ? 2 : 1);
					p.noFill();

					drawGlyph(pt.shape);

					if (pt.label) {
						p.noStroke();
						p.fill(r, g, b, alpha * 0.75);
						p.textSize(8);
						p.textAlign(p.LEFT);
						p.text(pt.label, 8, 3);
					}

					p.pop();
				}
			}

			p.draw = () => {
				// Opaque clear — RGBA background with alpha < 255 causes motion trails / ghosting.
				p.background(5, 5, 15);

				syncFromTelemetry();
				repelFieldAnchors();
				updateParticles();

				drawPressureField();
				drawEdges();
				drawTrails();
				drawCommunities();
				drawValues();
			};

			p.mousePressed = () => {
				dragging = true;
				didDrag = false;
				dragStartX = p.mouseX;
				dragStartY = p.mouseY;
				camStartX = cameraX;
				camStartY = cameraY;
			};

			p.mouseDragged = () => {
				if (!dragging) return;
				const dx = p.mouseX - dragStartX;
				const dy = p.mouseY - dragStartY;
				if (Math.abs(dx) > 2 || Math.abs(dy) > 2) didDrag = true;
				cameraX = camStartX - dx / zoom;
				cameraY = camStartY - dy / zoom;
			};

			p.mouseReleased = () => {
				if (!didDrag) {
					const world = s2w(p.mouseX, p.mouseY);

					for (const [, pt] of particles) {
						if (p.dist(pt.pos.x, pt.pos.y, world.x, world.y) <= 12 / zoom) {
							valueSelectRef.current?.(pt.id);
							dragging = false;
							return;
						}
					}

					for (const [, anchor] of fieldAnchors) {
						if (anchor.memberCount < 1) continue;
						const radius = (40 + anchor.memberCount * 10) / zoom;
						if (
							p.dist(anchor.pos.x, anchor.pos.y, world.x, world.y) <= radius
						) {
							fieldSelectRef.current?.(anchor.fieldRef);
							dragging = false;
							return;
						}
					}

					valueSelectRef.current?.("");
					fieldSelectRef.current?.(null);
				}
				dragging = false;
			};

			(p as unknown as { mouseWheel: (e: WheelEvent) => boolean }).mouseWheel =
				(event: WheelEvent) => {
					const before = s2w(p.mouseX, p.mouseY);
					const scale = Math.exp(-event.deltaY * 0.002);
					zoom = Math.min(6, Math.max(0.3, zoom * scale));
					const after = s2w(p.mouseX, p.mouseY);
					cameraX += before.x - after.x;
					cameraY += before.y - after.y;
					return false;
				};

			p.windowResized = () => {
				p.resizeCanvas(container.clientWidth, container.clientHeight);
			};
		};

		const instance = new p5(sketch);
		const ro = new ResizeObserver(() => {
			instance.resizeCanvas(container.clientWidth, container.clientHeight);
		});
		ro.observe(container);

		return () => {
			ro.disconnect();
			instance.remove();
		};
	}, []);

	return (
		<div
			ref={containerRef}
			className={
				className ??
				"h-full w-full rounded-xl border border-white/10 bg-[#05050f]"
			}
		/>
	);
}
