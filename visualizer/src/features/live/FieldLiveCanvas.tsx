import p5 from "p5";
import { useEffect, useRef } from "react";
import type {
	FieldSnapshot,
	VizGraphSnapshot,
} from "@/features/telemetry/types";

const PROGRAM_COLORS: Record<string, [number, number, number]> = {
	beam_swarm: [0, 255, 150],
	causal_explore: [255, 200, 0],
	active_inference: [255, 150, 50],
	classification: [100, 180, 255],
	surprisal: [0, 200, 255],
	falsification: [255, 80, 80],
};

const TAU = Math.PI * 2;
const MIN_FIELD_DISTANCE = 180;
const FIELD_REPEL_STRENGTH = 0.4;
const PARTICLE_ARRIVE_STRENGTH = 0.08;
const ORBIT_RADIUS_MIN = 20;
const ORBIT_RADIUS_MAX = 70;
const ORBIT_TANGENT_FORCE = 0.005;
const MAX_PARTICLES = 300;

function fnvHash(input: string, salt = 0) {
	let h = 2166136261 ^ salt;
	for (let i = 0; i < input.length; i++) {
		h ^= input.charCodeAt(i);
		h = Math.imul(h, 16777619);
	}
	return (h >>> 0) / 4294967295;
}

interface Particle {
	id: string;
	pos: p5.Vector;
	vel: p5.Vector;
	role: string;
	program: string;
	resonance: number;
	nextId: string;
	communityId: number;
	label: string;
	orbitRadius: number;
	orbitAngle: number;
}

interface FieldAnchor {
	id: number;
	pos: p5.Vector;
	vel: p5.Vector;
	memberCount: number;
	actionName: string;
	actionColor: [number, number, number];
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

			function colorForParticle(pt: Particle): [number, number, number] {
				if (pt.program && PROGRAM_COLORS[pt.program])
					return PROGRAM_COLORS[pt.program];
				if (pt.role === "action") return [0, 255, 150];
				if (pt.role === "reaction") return [255, 80, 80];
				if (pt.role === "prompt") return [255, 200, 0];
				const hue = (fnvHash(pt.id, 1) * 255) | 0;
				return [hue, 200, Math.max(80, 255 - hue * 0.3)];
			}

			function syncFromTelemetry() {
				const snap = snapshotRef.current;
				if (!snap) return;

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
							actionName: "beam_swarm",
							actionColor: [0, 255, 150],
							resonanceLevel: 0.4,
							fieldRef: field,
						};
						fieldAnchors.set(field.id, anchor);
					}

					const actionName =
						field.lastAction && field.lastAction !== "affinity"
							? field.lastAction
							: "";
					anchor.actionName = actionName;
					if (actionName && PROGRAM_COLORS[actionName]) {
						anchor.actionColor = PROGRAM_COLORS[actionName];
					} else {
						const hex = field.affinityHex
							.replace(/[^0-9a-f]/gi, "")
							.slice(0, 8);
						const hue = hex ? Number.parseInt(hex, 16) % 255 : 180;
						anchor.actionColor = [hue, 180, Math.max(80, 255 - hue * 0.25)];
					}
					anchor.resonanceLevel = Math.max(
						0.35,
						Math.min(1, field.concentration + 0.35),
					);
					anchor.fieldRef = field;
					anchor.memberCount = 0;

					for (const member of field.members) {
						if (budget <= 0) break;
						seen.add(member.id);
						particleToField.set(member.id, field.id);
						anchor.memberCount++;

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
								resonance: 0,
								nextId: member.nextId,
								communityId: field.id,
								label: "",
								orbitRadius:
									ORBIT_RADIUS_MIN +
									fnvHash(member.id, 30) *
										(ORBIT_RADIUS_MAX - ORBIT_RADIUS_MIN),
								orbitAngle: fnvHash(member.id, 31) * TAU,
							};
							particles.set(member.id, pt);
						}

						pt.role = member.role;
						pt.program = member.program;
						pt.nextId = member.nextId;
						pt.communityId = field.id;

						const targetRes = Math.min(1, member.resonance || 0);
						pt.resonance += (targetRes - pt.resonance) * 0.08;

						pt.label =
							member.program && member.program !== "affinity"
								? member.program
								: "";

						budget--;
					}
				}

				for (const orphan of snap.orphanValues) {
					if (budget <= 0) break;
					seen.add(orphan.id);

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
							resonance: 0,
							nextId: orphan.nextId,
							communityId: -1,
							label: "",
							orbitRadius: 0,
							orbitAngle: 0,
						};
						particles.set(orphan.id, pt);
					}

					pt.role = orphan.role;
					pt.program = orphan.program;
					pt.nextId = orphan.nextId;
					pt.communityId = -1;
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

						pt.orbitAngle += ORBIT_TANGENT_FORCE + fnvHash(pt.id, 40) * 0.003;
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
				}
			}

			function drawPressureField() {
				p.stroke(255, 255, 255, 8);
				p.strokeWeight(1);
				for (let x = 0; x < p.width; x += 80) {
					for (let y = 0; y < p.height; y += 80) {
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

			function drawCommunities() {
				for (const [, anchor] of fieldAnchors) {
					if (anchor.memberCount < 3) continue;

					const [r, g, b] = anchor.actionColor;
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

					if (anchor.actionName) {
						p.fill(r, g, b, alpha * 0.5);
						p.textSize(9);
						p.textAlign(p.CENTER);
						p.text(anchor.actionName, cs.x, cs.y + radius + 12);
						p.textAlign(p.LEFT);
					}
				}
			}

			function drawValues() {
				const selected = selectedIdRef.current;

				for (const [, pt] of particles) {
					const s = w2s(pt.pos.x, pt.pos.y);
					const [r, g, b] = colorForParticle(pt);
					const isSelected = pt.id === selected;
					const alpha = 120 + pt.resonance * 135;

					p.push();
					p.translate(s.x, s.y);

					if (pt.resonance > 0.2 || isSelected) {
						p.noStroke();
						p.fill(r, g, b, pt.resonance * 25 + (isSelected ? 15 : 0));
						p.ellipse(0, 0, 18 + pt.resonance * 20 + (isSelected ? 16 : 0));
					}

					p.stroke(r, g, b, alpha);
					p.strokeWeight(isSelected ? 2 : 1);
					p.noFill();

					if (pt.role === "action") {
						p.triangle(0, -5, 5, 5, -5, 5);
					} else if (pt.role === "reaction") {
						p.triangle(0, 5, 5, -4, -5, -4);
					} else {
						p.rect(-4, -4, 8, 8);
					}

					if (pt.label) {
						p.noStroke();
						p.fill(r, g, b, alpha * 0.7);
						p.textSize(8);
						p.textAlign(p.LEFT);
						p.text(pt.label, 8, 3);
					}

					p.pop();
				}
			}

			function drawHUD() {}

			p.draw = () => {
				p.background(5, 5, 15, 120);

				syncFromTelemetry();
				repelFieldAnchors();
				updateParticles();

				drawPressureField();
				drawEdges();
				drawCommunities();
				drawValues();
				drawHUD();
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
						if (anchor.memberCount < 3) continue;
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
