import { useEffect, useState } from "react";
import {
	isOperationGroupsResponse,
	type OperationGroups,
} from "../api";

interface UseOperationGroupsOptions {
	baseUrl: string;
}

interface UseOperationGroupsResult {
	groups: OperationGroups;
	error: string | null;
	isLoading: boolean;
}

/**
 * Hook to fetch and manage operation groups from the backend
 */
export function useOperationGroups({
	baseUrl,
}: UseOperationGroupsOptions): UseOperationGroupsResult {
	const [groups, setGroups] = useState<OperationGroups>({});
	const [error, setError] = useState<string | null>(null);
	const [isLoading, setIsLoading] = useState(true);

	useEffect(() => {
		let cancelled = false;
		setError(null);
		setIsLoading(true);

		fetch(`${baseUrl}/api/operations`)
			.then(async (res) => {
				if (!res.ok) {
					throw new Error(
						`Failed to fetch operations: ${res.status} ${res.statusText}`,
					);
				}
				return res.json() as Promise<unknown>;
			})
			.then((x) => {
				if (cancelled) return;
				if (!isOperationGroupsResponse(x)) {
					throw new Error("Backend returned invalid operations payload");
				}
				setGroups(x.groups);
			})
			.catch((e: unknown) => {
				if (cancelled) return;
				const msg = e instanceof Error ? e.message : String(e);
				setError(msg);
			})
			.finally(() => {
				if (!cancelled) setIsLoading(false);
			});

		return () => {
			cancelled = true;
		};
	}, [baseUrl]);

	return { groups, error, isLoading };
}
