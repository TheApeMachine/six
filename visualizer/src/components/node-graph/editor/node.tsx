import type React from "react";
import { cn } from "@/lib/utils";
import { NODE_RADIUS, PORT_RADIUS } from "./constants";
import type { ActiveConnection, GraphNode, NodeId } from "./types";

interface NodeProps {
	node: GraphNode;
	isDragging: boolean;
	portMarkers: readonly { id: string; rotationDegrees: number }[];
	inputPorts: readonly {
		name: string;
		rotationDegrees: number;
		isConnected: boolean;
		color: string;
		isRequired: boolean;
	}[];
	outputPorts: readonly {
		name: string;
		rotationDegrees: number;
		color: string;
	}[];
	badgeCount: number;
	activeConnection: ActiveConnection | null;
	onBadgeClick: (nodeId: NodeId) => void;
	onDragStart: (e: React.MouseEvent<HTMLElement>, nodeId: NodeId) => void;
	onStartConnection: (
		e: React.MouseEvent<HTMLElement>,
		nodeId: NodeId,
		outputName: string,
	) => void;
}

export const Node = ({
	node,
	isDragging,
	portMarkers,
	inputPorts,
	outputPorts,
	badgeCount,
	activeConnection,
	onBadgeClick,
	onDragStart,
	onStartConnection,
}: NodeProps) => {
	const Icon = node.icon;

	const isDraggingToThisNode =
		activeConnection && activeConnection.sourceId !== node.id;

	const connectedPorts = inputPorts.filter((p) => p.isConnected);
	const unconnectedPorts = inputPorts.filter((p) => !p.isConnected);

	return (
		<div
			className="absolute box-border"
			role="presentation"
			style={{
				width: NODE_RADIUS * 2,
				height: NODE_RADIUS * 2,
				left: node.x,
				top: node.y,
			}}
		>
			{portMarkers.map((p) => (
				<div
					key={p.id}
					className="bg-background pointer-events-none absolute z-18 box-border flex items-center justify-center rounded-full border-2"
					style={{
						width: PORT_RADIUS * 2,
						height: PORT_RADIUS * 2,
						borderColor: node.color,
						top: NODE_RADIUS - PORT_RADIUS,
						left: NODE_RADIUS - PORT_RADIUS,
						transform: `rotate(${p.rotationDegrees}deg) translate(${NODE_RADIUS}px, 0)`,
						transformOrigin: "center center",
					}}
				>
					<div
						className="size-1.5 rounded-full"
						style={{ backgroundColor: node.color }}
					/>
				</div>
			))}

			{connectedPorts.map((p) => (
				<div
					key={p.name}
					title={`${p.name} (${p.isRequired ? "required" : "optional"})`}
					className="bg-background pointer-events-none absolute z-19 box-border flex items-center justify-center rounded-full border-2"
					style={{
						width: PORT_RADIUS * 2,
						height: PORT_RADIUS * 2,
						borderColor: p.color,
						top: NODE_RADIUS - PORT_RADIUS,
						left: NODE_RADIUS - PORT_RADIUS,
						transform: `rotate(${p.rotationDegrees}deg) translate(${NODE_RADIUS}px, 0)`,
						transformOrigin: "center center",
					}}
				>
					<div
						className="size-1.5 rounded-full"
						style={{ backgroundColor: p.color }}
					/>
				</div>
			))}

			{isDraggingToThisNode &&
				unconnectedPorts.map((p) => (
					<div
						key={p.name}
						title={`${p.name} (${p.isRequired ? "required" : "optional"})`}
						className="pointer-events-none absolute z-19 box-border flex items-center justify-center rounded-full border-2 transition-opacity duration-150"
						style={{
							width: PORT_RADIUS * 2,
							height: PORT_RADIUS * 2,
						borderColor: p.color,
						backgroundColor: "var(--muted)",
							opacity: 0.5,
							top: NODE_RADIUS - PORT_RADIUS,
							left: NODE_RADIUS - PORT_RADIUS,
							transform: `rotate(${p.rotationDegrees}deg) translate(${NODE_RADIUS}px, 0)`,
							transformOrigin: "center center",
						}}
					>
						<div
							className="size-1.5 rounded-full"
							style={{ backgroundColor: p.color, opacity: 0.6 }}
						/>
					</div>
				))}

			{outputPorts.map((p) => (
				<button
					key={p.name}
					type="button"
					aria-label={`Start connection from ${node.name}.${p.name}`}
					className="bg-background absolute z-20 box-border flex cursor-pointer items-center justify-center rounded-full border-2 p-0"
					style={{
						width: PORT_RADIUS * 2,
						height: PORT_RADIUS * 2,
						borderColor: p.color,
						top: NODE_RADIUS - PORT_RADIUS,
						left: NODE_RADIUS - PORT_RADIUS,
						transform: `rotate(${p.rotationDegrees}deg) translate(${NODE_RADIUS}px, 0)`,
						transformOrigin: "center center",
					}}
					onMouseDown={(e) => {
						e.stopPropagation();
						onStartConnection(e, node.id, p.name);
					}}
				>
					<div
						className="size-1.5 rounded-full"
						style={{ backgroundColor: p.color }}
					/>
				</button>
			))}

			<button
				type="button"
				className={cn(
					"border-background relative z-25 box-border flex size-full items-center justify-center rounded-full border-4 p-0 shadow-md",
					isDragging ? "cursor-grabbing" : "cursor-grab",
				)}
				style={{ backgroundColor: node.color }}
				onMouseDown={(e) => onDragStart(e, node.id)}
			>
				<Icon className="text-primary-foreground size-8" />
			</button>

			{badgeCount > 0 && (
				<button
					type="button"
					aria-label={`Missing ${badgeCount} inputs`}
					className="bg-destructive text-destructive-foreground absolute -right-1 -top-1 z-40 flex size-6 cursor-pointer items-center justify-center rounded-full border-none p-0 text-sm font-bold"
					onMouseDown={(e) => e.stopPropagation()}
					onClick={() => onBadgeClick(node.id)}
				>
					{badgeCount}
				</button>
			)}

			<div className="text-foreground absolute inset-x-0 -bottom-6 text-center text-sm font-medium">
				{node.name}
			</div>
		</div>
	);
};
