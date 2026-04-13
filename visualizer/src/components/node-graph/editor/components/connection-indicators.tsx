import { NODE_RADIUS, PORT_RADIUS } from "../constants";
import type { ActiveConnection, GraphNode } from "../types";

interface ConnectionTargetIndicatorProps {
	activeConnection: ActiveConnection;
}

export const ConnectionTargetIndicator = ({
	activeConnection,
}: ConnectionTargetIndicatorProps) => {
	return (
		<div
			className="border-muted-foreground/50 bg-background/70 pointer-events-none absolute size-[30px] rounded-full border-2 border-dashed"
			style={{
				left: activeConnection.tempEndX - 15,
				top: activeConnection.tempEndY - 15,
			}}
		/>
	);
};

interface NodeReceiverHighlightProps {
	node: GraphNode;
	activeConnection: ActiveConnection;
}

export const NodeReceiverHighlight = ({
	node,
	activeConnection,
}: NodeReceiverHighlightProps) => {
	if (node.id === activeConnection.sourceId) return null;

	const nodeCenterX = node.x + NODE_RADIUS;
	const nodeCenterY = node.y + NODE_RADIUS;
	const dx = activeConnection.tempEndX - nodeCenterX;
	const dy = activeConnection.tempEndY - nodeCenterY;
	const distance = Math.sqrt(dx * dx + dy * dy);

	if (distance >= NODE_RADIUS + 20) return null;

	const angle = Math.atan2(dy, dx);
	const portX = nodeCenterX + NODE_RADIUS * Math.cos(angle);
	const portY = nodeCenterY + NODE_RADIUS * Math.sin(angle);

	return (
		<div
			className="bg-background/90 pointer-events-none absolute z-30 rounded-full border-2"
			style={{
				width: PORT_RADIUS * 2.5,
				height: PORT_RADIUS * 2.5,
				borderColor: node.color,
				left: portX - PORT_RADIUS * 1.25,
				top: portY - PORT_RADIUS * 1.25,
			}}
		/>
	);
};
