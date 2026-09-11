import type { QueryClient, UseQueryOptions } from "react-query";
import type { RepositoryCatalogResponse } from "#/api/api";
import { API } from "#/api/api";

export const repositoryCatalogQueryKey = ["repository-catalog"] as const;

/**
 * Repository-driven "Attach workspace" picker: lists the configured GitHub
 * organization's accessible, non-archived repositories, each annotated with
 * the current user's existing workspace id (if any). 404s when the
 * deployment has not configured repository-catalog-github-org/-token — the
 * picker falls back to the plain workspace list in that case, so this query
 * is only enabled lazily, when the Attach Workspace menu is actually opened
 * (listing must stay cheap and must never provision compute on its own).
 */
export const repositoryCatalog = (
	enabled: boolean,
): UseQueryOptions<RepositoryCatalogResponse> => ({
	queryKey: repositoryCatalogQueryKey,
	queryFn: () => API.getRepositoryCatalog(),
	enabled,
	staleTime: 15_000,
	retry: false,
});

export const attachRepositoryWorkspace = (queryClient: QueryClient) => ({
	mutationFn: ({ owner, repo }: { owner: string; repo: string }) =>
		API.attachRepositoryWorkspace(owner, repo),
	onSuccess: async () => {
		// A newly created (or newly discovered) workspace changes both the
		// repository catalog's per-repo workspace_id and the plain
		// owner:me workspace list used elsewhere in the chat UI.
		await queryClient.invalidateQueries({
			queryKey: repositoryCatalogQueryKey,
		});
		await queryClient.invalidateQueries({ queryKey: ["workspaces"] });
	},
});
