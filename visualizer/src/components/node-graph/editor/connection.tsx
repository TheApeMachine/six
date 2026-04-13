import type { Dot, GraphConnection } from "./types";

interface ConnectionProps {
	conn: GraphConnection;
	getDots: (conn: GraphConnection) => Dot[] | null;
	getColor: (conn: GraphConnection) => string | null;
}

export const Connection = ({ conn, getDots, getColor }: ConnectionProps) => {
	const dots = getDots(conn);
	const color = getColor(conn);
	if (!dots || !color) return null;

	return (
		<g>
			{dots.map((dot) => (
				<circle
					key={`${conn.id}-t${dot.t}`}
					cx={dot.x}
					cy={dot.y}
					r={3.5}
					fill={color}
					opacity={dot.fade}
				/>
			))}

			{/* Flow indicators */}
			{[0.25, 0.5, 0.75].map((offset) => {
				const idx = Math.floor(dots.length * offset);
				if (idx < 1 || idx >= dots.length - 1) return null;

				const prevDot = dots[idx - 1];
				const dot = dots[idx];
				const nextDot = dots[idx + 1];

				const angle =
					(radiansToDegrees(
						Math.atan2(nextDot.y - prevDot.y, nextDot.x - prevDot.x),
					) %
						360) ||
					0;

				return (
					<path
						key={`arrow-${offset}`}
						d="M-6,-3 L0,0 L-6,3"
						fill="none"
						stroke={color}
						strokeWidth="2"
						transform={`translate(${dot.x}, ${dot.y}) rotate(${angle})`}
						opacity={0.8}
						strokeLinecap="round"
						strokeLinejoin="round"
					/>
				);
			})}
		</g>
	);
};

function radiansToDegrees(radians: number): number {
	return (radians * 180) / Math.PI;
}