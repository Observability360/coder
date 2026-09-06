import type { FC } from "react";
import type * as TypesGen from "#/api/typesGenerated";
import {
	DateRangePicker,
	type DateRangeValue,
} from "#/components/DateRangePicker/DateRangePicker";
import type { PaginationResult } from "#/components/PaginationWidget/PaginationContainer";
import {
	SettingsHeader,
	SettingsHeaderDescription,
	SettingsHeaderTitle,
} from "#/components/SettingsHeader/SettingsHeader";
import { PremiumPaywallAIGovernance } from "#/modules/paywall/PremiumPaywallAIGovernance";
import { AIBridgeSetupAlert } from "#/pages/AIBridgePage/AIBridgeSetupAlert";
import { RetentionNotice } from "./components/RetentionNotice";
import { SpendDrillInView } from "./components/SpendDrillInView";
import { SpendSectionHeader } from "./components/SpendSectionHeader";
import { SpendSummaryView } from "./components/SpendSummaryView";
import { SpendUsersTable } from "./components/SpendUsersTable";
import { formatUsageDateRange, toInclusiveDateRange } from "./utils/dateRange";

export type SpendUsersQuery = PaginationResult & {
	data: TypesGen.AIGatewaySpendUsersResponse | undefined;
	isLoading: boolean;
	isFetching: boolean;
	error: unknown;
	refetch: () => unknown;
};

interface SpendPageViewProps {
	isEntitled: boolean;
	isEnabled: boolean;
	now?: Date;
	dateRange: DateRangeValue;
	onDateRangeChange: (value: DateRangeValue) => void;
	searchFilter: string;
	onSearchFilterChange: (value: string) => void;
	usersQuery: SpendUsersQuery;
	drillInUserId: string | null;
	drillInUser: TypesGen.User | null;
	isDrillInUserLoading: boolean;
	drillInUserError: unknown;
	onDrillInUserRetry: () => void;
	onClearSelectedUser: () => void;
	summaryData: TypesGen.AIGatewaySpendUserSummary | undefined;
	isSummaryLoading: boolean;
	summaryError: unknown;
	onSummaryRetry: () => void;
}

export const SpendPageView: FC<SpendPageViewProps> = ({
	isEntitled,
	isEnabled,
	now,
	dateRange,
	onDateRangeChange,
	searchFilter,
	onSearchFilterChange,
	usersQuery,
	drillInUserId,
	drillInUser,
	isDrillInUserLoading,
	drillInUserError,
	onDrillInUserRetry,
	onClearSelectedUser,
	summaryData,
	isSummaryLoading,
	summaryError,
	onSummaryRetry,
}) => {
	if (!isEntitled) {
		return (
			<PremiumPaywallAIGovernance variant="governance" source="ai_spend" />
		);
	}

	if (!isEnabled) {
		return <AIBridgeSetupAlert />;
	}

	// Both the default range and URL ranges carry the exclusive API end
	// boundary that DateRangePicker emits.
	const displayDateRange = toInclusiveDateRange(dateRange, true);
	const dateRangeLabel = formatUsageDateRange(dateRange, {
		endDateIsExclusive: true,
	});

	if (drillInUserId) {
		return (
			<SpendDrillInView
				selectedUser={drillInUser}
				now={now}
				isLoading={isDrillInUserLoading}
				error={drillInUserError}
				onRetry={onDrillInUserRetry}
				onBack={onClearSelectedUser}
				displayDateRange={displayDateRange}
				queryDateRange={dateRange}
				onDateRangeChange={onDateRangeChange}
				dateRangeLabel={dateRangeLabel}
				summaryData={summaryData}
				isSummaryLoading={isSummaryLoading}
				summaryError={summaryError}
				onSummaryRetry={onSummaryRetry}
			/>
		);
	}

	return (
		<div className="flex max-w-[1100px] flex-col gap-8">
			<SettingsHeader>
				<SettingsHeaderTitle>AI spend</SettingsHeaderTitle>
				<SettingsHeaderDescription>
					Monitor AI Gateway spend across your deployment.
				</SettingsHeaderDescription>
			</SettingsHeader>

			<div className="flex flex-wrap items-center justify-between gap-4">
				<p className="m-0 text-sm text-content-secondary">
					Reporting period: {dateRangeLabel}
				</p>
				<DateRangePicker
					now={now}
					value={displayDateRange}
					onChange={onDateRangeChange}
				/>
			</div>
			<SpendUsersTable
				displayDateRange={displayDateRange}
				searchFilter={searchFilter}
				onSearchFilterChange={onSearchFilterChange}
				usersQuery={usersQuery}
			/>
			<section className="space-y-6">
				<SpendSectionHeader
					title="Deployment spend"
					description="Totals and breakdowns across all users in the selected period, independent of the user search above."
				/>
				{summaryData && (
					<RetentionNotice
						requestedStart={dateRange.startDate}
						applied={summaryData}
					/>
				)}
				<SpendSummaryView
					summary={summaryData}
					isLoading={isSummaryLoading}
					error={summaryError}
					onRetry={onSummaryRetry}
				/>
			</section>
		</div>
	);
};
