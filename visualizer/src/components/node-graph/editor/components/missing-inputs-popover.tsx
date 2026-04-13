import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverHeader,
	PopoverTitle,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

export interface MissingInputsPopoverData {
	nodeId: string;
	x: number;
	y: number;
	opKey: string;
	requiredInputs: string[];
	connectedInputs: number;
	missingInputs: string[];
}

interface MissingInputsPopoverProps {
	data: MissingInputsPopoverData;
	onClose: () => void;
}

/**
 * Popover showing which required inputs are missing for a node
 */
export const MissingInputsPopover = ({
	data,
	onClose,
}: MissingInputsPopoverProps) => {
	return (
		<Popover open onOpenChange={(open) => !open && onClose()}>
			{/* Virtual anchor positioned at the node */}
			<div
				style={{
					position: "absolute",
					left: data.x,
					top: data.y,
					width: 1,
					height: 1,
				}}
			/>
			<PopoverContent
				side="right"
				align="start"
				sideOffset={20}
				className="w-80"
				style={{
					position: "absolute",
					left: data.x + 20,
					top: data.y - 10,
				}}
			>
				<PopoverHeader className="flex-row items-center justify-between">
					<PopoverTitle>
						Missing inputs ({data.missingInputs.length})
					</PopoverTitle>
					<Button variant="ghost" size="xs" onClick={onClose}>
						Close
					</Button>
				</PopoverHeader>

				<div className="text-muted-foreground text-xs">
					<div className="text-foreground font-medium">{data.opKey}</div>
					<div className="mt-1">
						Connected: {data.connectedInputs} / Required:{" "}
						{data.requiredInputs.length}
					</div>
				</div>

				<div>
					{data.missingInputs.length === 0 ? (
						<div className="text-muted-foreground text-sm">
							No missing inputs.
						</div>
					) : (
						<div className="text-sm">
							<div className="text-foreground font-medium">Required inputs</div>
							<ul className="mt-1 space-y-0.5">
								{data.requiredInputs.map((name) => {
									const isMissing = data.missingInputs.includes(name);
									return (
										<li
											key={name}
											className="flex items-center justify-between py-0.5"
										>
											<span>{name}</span>
											<span
												className={cn(
													isMissing ? "text-destructive" : "text-success",
												)}
											>
												{isMissing ? "missing" : "connected"}
											</span>
										</li>
									);
								})}
							</ul>
						</div>
					)}
				</div>
			</PopoverContent>
		</Popover>
	);
};
