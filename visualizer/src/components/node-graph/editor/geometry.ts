import { NODE_RADIUS } from "./constants";
import type {
	ActiveConnection,
	Dot,
	GraphConnection,
	GraphNode,
	NodeId,
	PortPosition,
} from "./types";

function radiansToDegrees(radians: number): number {
	return (radians * 180) / Math.PI;
}

function degreesToRadians(degrees: number): number {
	return (degrees * Math.PI) / 180;
}

export function getPortPosition(params: {
	nodeX: number;
	nodeY: number;
	targetX: number;
	targetY: number;
}): PortPosition {
	const nodeCenterX = params.nodeX + NODE_RADIUS;
	const nodeCenterY = params.nodeY + NODE_RADIUS;

	const angleRadians = Math.atan2(
		params.targetY - nodeCenterY,
		params.targetX - nodeCenterX,
	);

	return {
		x: nodeCenterX + NODE_RADIUS * Math.cos(angleRadians),
		y: nodeCenterY + NODE_RADIUS * Math.sin(angleRadians),
		angleDegrees: radiansToDegrees(angleRadians),
	};
}

export function getPortPositionFromRotation(params: {
	nodeX: number;
	nodeY: number;
	rotationDegrees: number;
}): { x: number; y: number } {
	const nodeCenterX = params.nodeX + NODE_RADIUS;
	const nodeCenterY = params.nodeY + NODE_RADIUS;
	const angleRadians = degreesToRadians(params.rotationDegrees);
	return {
		x: nodeCenterX + NODE_RADIUS * Math.cos(angleRadians),
		y: nodeCenterY + NODE_RADIUS * Math.sin(angleRadians),
	};
}

export function getInputPortRotationDegrees(params: {
	index: number;
	total: number;
}): number {
	// Inputs live on the left hemisphere: 120deg (top-left) -> 240deg (bottom-left).
	// For 1 input, place it exactly at 180deg (left).
	if (params.total <= 1) return 180;
	const start = 120;
	const end = 240;
	const t = params.index / (params.total - 1);
	return start + (end - start) * t;
}

export function getOutputPortRotationDegrees(params: {
	index: number;
	total: number;
}): number {
	// Outputs live on the right hemisphere: -60deg (top-right) -> 60deg (bottom-right).
	// For 1 output, place it exactly at 0deg (right).
	if (params.total <= 1) return 0;
	const start = -60;
	const end = 60;
	const t = params.index / (params.total - 1);
	return start + (end - start) * t;
}

export function getConnectionDots(params: {
	sourceNode: GraphNode;
	targetNode: GraphNode;
	count?: number;
}): Dot[] {
	const count = params.count ?? 25;
	const sourceCenterX = params.sourceNode.x + NODE_RADIUS;
	const sourceCenterY = params.sourceNode.y + NODE_RADIUS;
	const targetCenterX = params.targetNode.x + NODE_RADIUS;
	const targetCenterY = params.targetNode.y + NODE_RADIUS;

	const sourcePort = getPortPosition({
		nodeX: params.sourceNode.x,
		nodeY: params.sourceNode.y,
		targetX: targetCenterX,
		targetY: targetCenterY,
	});

	const targetPort = getPortPosition({
		nodeX: params.targetNode.x,
		nodeY: params.targetNode.y,
		targetX: sourceCenterX,
		targetY: sourceCenterY,
	});

	const dots: Dot[] = [];
	for (let i = 0; i <= count; i++) {
		const t = i / count;
		dots.push({
			x: sourcePort.x + (targetPort.x - sourcePort.x) * t,
			y: sourcePort.y + (targetPort.y - sourcePort.y) * t,
			fade: 0.8 - Math.abs(0.5 - t) * 0.4,
			t,
		});
	}

	return dots;
}

export function getConnectionDotsBetweenPoints(params: {
	sourceX: number;
	sourceY: number;
	targetX: number;
	targetY: number;
	count?: number;
}): Dot[] {
	const count = params.count ?? 25;
	const dots: Dot[] = [];
	for (let i = 0; i <= count; i++) {
		const t = i / count;
		dots.push({
			x: params.sourceX + (params.targetX - params.sourceX) * t,
			y: params.sourceY + (params.targetY - params.sourceY) * t,
			fade: 0.8 - Math.abs(0.5 - t) * 0.4,
			t,
		});
	}
	return dots;
}

export function getTempConnectionDots(params: {
	sourceNode: GraphNode;
	tempX: number;
	tempY: number;
	count?: number;
}): Dot[] {
	const count = params.count ?? 25;

	const sourcePort = getPortPosition({
		nodeX: params.sourceNode.x,
		nodeY: params.sourceNode.y,
		targetX: params.tempX,
		targetY: params.tempY,
	});

	const dots: Dot[] = [];
	for (let i = 0; i <= count; i++) {
		const t = i / count;
		dots.push({
			x: sourcePort.x + (params.tempX - sourcePort.x) * t,
			y: sourcePort.y + (params.tempY - sourcePort.y) * t,
			fade: 0.8 - t * 0.4,
			t,
		});
	}

	return dots;
}

export function getPortRotationDegreesForNode(params: {
	nodeId: NodeId;
	nodes: readonly GraphNode[];
	connections: readonly GraphConnection[];
	activeConnection: ActiveConnection | null;
}): number {
	const node = params.nodes.find((n) => n.id === params.nodeId);
	if (!node) return 0;

	const nodeCenterX = node.x + NODE_RADIUS;
	const nodeCenterY = node.y + NODE_RADIUS;

	if (params.activeConnection && params.activeConnection.sourceId === params.nodeId) {
		return radiansToDegrees(
			Math.atan2(
				params.activeConnection.tempEndY - nodeCenterY,
				params.activeConnection.tempEndX - nodeCenterX,
			),
		);
	}

	const sourceConn = params.connections.find((c) => c.sourceId === params.nodeId);
	if (sourceConn) {
		const targetNode = params.nodes.find((n) => n.id === sourceConn.targetId);
		if (!targetNode) return 0;

		return radiansToDegrees(
			Math.atan2(
				targetNode.y + NODE_RADIUS - nodeCenterY,
				targetNode.x + NODE_RADIUS - nodeCenterX,
			),
		);
	}

	// Node has no outgoing connections - position output port opposite to incoming connections
	const incomingConns = params.connections.filter((c) => c.targetId === params.nodeId);
	if (incomingConns.length > 0) {
		// Calculate average angle of all incoming connections
		let sumX = 0;
		let sumY = 0;
		for (const conn of incomingConns) {
			const sourceNode = params.nodes.find((n) => n.id === conn.sourceId);
			if (sourceNode) {
				sumX += sourceNode.x + NODE_RADIUS - nodeCenterX;
				sumY += sourceNode.y + NODE_RADIUS - nodeCenterY;
			}
		}
		// Point output port in the OPPOSITE direction (add 180 degrees)
		const incomingAngle = radiansToDegrees(Math.atan2(sumY, sumX));
		return incomingAngle + 180;
	}

	return 0;
}

