import type {
	AIGatewaySpendSortBy,
	AIGatewaySpendSortOrder,
} from "#/api/typesGenerated";

export const spendSortColumns: {
	field: AIGatewaySpendSortBy;
	label: string;
}[] = [
	{ field: "username", label: "User" },
	{ field: "total_cost_micros", label: "Cost" },
	{ field: "request_count", label: "Requests" },
	{ field: "session_count", label: "Sessions" },
	{ field: "input_tokens", label: "Input" },
	{ field: "output_tokens", label: "Output" },
	{ field: "cache_read_input_tokens", label: "Cache read" },
	{ field: "cache_write_input_tokens", label: "Cache write" },
];

/** Resolves URL sorting to the supported server fields and defaults. */
export function spendUsersSort(params: URLSearchParams): {
	sort_by: AIGatewaySpendSortBy;
	sort_order: AIGatewaySpendSortOrder;
} {
	return {
		sort_by:
			spendSortColumns.find(({ field }) => field === params.get("sort_by"))
				?.field ?? "total_cost_micros",
		sort_order: params.get("sort_order") === "asc" ? "asc" : "desc",
	};
}
