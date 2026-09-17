import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { API } from "#/api/api";
import { render } from "#/testHelpers/renderHelpers";
import { AgentChatInput } from "./AgentChatInput";

const defaultModelOptions = [
	{
		id: "model-config-1",
		provider: "openai",
		model: "gpt-4o",
		displayName: "GPT-4o",
	},
] as const;

afterEach(() => {
	vi.restoreAllMocks();
});

describe("AgentChatInput BTW (ask without interrupting)", () => {
	it("answers via sideQuery without ever calling onSend, and shows the answer in its own panel", async () => {
		const onSend = vi.fn();
		const sideQuery = vi
			.spyOn(API.experimental, "sideQuery")
			.mockResolvedValue({
				answer: "Running for 2m, using execute.",
				source: "snapshot",
				snapshot: {
					status: "running",
					is_running: true,
					subagents_running: 0,
					subagents_completed: 0,
					queued_count: 0,
					is_retrying: false,
					generation_attempt: 0,
				},
			});

		render(
			<AgentChatInput
				onSend={onSend}
				isDisabled={false}
				isLoading={false}
				selectedModel={defaultModelOptions[0].id}
				modelOptions={[...defaultModelOptions]}
				modelSelectorPlaceholder="Select model"
				hasModelOptions
				onModelChange={vi.fn()}
				canConfigureAgentSetup={false}
				chatId="chat-1"
				isStreaming
			/>,
		);

		await userEvent.click(
			screen.getByRole("button", { name: "Ask without interrupting" }),
		);

		expect(sideQuery).toHaveBeenCalledWith("chat-1", { question: "" });
		expect(
			await screen.findByText("Running for 2m, using execute."),
		).toBeInTheDocument();
		expect(onSend).not.toHaveBeenCalled();
	});

	it("does not render the BTW affordance or panel when the chat isn't streaming", () => {
		render(
			<AgentChatInput
				onSend={vi.fn()}
				isDisabled={false}
				isLoading={false}
				selectedModel={defaultModelOptions[0].id}
				modelOptions={[...defaultModelOptions]}
				modelSelectorPlaceholder="Select model"
				hasModelOptions
				onModelChange={vi.fn()}
				canConfigureAgentSetup={false}
				chatId="chat-1"
				isStreaming={false}
			/>,
		);

		expect(
			screen.queryByRole("button", { name: "Ask without interrupting" }),
		).not.toBeInTheDocument();
		expect(screen.queryByTestId("btw-panel")).not.toBeInTheDocument();
	});
});
