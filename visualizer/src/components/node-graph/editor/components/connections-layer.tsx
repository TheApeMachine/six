import { Connection } from "../connection";
import {
	getConnectionDotsBetweenPoints,
	getInputPortRotationDegrees,
	getOutputPortRotationDegrees,
	getPortPositionFromRotation,
} from "../geometry";
import type { OperationSpec } from "../api";
import type { ActiveConnection, GraphConnection, GraphNode } from "../types";
import {
	dataTypeForOutput,
	portColorForDataType,
} from "@/components/operation/types";

interface ConnectionsLayerProps {
	nodes: GraphNode[];
	connections: GraphConnection[];
	activeConnection: ActiveConnection | null;
	operationSpecs: Record<string, OperationSpec>;
}

export const ConnectionsLayer = ({
	nodes,
	connections,
	activeConnection,
	operationSpecs,
}: ConnectionsLayerProps) => {
	return (
		<svg className="pointer-events-none absolute inset-0 h-full w-full">
			<title>Node graph connections</title>

			{/* Regular connections */}
			{connections.map((conn) => (
				<Connection
					key={conn.id}
					conn={conn}
					getColor={(c) => {
						const sourceNode = nodes.find((n) => n.id === c.sourceId);
						if (!sourceNode) return null;
						const spec = operationSpecs[sourceNode.type];
						const outputs = spec?.outputs?.length ? spec.outputs : ["out"];
						const outputName = c.sourceOutput ?? outputs[0];
						const outputIndex = outputs.indexOf(outputName);
						const index = outputIndex >= 0 ? outputIndex : 0;
						const outputType = spec?.outputType ?? "Tensor";
						const dataType = dataTypeForOutput(outputName, outputType, index);
						return portColorForDataType(dataType);
					}}
					getDots={(c) => {
						const sourceNode = nodes.find((n) => n.id === c.sourceId);
						const targetNode = nodes.find((n) => n.id === c.targetId);
						if (!sourceNode || !targetNode) return null;

						const sourceSpec = operationSpecs[sourceNode.type];
						const sourceOutputs = sourceSpec?.outputs?.length
							? sourceSpec.outputs
							: ["out"];
						const sourceOutput = c.sourceOutput ?? sourceOutputs[0];
						const sourceIndex = Math.max(
							0,
							sourceOutputs.indexOf(sourceOutput),
						);
						const sourceRotation = getOutputPortRotationDegrees({
							index: sourceIndex,
							total: sourceOutputs.length,
						});
						const sourcePort = getPortPositionFromRotation({
							nodeX: sourceNode.x,
							nodeY: sourceNode.y,
							rotationDegrees: sourceRotation,
						});

						const targetSpec = operationSpecs[targetNode.type];
						const targetInputs = targetSpec
							? [...targetSpec.requiredInputs, ...targetSpec.optionalInputs]
							: [];
						const targetInput = c.targetInput ?? targetInputs[0] ?? "in";
						const targetIndex = Math.max(
							0,
							targetInputs.indexOf(targetInput),
						);
						const targetRotation = getInputPortRotationDegrees({
							index: targetIndex,
							total: Math.max(1, targetInputs.length),
						});
						const targetPort = getPortPositionFromRotation({
							nodeX: targetNode.x,
							nodeY: targetNode.y,
							rotationDegrees: targetRotation,
						});

						return getConnectionDotsBetweenPoints({
							sourceX: sourcePort.x,
							sourceY: sourcePort.y,
							targetX: targetPort.x,
							targetY: targetPort.y,
						});
					}}
				/>
			))}

			{/* Active connection being drawn */}
			{activeConnection && (
				<ActiveConnectionLine
					nodes={nodes}
					activeConnection={activeConnection}
					operationSpecs={operationSpecs}
				/>
			)}
		</svg>
	);
};

interface ActiveConnectionLineProps {
	nodes: GraphNode[];
	activeConnection: ActiveConnection;
	operationSpecs: Record<string, OperationSpec>;
}

const ActiveConnectionLine = ({
	nodes,
	activeConnection,
	operationSpecs,
}: ActiveConnectionLineProps) => {
	const sourceNode = nodes.find((n) => n.id === activeConnection.sourceId);
	if (!sourceNode) return null;

	const sourceSpec = operationSpecs[sourceNode.type];
	const outputs = sourceSpec?.outputs?.length ? sourceSpec.outputs : ["out"];
	const outputName = activeConnection.sourceOutput ?? outputs[0];
	const outputIndex = Math.max(0, outputs.indexOf(outputName));
	const outputRotation = getOutputPortRotationDegrees({
		index: outputIndex,
		total: outputs.length,
	});
	const outputType = sourceSpec?.outputType ?? "Tensor";
	const outputColor = portColorForDataType(
		dataTypeForOutput(outputName, outputType, outputIndex),
	);
	const port = getPortPositionFromRotation({
		nodeX: sourceNode.x,
		nodeY: sourceNode.y,
		rotationDegrees: outputRotation,
	});

	const dots = getConnectionDotsBetweenPoints({
		sourceX: port.x,
		sourceY: port.y,
		targetX: activeConnection.tempEndX,
		targetY: activeConnection.tempEndY,
	});

	return (
		<g>
			{dots.map((dot) => (
				<circle
					key={`temp-dot-t${dot.t}`}
					cx={dot.x}
					cy={dot.y}
					r={3.5}
					fill={outputColor}
					opacity={dot.fade}
				/>
			))}
		</g>
	);
};
