import { useEffect, useRef, useState } from "react";
import { isOperationSpecResponse, type OperationSpec } from "../api";
import type { GraphNode } from "../types";

interface UseOperationSpecsOptions {
	baseUrl: string;
	nodes: GraphNode[];
}

interface UseOperationSpecsResult {
	specs: Record<string, OperationSpec>;
}

/**
 * Hook to fetch operation specs for nodes that need them
 *
 * Automatically fetches specs for any node whose type contains a "."
 * (indicating it's an operation like "math.add")
 */
export function useOperationSpecs({
	baseUrl,
	nodes,
}: UseOperationSpecsOptions): UseOperationSpecsResult {
	const [specs, setSpecs] = useState<Record<string, OperationSpec>>({});
	const pendingRef = useRef<Set<string>>(new Set());

	useEffect(() => {
		const needed = nodes
			.map((n) => n.type)
			.filter((t) => t !== "graph.input" && t !== "graph.output")
			.filter((t) => !specs[t]);

		const unique = Array.from(new Set(needed));
		const toFetch = unique.filter((k) => !pendingRef.current.has(k));
		if (toFetch.length === 0) return;

		for (const opKey of toFetch) pendingRef.current.add(opKey);

		let cancelled = false;
		Promise.all(
			toFetch.map(async (opKey) => {
				const res = await fetch(
					`${baseUrl}/api/operation?op=${encodeURIComponent(opKey)}`,
				);
				if (!res.ok) {
					throw new Error(
						`Failed to fetch operation spec (${opKey}): ${res.status} ${res.statusText}`,
					);
				}
				const x = (await res.json()) as unknown;
				if (!isOperationSpecResponse(x)) {
					throw new Error(`Backend returned invalid spec for ${opKey}`);
				}
				return x.operation;
			}),
		)
			.then((ops) => {
				if (cancelled) return;
				setSpecs((prev) => {
					const next = { ...prev };
					for (const op of ops) next[op.key] = op;
					return next;
				});
			})
			.catch(() => {
				// Silently handle errors - the UI will show "unknown spec"
			})
			.finally(() => {
				for (const opKey of toFetch) pendingRef.current.delete(opKey);
			});

		return () => {
			cancelled = true;
		};
	}, [baseUrl, nodes, specs]);

	return { specs };
}
