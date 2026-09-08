import type { FC } from "react";
import type { AIGatewaySpendFilter } from "#/api/typesGenerated";
import {
	DateRangePicker,
	type DateRangeValue,
} from "#/components/DateRangePicker/DateRangePicker";
import { SearchField } from "#/components/SearchField/SearchField";
import {
	ClientFilter,
	type ClientFilterMenu,
} from "#/pages/AIBridgePage/filters/ClientFilter";
import {
	ModelFilter,
	type ModelFilterMenu,
} from "#/pages/AIBridgePage/filters/ModelFilter";
import {
	ProviderFilter,
	type ProviderFilterMenu,
} from "#/pages/AIBridgePage/filters/ProviderFilter";

// Narrower than the SelectFilter default so the search field keeps most of
// the row, matching the sessions page.
const FILTER_WIDTH = 150;

// The URL keys match both the spend API query parameters and the sessions
// page filter keys, so a drill-in can hand its filters to the sessions link
// unchanged.
export type SpendDimensions = Pick<
	AIGatewaySpendFilter,
	"provider_name" | "client" | "model"
>;

export interface SpendFilterMenus {
	provider: ProviderFilterMenu;
	client: ClientFilterMenu;
	model: ModelFilterMenu;
}

interface SpendFiltersProps {
	menus: SpendFilterMenus;
	now?: Date;
	dateRange: DateRangeValue;
	onDateRangeChange: (value: DateRangeValue) => void;
	search?: { value: string; onChange: (value: string) => void };
}

export const SpendFilters: FC<SpendFiltersProps> = ({
	menus,
	now,
	dateRange,
	onDateRangeChange,
	search,
}) => {
	return (
		<div className="flex flex-wrap gap-2 lg:flex-nowrap">
			{search && (
				<SearchField
					className="w-full"
					value={search.value}
					onChange={search.onChange}
					placeholder="Search by name or username"
					aria-label="Search spend by name or username"
				/>
			)}
			<ProviderFilter menu={menus.provider} width={FILTER_WIDTH} />
			<ClientFilter menu={menus.client} width={FILTER_WIDTH} />
			<ModelFilter menu={menus.model} width={FILTER_WIDTH} />
			<DateRangePicker
				now={now}
				value={dateRange}
				onChange={onDateRangeChange}
				size="lg"
			/>
		</div>
	);
};
