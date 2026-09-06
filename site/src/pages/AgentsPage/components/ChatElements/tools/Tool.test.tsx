import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClientProvider } from "react-query";
import { MemoryRouter } from "react-router";
import { createTestQueryClient } from "#/testHelpers/renderHelpers";
import { Tool } from "./Tool";

const renderTool = (children: ReactNode) => {
	return render(
		<QueryClientProvider client={createTestQueryClient()}>
			<MemoryRouter>{children}</MemoryRouter>
		</QueryClientProvider>,
	);
};

describe(Tool.name, () => {
	it("resolves the wait-agent title from the subagentTitles map", () => {
		renderTool(
			<Tool
				name="wait_agent"
				status="error"
				isError
				args={{ chat_id: "timed-out-child" }}
				result="timed out waiting for delegated subagent completion"
				subagentTitles={new Map([["timed-out-child", "Refactor auth module"]])}
			/>,
		);

		expect(screen.getByText("Refactor auth module")).toBeInTheDocument();
	});

	it("falls back to the descriptor title when the map has no entry", () => {
		renderTool(
			<Tool
				name="wait_agent"
				status="error"
				isError
				args={{ chat_id: "timed-out-child" }}
				result="timed out waiting for delegated subagent completion"
				subagentTitles={new Map()}
			/>,
		);

		expect(screen.queryByText("Refactor auth module")).not.toBeInTheDocument();
	});

	it("renders the image from an array computer result", () => {
		renderTool(
			<Tool
				name="computer"
				status="completed"
				result={[
					{
						type: "image",
						data: "aGVsbG8=",
						mime_type: "image/jpeg",
					},
				]}
			/>,
		);

		expect(
			screen.getByRole("img", { name: "Screenshot from computer tool" }),
		).toHaveAttribute("src", "data:image/jpeg;base64,aGVsbG8=");
	});
});
