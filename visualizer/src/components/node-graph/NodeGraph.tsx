/**
 * NodeGraph - GPU-accelerated force-directed graph visualization
 *
 * A React component for visualizing network graphs with thousands of nodes
 * using GPGPU computing for real-time physics simulation. Supports temporal
 * data visualization, multiple layout algorithms, and interactive node selection.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { TimeSlider } from "./components/TimeSlider";
import {
    createGraphGeometry,
    disposeGeometry,
    type GeometryResult,
    updateMaterialTextures
} from "./core/geometry";
import { GPUPicking, MouseTracker } from "./core/gpu-picking";
import { Graph, generators } from "./core/graph";
import { Simulator, type SimulatorConfig } from "./core/simulator";
import {
    dataTextureSize,
    generateCircularLayout,
    generateGridLayout,
    generateHelixLayout,
    generateSphericalLayout,
    indexTextureSize
} from "./utils/texture-generators";

/**
 * Load the UbuntuMono font texture
 *
 * Loads the pre-rendered font atlas texture used for label rendering.
 * The texture has flipY=false to match the original implementation.
 */
async function loadFontTexture(): Promise<THREE.Texture> {
    return new Promise((resolve, reject) => {
        const loader = new THREE.TextureLoader();
        loader.load(
            "/fonts/UbuntuMono.png",
            (texture) => {
                // Important: flipY must be false to match the original
                texture.flipY = false;
                texture.magFilter = THREE.LinearFilter;
                texture.minFilter = THREE.LinearFilter;
                texture.needsUpdate = true;
                console.log("[Font] Loaded UbuntuMono font texture");
                resolve(texture);
            },
            undefined,
            (error) => {
                console.error("[Font] Failed to load font texture:", error);
                reject(error);
            }
        );
    });
}

export type LayoutType = "force" | "circular" | "spherical" | "helix" | "grid";

export interface NodeGraphProps {
    /** Graph data to visualize */
    graph?: Graph;
    /** Initial layout type */
    layout?: LayoutType;
    /** Initial temperature for force simulation */
    initialTemperature?: number;
    /** Cooling rate per frame */
    coolingRate?: number;
    /** Minimum time for temporal filter (if not using internal slider) */
    epochMin?: number;
    /** Maximum time for temporal filter (if not using internal slider) */
    epochMax?: number;
    /** Show node labels */
    showLabels?: boolean;
    /** Show edges */
    showEdges?: boolean;
    /** Show time slider control */
    showTimeSlider?: boolean;
    /** Callback when a node is selected */
    onNodeSelect?: (nodeIndex: number, nodeName: string) => void;
    /** Callback when a node is hovered */
    onNodeHover?: (nodeIndex: number, nodeName: string) => void;
    /** Callback when time range changes */
    onTimeRangeChange?: (from: number, to: number) => void;
    /** CSS class name */
    className?: string;
}

/**
 * NodeGraph component
 *
 * Renders an interactive 3D force-directed graph using Three.js and GPGPU.
 */
export function NodeGraph({
    graph: externalGraph,
    layout = "force",
    initialTemperature = 500,
    coolingRate = 0.99,
    epochMin: externalEpochMin,
    epochMax: externalEpochMax,
    showLabels = true,
    showEdges = true,
    showTimeSlider = true,
    onNodeSelect,
    onNodeHover,
    onTimeRangeChange,
    className
}: NodeGraphProps) {
    const containerRef = useRef<HTMLDivElement>(null);
    const rendererRef = useRef<THREE.WebGLRenderer | null>(null);
    const sceneRef = useRef<THREE.Scene | null>(null);
    const cameraRef = useRef<THREE.PerspectiveCamera | null>(null);
    const controlsRef = useRef<OrbitControls | null>(null);
    const simulatorRef = useRef<Simulator | null>(null);
    const pickingRef = useRef<GPUPicking | null>(null);
    const mouseTrackerRef = useRef<MouseTracker | null>(null);
    const geometryResultRef = useRef<GeometryResult | null>(null);
    const graphStructureRef = useRef<THREE.Group | null>(null);
    const pickingSceneRef = useRef<THREE.Scene | null>(null);

    const animationFrameRef = useRef<number>(0);
    const temperatureRef = useRef<number>(initialTemperature);
    const [currentLayout, setCurrentLayout] = useState<LayoutType>(layout);
    const [isSimulating, setIsSimulating] = useState(true);
    const [selectedNode, setSelectedNode] = useState<string | null>(null);
    const [hoveredNode, setHoveredNode] = useState<string | null>(null);

    // Internal time range state for the slider
    const [timeRange, setTimeRange] = useState<{
        min: number;
        max: number;
        from: number;
        to: number;
    }>({
        from: 0,
        max: 1000,
        min: 0,
        to: 100
    });

    // Use external epoch values if provided, otherwise use internal time range
    const epochMin = externalEpochMin ?? timeRange.from;
    const epochMax = externalEpochMax ?? timeRange.to;

    // Graph configuration refs
    const configRef = useRef<{
        nodesWidth: number;
        edgesWidth: number;
        epochsWidth: number;
        epochOffset: number;
    } | null>(null);

    /**
     * Fit camera to show entire graph
     * Uses a fixed distance like the original (1500) and FOV (50)
     */
    const fitCameraToGraph = useCallback(() => {
        if (!cameraRef.current || !controlsRef.current || !simulatorRef.current)
            return;

        // Get the position texture to calculate bounds
        const positionTexture = simulatorRef.current.getPositionTexture();
        if (!positionTexture) return;

        // Use the same camera distance as the original (1500)
        const distance = 1500;

        // Reset camera position
        cameraRef.current.position.set(0, 0, distance);
        cameraRef.current.lookAt(0, 0, 0);

        // Update FOV to match original (50 degrees) for consistent viewing
        cameraRef.current.fov = 50;
        cameraRef.current.updateProjectionMatrix();

        // Reset orbit controls target
        controlsRef.current.target.set(0, 0, 0);
        controlsRef.current.update();

        const nodeCount = geometryResultRef.current?.nodeNames.length ?? 0;
        console.log(
            "[NodeGraph] Camera fit to distance:",
            distance,
            "for",
            nodeCount,
            "nodes"
        );
    }, []);

    /**
     * Initialize Three.js scene and renderer
     */
    const initScene = useCallback(() => {
        if (!containerRef.current) return;

        const container = containerRef.current;
        const width = container.clientWidth;
        const height = container.clientHeight;

        // Create renderer with transparent background (like original)
        // Important: Don't use setPixelRatio - handle DPR manually like the original
        const dpr = window.devicePixelRatio;
        const renderer = new THREE.WebGLRenderer({
            alpha: true,
            antialias: true
        });
        // Set size to actual pixel dimensions, pass false to not update CSS style
        renderer.setSize(width * dpr, height * dpr, false);
        renderer.setClearColor(0x000000, 0); // Transparent black like original
        // Manually set the canvas CSS size
        renderer.domElement.style.width = width + "px";
        renderer.domElement.style.height = height + "px";
        container.appendChild(renderer.domElement);
        rendererRef.current = renderer;

        // Create main scene
        const scene = new THREE.Scene();
        sceneRef.current = scene;

        // Create picking scene (for GPU picking)
        const pickingScene = new THREE.Scene();
        pickingSceneRef.current = pickingScene;

        // Create camera - FOV 50 to match original
        const camera = new THREE.PerspectiveCamera(
            50,
            width / height,
            0.0001,
            100000
        );
        camera.position.z = 1500;
        cameraRef.current = camera;

        // Create orbit controls
        const controls = new OrbitControls(camera, renderer.domElement);
        controls.enableDamping = true;
        controls.dampingFactor = 0.1;
        controls.rotateSpeed = 0.5;
        controls.zoomSpeed = 1.0;
        controlsRef.current = controls;

        // Create graph structure group
        const graphStructure = new THREE.Group();
        scene.add(graphStructure);
        graphStructureRef.current = graphStructure;

        // Create simulator
        const simulator = new Simulator(renderer);
        simulatorRef.current = simulator;

        // Create mouse tracker - attach to the canvas element itself for accurate coordinates
        const mouseTracker = new MouseTracker(renderer.domElement);
        mouseTrackerRef.current = mouseTracker;

        return () => {
            renderer.dispose();
            container.removeChild(renderer.domElement);
            mouseTracker.dispose();
        };
    }, []);

    /**
     * Load graph data and create geometries
     */
    const loadGraph = useCallback(
        async (graph: Graph) => {
            if (
                !rendererRef.current ||
                !simulatorRef.current ||
                !graphStructureRef.current ||
                !pickingSceneRef.current
            ) {
                return;
            }

            // Clean up existing geometry
            if (geometryResultRef.current) {
                disposeGeometry(geometryResultRef.current);
                graphStructureRef.current.clear();
                pickingSceneRef.current.clear();
            }

            const nodesCount = graph.getNodeCount();
            const edgesCount = graph.getEdgeCount();

            if (nodesCount === 0) return;

            // Calculate texture sizes
            const nodesWidth = indexTextureSize(graph.getNodesAndEdgesArray());
            const edgesWidth = dataTextureSize(graph.getNodesAndEdgesArray());
            const nodesAndEpochs = graph.getEpochTextureArray("nodes");
            const epochsWidth = dataTextureSize(nodesAndEpochs);

            // Calculate epoch offset (minimum and maximum timestamps)
            let epochOffset = Number.MAX_SAFE_INTEGER;
            let epochMaxValue = 0;
            nodesAndEpochs.forEach((epochs) => {
                if (epochs) {
                    epochs.forEach((epoch) => {
                        if (epoch < epochOffset) epochOffset = epoch;
                        if (epoch > epochMaxValue) epochMaxValue = epoch;
                    });
                }
            });
            if (epochOffset === Number.MAX_SAFE_INTEGER) epochOffset = 0;
            if (epochMaxValue === 0) epochMaxValue = 1000;

            // Set up time range for slider
            const timeSpan = epochMaxValue - epochOffset;
            const initialWindow = timeSpan / 25; // Start with 4% of the data visible
            setTimeRange({
                from: 0,
                max: timeSpan, // Offset-adjusted maximum
                min: 0, // Offset-adjusted minimum
                to: initialWindow
            });

            configRef.current = {
                edgesWidth,
                epochOffset,
                epochsWidth,
                nodesWidth
            };

            // Initialize simulator
            const simulatorConfig: SimulatorConfig = {
                edgesWidth,
                epochOffset,
                epochsWidth,
                nodesAndEdges: graph.getNodesAndEdgesArray(),
                nodesAndEpochs,
                nodesWidth
            };
            simulatorRef.current.init(simulatorConfig);

            // Load textures
            const textureLoader = new THREE.TextureLoader();
            const nodeTexture = await new Promise<THREE.Texture>((resolve) => {
                // Create a simple circle texture programmatically
                const canvas = document.createElement("canvas");
                canvas.width = 64;
                canvas.height = 64;
                const ctx = canvas.getContext("2d")!;
                const gradient = ctx.createRadialGradient(
                    32,
                    32,
                    0,
                    32,
                    32,
                    32
                );
                gradient.addColorStop(0, "rgba(255, 255, 255, 1)");
                gradient.addColorStop(0.5, "rgba(255, 255, 255, 0.5)");
                gradient.addColorStop(1, "rgba(255, 255, 255, 0)");
                ctx.fillStyle = gradient;
                ctx.fillRect(0, 0, 64, 64);
                const texture = new THREE.CanvasTexture(canvas);
                resolve(texture);
            });

            const threatTexture = nodeTexture.clone(); // Use same texture for now

            // Load the UbuntuMono font texture for labels
            const fontTexture = await loadFontTexture();

            // Create geometries
            const geometryResult = createGraphGeometry(
                graph,
                nodesWidth,
                edgesWidth,
                epochsWidth,
                nodeTexture,
                threatTexture,
                fontTexture
            );
            geometryResultRef.current = geometryResult;

            // Add to scenes - always add label mesh but control visibility
            graphStructureRef.current.add(geometryResult.nodeMesh);
            graphStructureRef.current.add(geometryResult.edgeMesh);
            graphStructureRef.current.add(geometryResult.labelMesh);
            geometryResult.labelMesh.visible = showLabels;
            pickingSceneRef.current.add(geometryResult.pickingMesh);

            // Set up GPU picking
            if (cameraRef.current) {
                const picking = new GPUPicking(
                    rendererRef.current,
                    pickingSceneRef.current,
                    cameraRef.current,
                    simulatorRef.current
                );
                picking.setNodeNames(geometryResult.nodeNames);
                picking.resize(
                    containerRef.current!.clientWidth,
                    containerRef.current!.clientHeight
                );

                picking.onNodeSelect = (index, name) => {
                    setSelectedNode(name);
                    onNodeSelect?.(index, name);
                };

                picking.onNodeHover = (index, name) => {
                    setHoveredNode(name);
                    onNodeHover?.(index, name);
                };

                picking.onSelectionClear = () => {
                    setSelectedNode(null);
                };

                pickingRef.current = picking;
            }

            // Reset temperature for simulation - use node count / 2 like original
            const nodeCount = graph.getNodeCount();
            temperatureRef.current = nodeCount / 2;
            setIsSimulating(true);

            // Fit camera to show the entire graph after a short delay
            // (allow initial positions to be set)
            setTimeout(() => fitCameraToGraph(), 100);
        },
        [
            showLabels,
            initialTemperature,
            onNodeSelect,
            onNodeHover,
            fitCameraToGraph
        ]
    );

    /**
     * Animation loop
     */
    const animate = useCallback(() => {
        animationFrameRef.current = requestAnimationFrame(animate);

        if (
            !rendererRef.current ||
            !sceneRef.current ||
            !cameraRef.current ||
            !simulatorRef.current
        ) {
            return;
        }

        const delta = 1 / 60;

        // Update temperature
        if (temperatureRef.current > 0.1) {
            temperatureRef.current *= coolingRate;
        } else if (isSimulating) {
            setIsSimulating(false);
        }

        // Run simulation step
        simulatorRef.current.simulate(
            delta,
            temperatureRef.current,
            epochMin,
            epochMax
        );

        // Update material textures
        if (geometryResultRef.current) {
            updateMaterialTextures(
                geometryResultRef.current,
                simulatorRef.current.getPositionTexture(),
                simulatorRef.current.getNodeAttribTexture()
            );
        }

        // Update controls
        controlsRef.current?.update();

        // Update GPU picking
        if (mouseTrackerRef.current && pickingRef.current) {
            const mouseState = mouseTrackerRef.current.getState();
            pickingRef.current.update(mouseState);
        }

        // Render
        rendererRef.current.render(sceneRef.current, cameraRef.current);
    }, [coolingRate, epochMin, epochMax, isSimulating]);

    /**
     * Handle window resize
     */
    const handleResize = useCallback(() => {
        if (!containerRef.current || !rendererRef.current || !cameraRef.current)
            return;

        const width = containerRef.current.clientWidth;
        const height = containerRef.current.clientHeight;
        const dpr = window.devicePixelRatio;

        cameraRef.current.aspect = width / height;
        cameraRef.current.updateProjectionMatrix();

        // Set size to actual pixel dimensions, pass false to not update CSS style
        rendererRef.current.setSize(width * dpr, height * dpr, false);
        rendererRef.current.domElement.style.width = width + "px";
        rendererRef.current.domElement.style.height = height + "px";
        pickingRef.current?.resize(width, height);
    }, []);

    /**
     * Apply a specific layout
     */
    const applyLayout = useCallback(
        (layoutType: LayoutType) => {
            if (
                !simulatorRef.current ||
                !configRef.current ||
                !geometryResultRef.current
            )
                return;

            const { nodesWidth } = configRef.current;
            const nodesAndEdges = geometryResultRef.current.nodeNames.map(
                (_, i) => [i]
            );

            let layoutTexture: THREE.DataTexture | null = null;

            switch (layoutType) {
                case "circular":
                    layoutTexture = generateCircularLayout(
                        nodesAndEdges,
                        nodesWidth
                    );
                    break;
                case "spherical":
                    layoutTexture = generateSphericalLayout(
                        nodesAndEdges,
                        nodesWidth
                    );
                    break;
                case "helix":
                    layoutTexture = generateHelixLayout(
                        nodesAndEdges,
                        nodesWidth
                    );
                    break;
                case "grid":
                    layoutTexture = generateGridLayout(
                        nodesAndEdges,
                        nodesWidth
                    );
                    break;
                case "force":
                default:
                    // Reset to zero positions for force-directed
                    layoutTexture = generateCircularLayout(
                        nodesAndEdges,
                        nodesWidth
                    );
                    break;
            }

            if (layoutTexture) {
                simulatorRef.current.setLayoutPositions(layoutTexture);
                // Reheat simulation for layout transition - use modest temperature
                const nodeCount =
                    geometryResultRef.current?.nodeNames.length ?? 100;
                temperatureRef.current = nodeCount / 4; // Lower for layout transitions
                setIsSimulating(true);

                // Fit camera after layout change
                setTimeout(() => fitCameraToGraph(), 100);
            }

            setCurrentLayout(layoutType);
        },
        [fitCameraToGraph]
    );

    /**
     * Reheat simulation
     */
    const reheat = useCallback(() => {
        // Use node count / 2 like original
        const nodeCount = geometryResultRef.current?.nodeNames.length ?? 100;
        temperatureRef.current = nodeCount / 2;
        setIsSimulating(true);
    }, []);

    /**
     * Handle time slider change
     */
    const handleTimeChange = useCallback(
        (from: number, to: number) => {
            setTimeRange((prev) => ({ ...prev, from, to }));
            onTimeRangeChange?.(from, to);
        },
        [onTimeRangeChange]
    );

    // Initialize scene on mount
    useEffect(() => {
        const cleanup = initScene();
        return () => {
            cancelAnimationFrame(animationFrameRef.current);
            cleanup?.();
        };
    }, [initScene]);

    // Start animation loop
    useEffect(() => {
        animate();
        return () => {
            cancelAnimationFrame(animationFrameRef.current);
        };
    }, [animate]);

    // Handle resize
    useEffect(() => {
        window.addEventListener("resize", handleResize);
        return () => window.removeEventListener("resize", handleResize);
    }, [handleResize]);

    // Load external graph or create demo
    useEffect(() => {
        if (externalGraph) {
            loadGraph(externalGraph);
        } else {
            // Create demo graph
            const demoGraph = new Graph();
            generators.balancedTree(demoGraph, 7);
            loadGraph(demoGraph);
        }
    }, [externalGraph, loadGraph]);

    // Update visibility of edges/labels
    useEffect(() => {
        if (geometryResultRef.current) {
            geometryResultRef.current.edgeMesh.visible = showEdges;
            geometryResultRef.current.labelMesh.visible = showLabels;
        }
    }, [showEdges, showLabels]);

    return (
        <div className={`node-graph-container ${className ?? ""}`}>
            <div
                ref={containerRef}
                className="node-graph-canvas"
                style={{ height: "100%", width: "100%" }}
            />
            <div className="node-graph-controls">
                <div className="control-group">
                    <span className="control-label">Layout:</span>
                    <button
                        className={`control-btn ${currentLayout === "force" ? "active" : ""}`}
                        onClick={() => applyLayout("force")}
                    >
                        Force
                    </button>
                    <button
                        className={`control-btn ${currentLayout === "circular" ? "active" : ""}`}
                        onClick={() => applyLayout("circular")}
                    >
                        Circular
                    </button>
                    <button
                        className={`control-btn ${currentLayout === "spherical" ? "active" : ""}`}
                        onClick={() => applyLayout("spherical")}
                    >
                        Spherical
                    </button>
                    <button
                        className={`control-btn ${currentLayout === "helix" ? "active" : ""}`}
                        onClick={() => applyLayout("helix")}
                    >
                        Helix
                    </button>
                    <button
                        className={`control-btn ${currentLayout === "grid" ? "active" : ""}`}
                        onClick={() => applyLayout("grid")}
                    >
                        Grid
                    </button>
                </div>
                <div className="control-group">
                    <button className="control-btn" onClick={reheat}>
                        Reheat
                    </button>
                    <span className="status-indicator">
                        {isSimulating ? "🔥 Simulating" : "✓ Settled"}
                    </span>
                </div>
            </div>
            <div className="node-graph-info">
                {selectedNode && (
                    <div className="info-selected">
                        Selected: <strong>{selectedNode}</strong>
                        <span className="info-hint">
                            (double-click to clear)
                        </span>
                    </div>
                )}
                {hoveredNode && !selectedNode && (
                    <div className="info-hovered">
                        Hovering: <strong>{hoveredNode}</strong>
                    </div>
                )}
            </div>
            {showTimeSlider && (
                <TimeSlider
                    min={timeRange.min}
                    max={timeRange.max}
                    from={timeRange.from}
                    to={timeRange.to}
                    onChange={handleTimeChange}
                />
            )}
        </div>
    );
}

export default NodeGraph;
