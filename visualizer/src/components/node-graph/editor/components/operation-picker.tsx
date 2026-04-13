import { IconAlertCircle, IconSearch } from "@tabler/icons-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { OperationGroups, OperationSpec } from "../api";

export interface OperationMenuData {
	sourceId: string | null;
	x: number;
	y: number;
}

interface OperationPickerProps {
	menu: OperationMenuData & { sourceOutput: string | null };
	groups: OperationGroups;
	groupsError: string | null;
	sourceSpec: OperationSpec | null;
	operationSpecs: Record<string, OperationSpec>;
	onSelectOperation: (group: string, op: string) => void;
	onClose: () => void;
}

export function OperationPicker({
	menu,
	groups,
	groupsError,
	sourceSpec,
	operationSpecs,
	onSelectOperation,
	onClose,
}: OperationPickerProps) {
	const [search, setSearch] = useState("");
	const inputRef = useRef<HTMLInputElement>(null);

	useEffect(() => {
		const timer = setTimeout(() => {
			inputRef.current?.focus();
		}, 0);
		return () => clearTimeout(timer);
	}, []);

	useEffect(() => {
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				onClose();
			}
		};
		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [onClose]);

	const filteredGroups = useMemo(() => {
		if (!search.trim()) return groups;
		const term = search.toLowerCase();
		const next: OperationGroups = {};

		for (const [group, ops] of Object.entries(groups)) {
			const groupMatch = group.toLowerCase().includes(term);

			const matchingOps = ops.filter(
				(op) => groupMatch || op.toLowerCase().includes(term),
			);

			if (matchingOps.length > 0) {
				next[group] = matchingOps;
			}
		}
		return next;
	}, [groups, search]);

	return (
		<button
			type="button"
			className="fixed inset-0 z-50 h-full w-full cursor-default border-none bg-transparent p-0 outline-none"
			onClick={onClose}
			onKeyDown={(e) => {
				if (e.key === "Escape") onClose();
			}}
			tabIndex={-1}
			onContextMenu={(e) => {
				e.preventDefault();
				onClose();
			}}
		>
			<Card
				className="absolute flex flex-col overflow-hidden shadow-xl animate-in fade-in zoom-in-95 duration-100"
				style={{
					left: menu.x,
					top: menu.y,
					width: "16rem",
					maxHeight: "400px",
				}}
				onClick={(e) => e.stopPropagation()}
			>
				<div className="p-2 border-b bg-muted/20 space-y-2">
					{sourceSpec && (
						<div className="flex items-center gap-1.5 px-1 pb-1">
							<span className="text-[10px] text-muted-foreground uppercase font-medium tracking-wider">
								From
							</span>
							<Badge
								variant="outline"
								className="text-[10px] h-4 px-1.5 font-normal bg-background/50"
							>
								{sourceSpec.key}
							</Badge>
						</div>
					)}
					<div className="relative">
						<IconSearch className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
						<Input
							ref={inputRef}
							value={search}
							onChange={(e) => setSearch(e.target.value)}
							placeholder="Search..."
							className="pl-8 h-7 text-xs bg-background"
						/>
					</div>
				</div>

				<ScrollArea className="flex-1">
					<div className="p-1">
						{groupsError ? (
							<div className="flex flex-col items-center justify-center p-6 text-destructive gap-2">
								<IconAlertCircle className="w-5 h-5" />
								<span className="text-xs text-center font-medium">
									{groupsError}
								</span>
							</div>
						) : Object.keys(filteredGroups).length === 0 ? (
							<div className="p-8 text-center text-xs text-muted-foreground">
								No operations found
							</div>
						) : (
							Object.entries(filteredGroups).map(([group, ops]) => (
								<div key={group} className="mb-2 last:mb-0">
									<div className="px-2 py-1.5 text-[10px] font-semibold text-muted-foreground uppercase tracking-wider sticky top-0 bg-card/95 backdrop-blur-sm z-10">
										{group}
									</div>
									<div className="grid gap-0.5 px-1">
										{ops.map((op) => (
											<button
												key={op}
												type="button"
												className="flex items-center w-full text-left px-2 py-1.5 text-xs rounded-sm hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground outline-none transition-colors"
												onClick={() => onSelectOperation(group, op)}
											>
												{op}
											</button>
										))}
									</div>
								</div>
							))
						)}
					</div>
				</ScrollArea>
			</Card>
		</button>
	);
}
