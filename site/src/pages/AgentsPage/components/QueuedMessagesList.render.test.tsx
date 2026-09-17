import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ChatQueuedMessage } from "#/api/typesGenerated";
import { MockChatQueuedMessage } from "#/testHelpers/chatEntities";
import { render } from "#/testHelpers/renderHelpers";
import { QueuedMessagesList } from "./QueuedMessagesList";

const buildMessage = (
	overrides: Partial<ChatQueuedMessage> = {},
): ChatQueuedMessage => ({ ...MockChatQueuedMessage, ...overrides });

describe("QueuedMessagesList", () => {
	it("labels a queued message explicitly as Queued, with an explanation of when it will send", () => {
		render(
			<QueuedMessagesList
				messages={[buildMessage({ id: 1 })]}
				onDelete={vi.fn()}
				onPromote={vi.fn()}
			/>,
		);

		// Visible badge text -- the whole point of this fix (BUG_C follow-up
		// UX package item 3): a queued message must never look like an
		// ordinary sent message.
		expect(screen.getByText("Queued")).toBeInTheDocument();
		// The full explanation is always present for assistive tech via the
		// sr-only span, regardless of tooltip hover state.
		expect(
			screen.getAllByText(/will be sent when the current turn finishes/i)
				.length,
		).toBeGreaterThan(0);
	});

	it("renders no queued-message item when the queue is empty", () => {
		render(
			<QueuedMessagesList messages={[]} onDelete={vi.fn()} onPromote={vi.fn()} />,
		);
		expect(screen.queryByText("Queued")).not.toBeInTheDocument();
	});
});
