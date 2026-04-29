import { useEffect, useMemo, useRef } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { selectFieldValueById } from "@/lib/field-store";
import {
	buildSnapshot,
	type ColorMode,
	type GeometryKind,
	type ScenePreset,
} from "@/lib/scene-mapping";
import type { StoredValue } from "@/lib/value-frame";

const MAX_INSTANCES_PER_KIND = 4096;
const DIM_FACTOR = 0.18;

const GEOMETRY_KINDS: GeometryKind[] = [
	"sphere",
	"torus",
	"octahedron",
	"cube",
	"cone",
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

interface SceneRefs {
	scene: THREE.Scene;
	camera: THREE.PerspectiveCamera;
	renderer: THREE.WebGLRenderer;
	controls: OrbitControls;
	kindMeshes: Map<GeometryKind, KindMesh>;
	chainLines: THREE.LineSegments;
	accentLines: THREE.LineSegments;
	highlightRing: THREE.Mesh;
	fieldShell: THREE.Mesh;
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
	const refs = useRef<SceneRefs | null>(null);

	const snapshot = useMemo(
		() =>
			buildSnapshot(values, ticksSinceTouch, colorMode, preset, selectedId),
		[values, ticksSinceTouch, colorMode, preset, selectedId],
	);

	useEffect(() => {
		const container = containerRef.current;
		if (!container) {
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
		renderer.setPixelRatio(window.devicePixelRatio);
		renderer.setSize(container.clientWidth, container.clientHeight);
		container.appendChild(renderer.domElement);

		const controls = new OrbitControls(camera, renderer.domElement);
		controls.enableDamping = true;
		controls.dampingFactor = 0.08;
		controls.minDistance = 20;
		controls.maxDistance = 600;

		scene.add(new THREE.AmbientLight(0xffffff, 0.55));

		const keyLight = new THREE.PointLight(0xffffff, 0.7, 1000, 1.4);
		keyLight.position.set(120, 200, 120);
		scene.add(keyLight);

		const fillLight = new THREE.PointLight(0x4f46e5, 0.35, 600, 1.6);
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
			const material = new THREE.MeshStandardMaterial({
				metalness: 0.18,
				roughness: 0.42,
			});
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

		refs.current = {
			scene,
			camera,
			renderer,
			controls,
			kindMeshes,
			chainLines,
			accentLines,
			highlightRing,
			fieldShell,
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
			next.renderer.setSize(width, height);
		};

		const observer = new ResizeObserver(handleResize);
		observer.observe(container);

		const raycaster = new THREE.Raycaster();
		const pointer = new THREE.Vector2();
		let pointerDownAt = 0;
		let pointerStart = { x: 0, y: 0 };

		const handlePointerDown = (event: PointerEvent) => {
			pointerDownAt = performance.now();
			pointerStart = { x: event.clientX, y: event.clientY };
		};

		const handlePointerUp = (event: PointerEvent) => {
			const elapsed = performance.now() - pointerDownAt;
			const dx = event.clientX - pointerStart.x;
			const dy = event.clientY - pointerStart.y;
			if (elapsed > 450 || Math.hypot(dx, dy) > 8) {
				return;
			}

			const next = refs.current;
			if (!next) {
				return;
			}

			const rect = renderer.domElement.getBoundingClientRect();
			pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
			pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

			raycaster.setFromCamera(pointer, next.camera);

			const meshes: THREE.Object3D[] = [];
			for (const entry of next.kindMeshes.values()) {
				meshes.push(entry.mesh);
			}

			const intersects = raycaster.intersectObjects(meshes, false);
			if (intersects.length === 0) {
				return;
			}

			const hit = intersects[0];
			const instanceId = hit.instanceId;
			if (instanceId === undefined) {
				return;
			}

			const kind = (hit.object as THREE.InstancedMesh).userData.kind as
				| GeometryKind
				| undefined;
			if (!kind) {
				return;
			}

			const entry = next.kindMeshes.get(kind);
			if (!entry) {
				return;
			}

			const id = entry.idByInstance[instanceId];
			if (id) {
				selectFieldValueById(id);
			}
		};

		renderer.domElement.addEventListener("pointerdown", handlePointerDown);
		renderer.domElement.addEventListener("pointerup", handlePointerUp);

		let animationFrameHandle = 0;
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

			next.renderer.render(next.scene, next.camera);
		};

		animate();

		return () => {
			window.cancelAnimationFrame(animationFrameHandle);
			observer.disconnect();
			renderer.domElement.removeEventListener("pointerdown", handlePointerDown);
			renderer.domElement.removeEventListener("pointerup", handlePointerUp);
			controls.dispose();
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
	}, [snapshot, preset, selectedId]);

	return <div ref={containerRef} className="absolute inset-0 select-none" />;
}

function makeGeometryForKind(kind: GeometryKind): THREE.BufferGeometry {
	switch (kind) {
		case "torus":
			return new THREE.TorusGeometry(1.6, 0.5, 12, 32);
		case "octahedron":
			return new THREE.OctahedronGeometry(1.55, 0);
		case "cube":
			return new THREE.BoxGeometry(2.0, 2.0, 2.0);
		case "cone":
			return new THREE.ConeGeometry(1.3, 2.6, 20);
		default:
			return new THREE.SphereGeometry(1.35, 20, 20);
	}
}

function applySnapshot(
	refs: SceneRefs,
	snapshot: ReturnType<typeof buildSnapshot>,
	preset: ScenePreset,
	selectedId: string | null,
) {
	const dummy = new THREE.Object3D();
	const dimColor = new THREE.Color();

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

		for (let idx = 0; idx < count; idx++) {
			const instance = kindInstances[idx];
			dummy.position.copy(instance.position);
			const scale = instance.dimmed
				? Math.max(0.35, instance.scale * 0.5)
				: instance.scale;
			dummy.scale.setScalar(scale);
			dummy.rotation.set(0, 0, 0);
			if (kind === "torus") {
				dummy.rotation.x = Math.PI / 2;
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

	if (snapshot.selectedPosition) {
		refs.highlightRing.position.copy(snapshot.selectedPosition);
		refs.highlightRing.visible = true;
	} else {
		refs.highlightRing.visible = false;
	}

	refs.fieldShell.visible = preset === "all";

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
