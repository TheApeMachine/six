/**
 * Weight statistics visualization
 *
 * Displays detailed statistics for a parameter tensor:
 * - Distribution histogram
 * - Heatmap visualization
 * - Summary statistics
 */

import { IconArrowRight } from "@tabler/icons-react";
import { useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { WeightStats } from "./types";
import { formatParamCount, formatShape } from "./types";

interface WeightStatsViewProps {
	stats: WeightStats;
	onDrillDown?: () => void;
	className?: string;
}

export function WeightStatsView({
	stats,
	onDrillDown,
	className,
}: WeightStatsViewProps) {
	// Calculate histogram bar heights
	const histogramBars = useMemo(() => {
		if (!stats.histogramCounts || stats.histogramCounts.length === 0) return [];

		const maxCount = Math.max(...stats.histogramCounts);
		const maxHeight = 100;

		return stats.histogramCounts.map((count, i) => ({
			count,
			height: maxCount > 0 ? (count / maxCount) * maxHeight : 0,
			binStart: stats.histogramBins[i],
			binEnd: stats.histogramBins[i + 1],
		}));
	}, [stats.histogramCounts, stats.histogramBins]);

	// Generate heatmap colors
	const heatmapCells = useMemo(() => {
		if (!stats.heatmapData || stats.heatmapData.length === 0) return [];

		const minVal = Math.min(...stats.heatmapData);
		const maxVal = Math.max(...stats.heatmapData);
		const range = maxVal - minVal || 1;

		return stats.heatmapData.map((value) => {
			const normalized = (value - minVal) / range;
			return valueToColor(normalized);
		});
	}, [stats.heatmapData]);

	return (
		<div className={cn("space-y-4", className)}>
			{/* Header Card */}
			<Card>
				<CardHeader>
					<div className="flex items-start justify-between">
						<div>
							<CardTitle className="font-mono text-base">
								{stats.paramId}
							</CardTitle>
							<CardDescription className="mt-1 flex flex-wrap items-center gap-2">
								<Badge variant="outline">{formatShape(stats.shape)}</Badge>
								<Badge variant="secondary">{stats.dtype}</Badge>
								<Badge variant="secondary">
									{formatParamCount(stats.paramCount)} params
								</Badge>
							</CardDescription>
						</div>
						{onDrillDown && (
							<Button variant="outline" size="sm" onClick={onDrillDown}>
								View Neurons
								<IconArrowRight className="ml-1.5 size-4" />
							</Button>
						)}
					</div>
				</CardHeader>
			</Card>

			{/* Stats Grid */}
			<div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-6">
				<StatCard label="Mean" value={stats.mean.toFixed(4)} />
				<StatCard label="Std" value={stats.std.toFixed(4)} />
				<StatCard label="Min" value={stats.min.toFixed(4)} />
				<StatCard label="Max" value={stats.max.toFixed(4)} />
				<StatCard
					label="Sparsity"
					value={`${(stats.sparsity * 100).toFixed(1)}%`}
					highlight={stats.sparsity > 0.5}
				/>
				{stats.gradientNorm > 0 && (
					<StatCard label="Grad Norm" value={stats.gradientNorm.toFixed(4)} />
				)}
			</div>

			{/* Visualizations */}
			<div className="grid gap-4 md:grid-cols-2">
				{/* Histogram */}
				<Card>
					<CardHeader className="pb-2">
						<CardTitle className="text-sm font-medium text-muted-foreground">
							Distribution
						</CardTitle>
					</CardHeader>
					<CardContent>
						<div className="h-28">
							<div className="flex h-24 items-end gap-px">
								{histogramBars.map((bar) => (
									<Tooltip key={bar.binStart}>
										<TooltipTrigger asChild>
											<div
												className="flex-1 rounded-t bg-linear-to-t from-primary to-primary/60 transition-colors hover:from-primary/80 hover:to-primary/40"
												style={{ height: `${bar.height}%` }}
											/>
										</TooltipTrigger>
										<TooltipContent>
											<p>
												{bar.binStart?.toFixed(3)} to {bar.binEnd?.toFixed(3)}
											</p>
											<p className="text-muted-foreground">
												Count: {bar.count}
											</p>
										</TooltipContent>
									</Tooltip>
								))}
							</div>
							<div className="mt-1 flex justify-between text-[10px] text-muted-foreground">
								<span>{stats.min.toFixed(2)}</span>
								<span>0</span>
								<span>{stats.max.toFixed(2)}</span>
							</div>
						</div>
					</CardContent>
				</Card>

				{/* Heatmap */}
				{heatmapCells.length > 0 && (
					<Card>
						<CardHeader className="pb-2">
							<CardTitle className="text-sm font-medium text-muted-foreground">
								Weight Matrix
							</CardTitle>
						</CardHeader>
						<CardContent>
							<div
								className="grid aspect-square max-h-48 gap-px"
								style={{
									gridTemplateColumns: `repeat(${stats.heatmapWidth}, 1fr)`,
									gridTemplateRows: `repeat(${stats.heatmapHeight}, 1fr)`,
								}}
							>
								{heatmapCells.map((color, i) => (
									<div
										key={`heatmap-cell-${i}-${color}`}
										className="rounded-[1px]"
										style={{ backgroundColor: color }}
									/>
								))}
							</div>
							<div className="mt-2 flex items-center justify-center gap-2 text-[10px] text-muted-foreground">
								<span>Low</span>
								<div className="h-2 w-16 rounded bg-linear-to-r from-blue-800 via-emerald-500 to-red-500" />
								<span>High</span>
							</div>
						</CardContent>
					</Card>
				)}
			</div>
		</div>
	);
}

function StatCard({
	label,
	value,
	highlight = false,
}: {
	label: string;
	value: string;
	highlight?: boolean;
}) {
	return (
		<Card
			size="sm"
			className={cn(highlight && "border-primary/50 bg-primary/5")}
		>
			<CardContent className="p-3 text-center">
				<p className="text-[10px] uppercase tracking-wide text-muted-foreground">
					{label}
				</p>
				<p className="mt-1 font-mono text-sm font-semibold">{value}</p>
			</CardContent>
		</Card>
	);
}

/**
 * Convert a normalized value [0, 1] to a heatmap color
 */
function valueToColor(value: number): string {
	// Viridis-like colormap
	const r = Math.round(68 + value * 187);
	const g = Math.round(1 + value * 200 - value * value * 100);
	const b = Math.round(84 + value * 100 - value * value * 150);
	return `rgb(${r}, ${g}, ${b})`;
}
