import * as React from "react";
import { cn } from "@/lib/utils";

/*
Flex is a simple flex-container primitive used by NodeGraphLegacy controls.
Variants map to common layout presets.
*/

interface FlexProps extends React.HTMLAttributes<HTMLDivElement> {
	variant?: "panel" | "row" | "col" | "center";
	gap?: number;
}

const VARIANT_CLASSES: Record<string, string> = {
	panel:
		"flex flex-row items-center gap-1.5 rounded-lg border border-white/15 bg-black/70 p-1.5 backdrop-blur-sm",
	row: "flex flex-row items-center",
	col: "flex flex-col",
	center: "flex items-center justify-center",
};

export const Flex = React.forwardRef<HTMLDivElement, FlexProps>(
	({ className, variant = "row", gap, style, ...props }, ref) => (
		<div
			ref={ref}
			className={cn(VARIANT_CLASSES[variant], className)}
			style={gap !== undefined ? { gap: `${gap * 4}px`, ...style } : style}
			{...props}
		/>
	),
);
Flex.displayName = "Flex";
