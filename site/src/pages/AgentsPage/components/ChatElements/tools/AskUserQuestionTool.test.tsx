import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClientProvider } from "react-query";
import { describe, expect, it, vi } from "vitest";
import { createTestQueryClient } from "#/testHelpers/renderHelpers";
import { AskUserQuestionTool } from "./AskUserQuestionTool";

const renderTool = (children: ReactNode) =>
	render(
		<QueryClientProvider client={createTestQueryClient()}>
			{children}
		</QueryClientProvider>,
	);

describe(AskUserQuestionTool.name, () => {
	it("renders a live status region instead of a form while running with no questions", () => {
		renderTool(
			<AskUserQuestionTool
				questions={[]}
				status="running"
				isError={false}
				onSubmitAnswer={vi.fn()}
			/>,
		);

		expect(screen.getByRole("status")).toHaveAttribute("aria-live", "polite");
		expect(screen.getByText("Asking for clarification...")).toBeInTheDocument();
		expect(screen.queryByRole("radio")).not.toBeInTheDocument();
	});

	it("renders no live region once completed with no questions", () => {
		renderTool(
			<AskUserQuestionTool questions={[]} status="completed" isError={false} />,
		);

		expect(screen.getByText("No questions available.")).toBeInTheDocument();
		expect(screen.queryByRole("status")).not.toBeInTheDocument();
	});
});
