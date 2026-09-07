import type { FC } from "react";
import type { AIGatewaySpendFilter } from "#/api/typesGenerated";
import {
	DateRangePicker,
	type DateRangeValue,
} from "#/components/DateRangePicker/DateRangePicker";
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
}

export const SpendFilters: FC<SpendFiltersProps> = ({
	menus,
	now,
	dateRange,
	onDateRangeChange,
}) => {
	return (
		<div className="flex flex-wrap items-center gap-2">
			<ProviderFilter menu={menus.provider} />
			<ClientFilter menu={menus.client} />
			<ModelFilter menu={menus.model} />
			<DateRangePicker
				now={now}
				value={dateRange}
				onChange={onDateRangeChange}
				size="lg"
			/>
		</div>
	);
};
