import { useEffect, useMemo, useRef } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { selectFieldValueById } from "@/lib/field-store";
import {
	buildSnapshot,
	type ColorMode,
	type GeometryKind,
	type LabelState,
	type ScenePreset,
} from "@/lib/scene-mapping";
import type { StoredValue } from "@/lib/value-frame";

const MAX_INSTANCES_PER_KIND = 4096;
const MAX_HALO_INSTANCES = 256;
// DIM_FACTOR is applied to the base color of every Value that doesn't
// match the active preset. 0.35 pushes dimmed Values further into the
// background than before (was 0.45) so the foreground program-running
// Values pop with their bright category colors; the scale halving in
// applySnapshot does the rest of the work to recede them.
const DIM_FACTOR = 0.35;
const PICK_RADIUS = 0.06;

const GEOMETRY_KINDS: GeometryKind[] = [
	"sphere",
	"cube",
	"cone",
	"cone_down",
	"octahedron",
	"tetrahedron",
	"dodecahedron",
	"icosahedron",
	"torus",
	"torus_knot",
	"cylinder",
];

interface Field3DSceneProps {
	values: ReadonlyMap<string, StoredValue>;
	ticksSinceTouch: ReadonlyMap<string, number>;
	selectedId: string | null;
	colorMode: ColorMode;
	preset: ScenePreset;
}

interface KindMesh {
	mesh: THREE.InstancedMesh;
	idByInstance: string[];
}

const MAX_PROMPT_RINGS = 64;

interface LabelEntry {
	el: HTMLDivElement;
	position: THREE.Vector3;
}

interface SceneRefs {
	scene: THREE.Scene;
	camera: THREE.PerspectiveCamera;
	renderer: THREE.WebGLRenderer;
	controls: OrbitControls;
	kindMeshes: Map<GeometryKind, KindMesh>;
	chainLines: THREE.LineSegments;
	accentLines: THREE.LineSegments;
	highlightRing: THREE.Mesh;
	promptRings: THREE.InstancedMesh;
	promptRingPositions: THREE.Vector3[];
	communityHalos: THREE.InstancedMesh;
	fieldShell: THREE.Mesh;
	scratchDummy: THREE.Object3D;
	scratchDimColor: THREE.Color;
	scratchProjection: THREE.Vector3;
	labelLayer: HTMLDivElement;
	labelEntries: Map<string, LabelEntry>;
	labelOrder: string[];
	clock: THREE.Clock;
	lastPreset: ScenePreset | null;
	lastSelectedId: string | null;
}

/*
Field3DScene is the primary view. Every Value is one instance whose
geometry kind, color, brightness, and scale together encode role,
status, community, recent activity, and selection. The prev/next chain
graph is drawn at all times as a subtle gray scaffold so structure
stays visible even when no preset is active; preset and selection
edges paint over the scaffold in their own colors so the eye reads
them as foreground.
*/
export function Field3DScene({
	values,
	ticksSinceTouch,
	selectedId,
	colorMode,
	preset,
}: Field3DSceneProps) {
	const containerRef = useRef<HTMLDivElement | null>(null);
	const labelLayerRef = useRef<HTMLDivElement | null>(null);
	const refs = useRef<SceneRefs | null>(null);

	const snapshot = useMemo(
		() => buildSnapshot(values, ticksSinceTouch, colorMode, preset, selectedId),
		[values, ticksSinceTouch, colorMode, preset, selectedId],
	);

	useEffect(() => {
		const container = containerRef.current;
		const labelLayer = labelLayerRef.current;
		if (!container || !labelLayer) {
			return;
		}

		const scene = new THREE.Scene();
		scene.background = new THREE.Color(0x05050f);
		scene.fog = new THREE.FogExp2(0x05050f, 0.0035);

		const camera = new THREE.PerspectiveCamera(
			55,
			container.clientWidth / Math.max(1, container.clientHeight),
			0.1,
			2000,
		);
		camera.position.set(120, 90, 160);
		camera.lookAt(0, 0, 0);

		const renderer = new THREE.WebGLRenderer({ antialias: true });
		renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
		renderer.setSize(container.clientWidth, container.clientHeight);
		container.appendChild(renderer.domElement);

		const controls = new OrbitControls(camera, renderer.domElement);
		controls.enableDamping = true;
		controls.dampingFactor = 0.08;
		controls.minDistance = 20;
		controls.maxDistance = 600;

		// Ambient is pushed to 0.95 so the per-instance Lambert albedo
		// stays at near-full saturation regardless of camera angle. The
		// previous 0.55 ambient + standard material baked half the color
		// out of every dot, which is what made the field look muted and
		// sickly. The directional key light still gives the geometry a
		// readable highlight; the fill light is a cool tint that biases
		// shadows toward indigo instead of black.
		scene.add(new THREE.AmbientLight(0xffffff, 0.95));

		const keyLight = new THREE.DirectionalLight(0xffffff, 0.55);
		keyLight.position.set(120, 200, 120);
		scene.add(keyLight);

		const fillLight = new THREE.DirectionalLight(0x6366f1, 0.25);
		fillLight.position.set(-180, -120, -100);
		scene.add(fillLight);

		const fieldShell = new THREE.Mesh(
			new THREE.SphereGeometry(75, 24, 24),
			new THREE.MeshBasicMaterial({
				color: 0x111827,
				wireframe: true,
				transparent: true,
				opacity: 0.05,
			}),
		);
		scene.add(fieldShell);

		const kindMeshes = new Map<GeometryKind, KindMesh>();

		for (const kind of GEOMETRY_KINDS) {
			const geometry = makeGeometryForKind(kind);
			// MeshLambertMaterial is the cheapest shading model that still
			// reads the per-instance color directly as albedo. The previous
			// MeshStandardMaterial multiplied every color by metalness +
			// roughness which crushed the saturation across the whole
			// field; Lambert keeps the neon palette intact.
			const material = new THREE.MeshLambertMaterial({});
			const mesh = new THREE.InstancedMesh(
				geometry,
				material,
				MAX_INSTANCES_PER_KIND,
			);
			mesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
			mesh.count = 0;
			mesh.frustumCulled = false;
			mesh.userData.kind = kind;
			scene.add(mesh);

			kindMeshes.set(kind, { mesh, idByInstance: [] });
		}

		// Community halos: a single InstancedMesh of low-res spheres,
		// drawn back-side so the camera sees a soft bubble around each
		// cluster instead of a hard front face. Depth-write is disabled
		// so the bubbles never occlude the bright instances inside; the
		// renderOrder pin keeps them painted before everything else so
		// they end up visually behind the foreground regardless of
		// transparent-sort ordering.
		const haloGeometry = new THREE.IcosahedronGeometry(1, 2);
		const haloMaterial = new THREE.MeshBasicMaterial({
			transparent: true,
			opacity: 0.16,
			side: THREE.BackSide,
			depthWrite: false,
		});
		const communityHalos = new THREE.InstancedMesh(
			haloGeometry,
			haloMaterial,
			MAX_HALO_INSTANCES,
		);
		communityHalos.count = 0;
		communityHalos.frustumCulled = false;
		communityHalos.renderOrder = -1;
		scene.add(communityHalos);

		const chainGeometry = new THREE.BufferGeometry();
		chainGeometry.setAttribute(
			"position",
			new THREE.BufferAttribute(new Float32Array(0), 3),
		);
		const chainLines = new THREE.LineSegments(
			chainGeometry,
			new THREE.LineBasicMaterial({
				color: 0x4b5563,
				transparent: true,
				opacity: 0.28,
			}),
		);
		chainLines.frustumCulled = false;
		scene.add(chainLines);

		const accentGeometry = new THREE.BufferGeometry();
		accentGeometry.setAttribute(
			"position",
			new THREE.BufferAttribute(new Float32Array(0), 3),
		);
		accentGeometry.setAttribute(
			"color",
			new THREE.BufferAttribute(new Float32Array(0), 3),
		);
		const accentLines = new THREE.LineSegments(
			accentGeometry,
			new THREE.LineBasicMaterial({
				vertexColors: true,
				transparent: true,
				opacity: 0.85,
			}),
		);
		accentLines.frustumCulled = false;
		scene.add(accentLines);

		const highlightRing = new THREE.Mesh(
			new THREE.TorusGeometry(2.6, 0.12, 12, 48),
			new THREE.MeshBasicMaterial({ color: 0xffffff }),
		);
		highlightRing.visible = false;
		scene.add(highlightRing);

		// Prompt halo: a bright-yellow torus stamped on every Prompt
		// Value so the operator can pick the substrate's ingress points
		// out of the field at any distance, regardless of which color
		// mode is active. We keep it as a separate InstancedMesh (rather
		// than a per-Value mesh) so adding more prompts is free.
		const promptRingGeom = new THREE.TorusGeometry(3.4, 0.18, 10, 36);
		const promptRingMat = new THREE.MeshBasicMaterial({
			color: 0xfff03a,
			transparent: true,
			opacity: 0.95,
		});
		const promptRings = new THREE.InstancedMesh(
			promptRingGeom,
			promptRingMat,
			MAX_PROMPT_RINGS,
		);
		promptRings.count = 0;
		promptRings.frustumCulled = false;
		scene.add(promptRings);

		refs.current = {
			scene,
			camera,
			renderer,
			controls,
			kindMeshes,
			chainLines,
			accentLines,
			highlightRing,
			promptRings,
			promptRingPositions: [],
			communityHalos,
			fieldShell,
			scratchDummy: new THREE.Object3D(),
			scratchDimColor: new THREE.Color(),
			scratchProjection: new THREE.Vector3(),
			labelLayer,
			labelEntries: new Map(),
			labelOrder: [],
			clock: new THREE.Clock(),
			lastPreset: null,
			lastSelectedId: null,
		};

		const handleResize = () => {
			const next = refs.current;
			if (!next) {
				return;
			}

			const width = container.clientWidth;
			const height = container.clientHeight;

			next.camera.aspect = width / Math.max(1, height);
			next.camera.updateProjectionMatrix();
			next.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
			next.renderer.setSize(width, height);
		};

		const observer = new ResizeObserver(handleResize);
		observer.observe(container);

		let pointerDownAt = 0;
		let pointerStart = { x: 0, y: 0 };

		const handlePointerDown = (event: PointerEvent) => {
			pointerDownAt = performance.now();
			pointerStart = { x: event.clientX, y: event.clientY };
		};

		/*
		handleClick uses a closest-projected-instance pick instead of
		strict raycast geometry. Each instance is projected into NDC; the
		Value whose screen position is nearest the cursor (within a
		forgiving radius) wins. This is dramatically more reliable than
		raycasting tiny spheres at far camera distances and matches the
		"lasso" approach used in scientific 3D viewers. We still bail if
		the gesture was a drag, so OrbitControls keeps clean rotate / pan
		semantics.
		*/
		const handleClick = (event: MouseEvent) => {
			const elapsed = performance.now() - pointerDownAt;
			const dx = event.clientX - pointerStart.x;
			const dy = event.clientY - pointerStart.y;
			if (elapsed > 600 || Math.hypot(dx, dy) > 12) {
				return;
			}

			const next = refs.current;
			if (!next) {
				return;
			}

			const rect = renderer.domElement.getBoundingClientRect();
			const ndcX = ((event.clientX - rect.left) / rect.width) * 2 - 1;
			const ndcY = -((event.clientY - rect.top) / rect.height) * 2 + 1;

			const matrix = new THREE.Matrix4();
			const position = new THREE.Vector3();
			let bestId: string | null = null;
			let bestDist = PICK_RADIUS;

			for (const entry of next.kindMeshes.values()) {
				for (let idx = 0; idx < entry.mesh.count; idx++) {
					entry.mesh.getMatrixAt(idx, matrix);
					position.setFromMatrixPosition(matrix).project(next.camera);

					if (position.z > 1 || position.z < -1) {
						continue;
					}

					const screenDx = position.x - ndcX;
					const screenDy = position.y - ndcY;
					const dist = Math.hypot(screenDx, screenDy);

					if (dist < bestDist) {
						bestDist = dist;
						bestId = entry.idByInstance[idx];
					}
				}
			}

			if (bestId) {
				selectFieldValueById(bestId);
			}
		};

		renderer.domElement.addEventListener("pointerdown", handlePointerDown);
		renderer.domElement.addEventListener("click", handleClick);

		let animationFrameHandle = 0;
		let promptSpin = 0;
		const animate = () => {
			animationFrameHandle = window.requestAnimationFrame(animate);
			const next = refs.current;
			if (!next) {
				return;
			}

			const delta = next.clock.getDelta();
			next.controls.update();

			if (next.highlightRing.visible) {
				next.highlightRing.rotation.x += delta * 1.3;
				next.highlightRing.rotation.y += delta * 1.7;
			}

			if (next.promptRings.count > 0) {
				promptSpin += delta * 0.9;
				const pulse = 1 + Math.sin(promptSpin * 2.4) * 0.12;
				const dummy = next.scratchDummy;
				for (let idx = 0; idx < next.promptRings.count; idx++) {
					dummy.position.copy(next.promptRingPositions[idx]);
					dummy.rotation.set(Math.PI / 2, promptSpin, 0);
					dummy.scale.setScalar(pulse);
					dummy.updateMatrix();
					next.promptRings.setMatrixAt(idx, dummy.matrix);
				}
				next.promptRings.instanceMatrix.needsUpdate = true;
			}

			next.renderer.render(next.scene, next.camera);
			projectLabels(next);
		};

		animate();

		return () => {
			window.cancelAnimationFrame(animationFrameHandle);
			observer.disconnect();
			renderer.domElement.removeEventListener("pointerdown", handlePointerDown);
			renderer.domElement.removeEventListener("click", handleClick);
			controls.dispose();
			const dyingRefs = refs.current;
			if (dyingRefs) {
				for (const entry of dyingRefs.labelEntries.values()) {
					entry.el.remove();
				}
				dyingRefs.labelEntries.clear();
			}
			scene.traverse((obj) => {
				if ((obj as THREE.Mesh).geometry) {
					(obj as THREE.Mesh).geometry.dispose();
				}
				const material = (obj as THREE.Mesh).material;
				if (Array.isArray(material)) {
					for (const m of material) {
						m.dispose();
					}
				} else if (material) {
					(material as THREE.Material).dispose();
				}
			});
			renderer.dispose();
			renderer.domElement.remove();
			refs.current = null;
		};
	}, []);

	useEffect(() => {
		const next = refs.current;
		if (!next) {
			return;
		}

		applySnapshot(next, snapshot, preset, selectedId);
		reconcileLabels(next, snapshot.labels);
	}, [snapshot, preset, selectedId]);

	return (
		<>
			<div
				ref={containerRef}
				className="absolute inset-0 cursor-crosshair select-none"
			/>
			<div
				ref={labelLayerRef}
				className="pointer-events-none absolute inset-0 overflow-hidden"
				aria-hidden="true"
			/>
		</>
	);
}

function makeGeometryForKind(kind: GeometryKind): THREE.BufferGeometry {
	switch (kind) {
		case "torus":
			return new THREE.TorusGeometry(1.6, 0.5, 14, 36);
		case "octahedron":
			return new THREE.OctahedronGeometry(1.55, 0);
		case "cube":
			return new THREE.BoxGeometry(2.0, 2.0, 2.0);
		case "cone":
			return new THREE.ConeGeometry(1.3, 2.6, 20);
		case "cone_down":
			return new THREE.ConeGeometry(1.3, 2.6, 20);
		case "tetrahedron":
			return new THREE.TetrahedronGeometry(1.7, 0);
		case "dodecahedron":
			return new THREE.DodecahedronGeometry(1.45, 0);
		case "icosahedron":
			return new THREE.IcosahedronGeometry(1.45, 0);
		case "torus_knot":
			return new THREE.TorusKnotGeometry(1.1, 0.36, 64, 10);
		case "cylinder":
			return new THREE.CylinderGeometry(1.0, 1.0, 2.4, 18);
		default:
			return new THREE.SphereGeometry(1.35, 20, 20);
	}
}

/*
ROTATION_FOR_KIND keeps the canonical orientation each shape needs to
read as itself. Torus is laid flat (X = π/2) so it presents as a ring
rather than a doughnut on its side; cone_down is flipped on Z so the
point faces down — that is the visual contract for "downward sweep"
in the program legend.
*/
const ROTATION_FOR_KIND: Partial<
	Record<GeometryKind, [number, number, number]>
> = {
	torus: [Math.PI / 2, 0, 0],
	cone_down: [Math.PI, 0, 0],
};

function applySnapshot(
	refs: SceneRefs,
	snapshot: ReturnType<typeof buildSnapshot>,
	preset: ScenePreset,
	selectedId: string | null,
) {
	const dummy = refs.scratchDummy;
	const dimColor = refs.scratchDimColor;
	const promptPositions: THREE.Vector3[] = [];

	const buckets = new Map<GeometryKind, typeof snapshot.instances>();
	for (const kind of GEOMETRY_KINDS) {
		buckets.set(kind, []);
	}

	for (const instance of snapshot.instances) {
		buckets.get(instance.kind)?.push(instance);
	}

	for (const [kind, kindInstances] of buckets) {
		const entry = refs.kindMeshes.get(kind);
		if (!entry) {
			continue;
		}

		const count = Math.min(kindInstances.length, MAX_INSTANCES_PER_KIND);
		const idByInstance: string[] = [];

		const baseRotation = ROTATION_FOR_KIND[kind];

		for (let idx = 0; idx < count; idx++) {
			const instance = kindInstances[idx];
			dummy.position.copy(instance.position);
			const scale = instance.dimmed
				? Math.max(0.35, instance.scale * 0.5)
				: instance.scale;
			dummy.scale.setScalar(scale);
			if (baseRotation) {
				dummy.rotation.set(
					baseRotation[0],
					baseRotation[1],
					baseRotation[2],
				);
			} else {
				dummy.rotation.set(0, 0, 0);
			}
			dummy.updateMatrix();
			entry.mesh.setMatrixAt(idx, dummy.matrix);

			if (instance.dimmed) {
				dimColor.copy(instance.color).multiplyScalar(DIM_FACTOR);
				entry.mesh.setColorAt(idx, dimColor);
			} else {
				entry.mesh.setColorAt(idx, instance.color);
			}

			idByInstance.push(instance.id);

			// Octahedron is the Prompt-only geometry per scene-mapping.
			// Stash the world position so the prompt-ring InstancedMesh
			// can place a halo around each one in the same frame.
			if (kind === "octahedron") {
				promptPositions.push(instance.position);
			}
		}

		entry.mesh.count = count;
		entry.mesh.instanceMatrix.needsUpdate = true;
		if (entry.mesh.instanceColor) {
			entry.mesh.instanceColor.needsUpdate = true;
		}
		entry.idByInstance = idByInstance;
	}

	updateChainGeometry(refs.chainLines.geometry, snapshot.chainEdges);
	updateAccentGeometry(refs.accentLines.geometry, snapshot.accentEdges);

	const haloLimit = Math.min(snapshot.halos.length, MAX_HALO_INSTANCES);
	for (let idx = 0; idx < haloLimit; idx++) {
		const halo = snapshot.halos[idx];
		dummy.position.copy(halo.position);
		dummy.rotation.set(0, 0, 0);
		dummy.scale.setScalar(halo.radius);
		dummy.updateMatrix();
		refs.communityHalos.setMatrixAt(idx, dummy.matrix);
		refs.communityHalos.setColorAt(idx, halo.color);
	}
	refs.communityHalos.count = haloLimit;
	refs.communityHalos.instanceMatrix.needsUpdate = true;
	if (refs.communityHalos.instanceColor) {
		refs.communityHalos.instanceColor.needsUpdate = true;
	}

	const promptCount = Math.min(promptPositions.length, MAX_PROMPT_RINGS);
	for (let idx = 0; idx < promptCount; idx++) {
		dummy.position.copy(promptPositions[idx]);
		dummy.rotation.set(Math.PI / 2, 0, 0);
		dummy.scale.setScalar(1);
		dummy.updateMatrix();
		refs.promptRings.setMatrixAt(idx, dummy.matrix);
	}
	refs.promptRings.count = promptCount;
	refs.promptRings.instanceMatrix.needsUpdate = true;
	refs.promptRingPositions = promptPositions.slice(0, promptCount);

	if (snapshot.selectedPosition) {
		refs.highlightRing.position.copy(snapshot.selectedPosition);
		refs.highlightRing.visible = true;
	} else {
		refs.highlightRing.visible = false;
	}

	refs.fieldShell.visible = preset === "all";
	// The prev/next chain scaffold is the substrate's causal graph and is
	// useful when the user is browsing the field generically, but in
	// story-driven presets it crosses the foreground edges and adds
	// nothing — the preset edges already encode the relationships that
	// matter. Hide it everywhere except "all" plus when a Value is
	// selected (so the user can still trace the chain of the focused
	// Value regardless of preset).
	refs.chainLines.visible = preset === "all" || selectedId !== null;

	const presetChanged = refs.lastPreset !== preset;
	const selectionChanged = refs.lastSelectedId !== selectedId;
	const firstPaint = refs.lastPreset === null;

	if (selectionChanged && snapshot.selectedPosition) {
		refs.controls.target.copy(snapshot.selectedPosition);
		refs.controls.update();
	} else if (presetChanged || firstPaint) {
		const focus = computeFocusCentroid(snapshot);
		if (focus) {
			refs.controls.target.copy(focus);
			refs.controls.update();
		}
	}

	refs.lastPreset = preset;
	refs.lastSelectedId = selectedId;
}

/*
computeFocusCentroid finds the visual centre of the active subset so
the camera pivots around something the user can see. Dimmed instances
(filtered by preset) are excluded; if everything is dim or empty we
return null so the caller leaves the existing target untouched.
*/
function computeFocusCentroid(
	snapshot: ReturnType<typeof buildSnapshot>,
): THREE.Vector3 | null {
	let cx = 0;
	let cy = 0;
	let cz = 0;
	let count = 0;

	for (const instance of snapshot.instances) {
		if (instance.dimmed) {
			continue;
		}
		cx += instance.position.x;
		cy += instance.position.y;
		cz += instance.position.z;
		count++;
	}

	if (count === 0) {
		for (const instance of snapshot.instances) {
			cx += instance.position.x;
			cy += instance.position.y;
			cz += instance.position.z;
			count++;
		}
	}

	if (count === 0) {
		return null;
	}

	return new THREE.Vector3(cx / count, cy / count, cz / count);
}

function updateChainGeometry(
	geometry: THREE.BufferGeometry,
	edges: ReturnType<typeof buildSnapshot>["chainEdges"],
) {
	const vertexCount = edges.length * 3;
	const existing = geometry.getAttribute("position") as
		| THREE.BufferAttribute
		| undefined;

	if (
		existing &&
		existing.itemSize === 3 &&
		existing.array instanceof Float32Array &&
		existing.array.length === vertexCount
	) {
		const positions = existing.array as Float32Array;
		for (let idx = 0; idx < edges.length; idx++) {
			const edge = edges[idx];
			positions[idx * 6 + 0] = edge.from.x;
			positions[idx * 6 + 1] = edge.from.y;
			positions[idx * 6 + 2] = edge.from.z;
			positions[idx * 6 + 3] = edge.to.x;
			positions[idx * 6 + 4] = edge.to.y;
			positions[idx * 6 + 5] = edge.to.z;
		}
		existing.needsUpdate = true;
		geometry.computeBoundingSphere();

		return;
	}

	const positions = new Float32Array(edges.length * 6);
	for (let idx = 0; idx < edges.length; idx++) {
		const edge = edges[idx];
		positions[idx * 6 + 0] = edge.from.x;
		positions[idx * 6 + 1] = edge.from.y;
		positions[idx * 6 + 2] = edge.from.z;
		positions[idx * 6 + 3] = edge.to.x;
		positions[idx * 6 + 4] = edge.to.y;
		positions[idx * 6 + 5] = edge.to.z;
	}
	geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
	geometry.computeBoundingSphere();
}

function updateAccentGeometry(
	geometry: THREE.BufferGeometry,
	edges: ReturnType<typeof buildSnapshot>["accentEdges"],
) {
	const positions = new Float32Array(edges.length * 6);
	const colors = new Float32Array(edges.length * 6);
	for (let idx = 0; idx < edges.length; idx++) {
		const edge = edges[idx];
		positions[idx * 6 + 0] = edge.from.x;
		positions[idx * 6 + 1] = edge.from.y;
		positions[idx * 6 + 2] = edge.from.z;
		positions[idx * 6 + 3] = edge.to.x;
		positions[idx * 6 + 4] = edge.to.y;
		positions[idx * 6 + 5] = edge.to.z;

		colors[idx * 6 + 0] = edge.color.r;
		colors[idx * 6 + 1] = edge.color.g;
		colors[idx * 6 + 2] = edge.color.b;
		colors[idx * 6 + 3] = edge.color.r;
		colors[idx * 6 + 4] = edge.color.g;
		colors[idx * 6 + 5] = edge.color.b;
	}
	geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
	geometry.setAttribute("color", new THREE.BufferAttribute(colors, 3));
	geometry.computeBoundingSphere();
}

/*
reconcileLabels syncs the DOM children of the label overlay with the
salient subset chosen by buildSnapshot. Existing chips are reused and
have their content overwritten in place; chips for Values that are no
longer salient are detached. Content is set via direct property writes
so we never trigger a React reconcile on the hot path — projectLabels
runs every animation frame and any allocation here would show up as
jank.
*/
function reconcileLabels(refs: SceneRefs, labels: LabelState[]) {
	const seen = new Set<string>();
	const order: string[] = [];

	for (const label of labels) {
		seen.add(label.id);
		order.push(label.id);
		let entry = refs.labelEntries.get(label.id);

		if (!entry) {
			const el = document.createElement("div");
			el.className = "scene-label";
			el.appendChild(document.createElement("div")); // header row
			el.appendChild(document.createElement("div")); // secondary
			el.appendChild(document.createElement("div")); // detail
			refs.labelLayer.appendChild(el);
			entry = { el, position: label.position };
			refs.labelEntries.set(label.id, entry);
		} else {
			entry.position = label.position;
		}

		entry.el.dataset.highlight = label.highlight ? "1" : "0";
		entry.el.dataset.prompt = label.isPrompt ? "1" : "0";

		const [headerEl, secondaryEl, detailEl] = entry.el
			.children as unknown as HTMLDivElement[];

		headerEl.className = "scene-label-row";
		headerEl.innerHTML = "";
		const badge = document.createElement("span");
		badge.className = "scene-label-badge";
		badge.style.backgroundColor = label.badgeColor;
		badge.textContent = label.badge;
		const primary = document.createElement("span");
		primary.className = "scene-label-primary";
		primary.textContent = label.primary;
		headerEl.appendChild(badge);
		headerEl.appendChild(primary);

		if (label.secondary) {
			secondaryEl.className = "scene-label-secondary";
			secondaryEl.textContent = label.secondary;
			secondaryEl.style.display = "";
		} else {
			secondaryEl.style.display = "none";
		}

		if (label.detail) {
			detailEl.className = "scene-label-detail";
			detailEl.textContent = label.detail;
			detailEl.style.display = "";
		} else {
			detailEl.style.display = "none";
		}
	}

	for (const [id, entry] of refs.labelEntries) {
		if (!seen.has(id)) {
			entry.el.remove();
			refs.labelEntries.delete(id);
		}
	}

	refs.labelOrder = order;
}

/*
VISIBLE_LABEL_LIMIT bounds how many label chips paint at once. The
salience-ordered labelOrder array means the most informative labels
(selected, program-running, active status, prompts) always win the
visible budget; the rest fall through to display:none even when in
frustum so the overlay never crowds the field. The user can rotate
the camera to surface different parts of the order.
*/
const VISIBLE_LABEL_LIMIT = 240;

function projectLabels(refs: SceneRefs) {
	if (refs.labelEntries.size === 0) {
		return;
	}

	const rect = refs.renderer.domElement.getBoundingClientRect();
	const width = rect.width;
	const height = rect.height;
	const projection = refs.scratchProjection;
	let shown = 0;

	for (const id of refs.labelOrder) {
		const entry = refs.labelEntries.get(id);
		if (!entry) {
			continue;
		}

		if (shown >= VISIBLE_LABEL_LIMIT) {
			entry.el.style.display = "none";
			continue;
		}

		projection.copy(entry.position).project(refs.camera);

		if (
			projection.z > 1 ||
			projection.z < -1 ||
			projection.x < -1.05 ||
			projection.x > 1.05 ||
			projection.y < -1.05 ||
			projection.y > 1.05
		) {
			entry.el.style.display = "none";
			continue;
		}

		const x = (projection.x * 0.5 + 0.5) * width;
		const y = (-projection.y * 0.5 + 0.5) * height;
		// Must use an explicit non-empty value here — clearing the
		// inline style falls back to the .scene-label stylesheet rule,
		// which is `display: none`, so the label would never appear.
		entry.el.style.display = "block";
		entry.el.style.transform = `translate3d(${x + 10}px, ${y - 10}px, 0)`;
		shown++;
	}
}
