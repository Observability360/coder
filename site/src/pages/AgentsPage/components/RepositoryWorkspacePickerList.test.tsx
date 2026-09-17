import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AxiosError, type AxiosResponse } from "axios";
import { toast } from "sonner";
import { API } from "#/api/api";
import { render } from "#/testHelpers/renderHelpers";
import { RepositoryWorkspacePickerList } from "./AgentChatInput";

function axios404(): AxiosError {
	const err = new AxiosError("Not Found");
	err.response = { status: 404 } as AxiosResponse;
	return err;
}

afterEach(() => {
	vi.restoreAllMocks();
});

describe("RepositoryWorkspacePickerList", () => {
	it("lists accessible repositories with their status, filtered by search", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockResolvedValue({
			repositories: [
				{
					id: 1,
					owner: "Observability360",
					name: "OmniRoute",
					full_name: "Observability360/OmniRoute",
					private: true,
					default_branch: "main",
					clone_url: "git@github.com:Observability360/OmniRoute.git",
					html_url: "https://github.com/Observability360/OmniRoute",
					workspace_id: "ws-omniroute",
				},
				{
					id: 2,
					owner: "Observability360",
					name: "O360-AI-Memory",
					full_name: "Observability360/O360-AI-Memory",
					private: true,
					default_branch: "main",
					clone_url: "git@github.com:Observability360/O360-AI-Memory.git",
					html_url: "https://github.com/Observability360/O360-AI-Memory",
					// No workspace yet.
				},
			],
		});

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[]}
				selectedWorkspaceId="ws-omniroute"
				onSelect={vi.fn()}
			/>,
		);

		await waitFor(() => {
			expect(screen.getByText("OmniRoute")).toBeInTheDocument();
		});
		expect(screen.getByText("Attached")).toBeInTheDocument();
		expect(screen.getByText("Not created")).toBeInTheDocument();

		const search = screen.getByPlaceholderText("Search repositories...");
		await userEvent.type(search, "AI-Memory");

		expect(screen.getByText("O360-AI-Memory")).toBeInTheDocument();
		expect(screen.queryByText("OmniRoute")).not.toBeInTheDocument();
	});

	it("selecting a repository with an existing workspace attaches it directly, without creating a new one", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockResolvedValue({
			repositories: [
				{
					id: 1,
					owner: "Observability360",
					name: "OmniRoute",
					full_name: "Observability360/OmniRoute",
					private: true,
					default_branch: "main",
					clone_url: "git@github.com:Observability360/OmniRoute.git",
					html_url: "https://github.com/Observability360/OmniRoute",
					workspace_id: "ws-omniroute",
				},
			],
		});
		const attachSpy = vi.spyOn(API, "attachRepositoryWorkspace");
		const onSelect = vi.fn();

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[]}
				selectedWorkspaceId={null}
				onSelect={onSelect}
			/>,
		);

		await waitFor(() => screen.getByText("OmniRoute"));
		await userEvent.click(screen.getByText("OmniRoute"));

		expect(onSelect).toHaveBeenCalledWith("ws-omniroute");
		expect(attachSpy).not.toHaveBeenCalled();
	});

	it("selecting a repository with no workspace lazily creates one, then attaches the result", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockResolvedValue({
			repositories: [
				{
					id: 2,
					owner: "Observability360",
					name: "O360-AI-Memory",
					full_name: "Observability360/O360-AI-Memory",
					private: true,
					default_branch: "main",
					clone_url: "git@github.com:Observability360/O360-AI-Memory.git",
					html_url: "https://github.com/Observability360/O360-AI-Memory",
				},
			],
		});
		const attachSpy = vi
			.spyOn(API, "attachRepositoryWorkspace")
			.mockResolvedValue({
				// biome-ignore lint/suspicious/noExplicitAny: minimal fixture, only .id is read
				workspace: { id: "ws-new-aimemory" } as any,
				created: true,
			});
		const onSelect = vi.fn();

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[]}
				selectedWorkspaceId={null}
				onSelect={onSelect}
			/>,
		);

		await waitFor(() => screen.getByText("O360-AI-Memory"));
		await userEvent.click(screen.getByText("O360-AI-Memory"));

		expect(attachSpy).toHaveBeenCalledWith(
			"Observability360",
			"O360-AI-Memory",
		);
		await waitFor(() => {
			expect(onSelect).toHaveBeenCalledWith("ws-new-aimemory");
		});
	});

	it("surfaces a clear error and does not attach anything when workspace creation fails", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockResolvedValue({
			repositories: [
				{
					id: 2,
					owner: "Observability360",
					name: "O360-AI-Memory",
					full_name: "Observability360/O360-AI-Memory",
					private: true,
					default_branch: "main",
					clone_url: "git@github.com:Observability360/O360-AI-Memory.git",
					html_url: "https://github.com/Observability360/O360-AI-Memory",
				},
			],
		});
		vi.spyOn(API, "attachRepositoryWorkspace").mockRejectedValue(
			new Error("upstream 500"),
		);
		const toastErrorSpy = vi.spyOn(toast, "error");
		const onSelect = vi.fn();

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[]}
				selectedWorkspaceId={null}
				onSelect={onSelect}
			/>,
		);

		await waitFor(() => screen.getByText("O360-AI-Memory"));
		await userEvent.click(screen.getByText("O360-AI-Memory"));

		await waitFor(() => {
			expect(toastErrorSpy).toHaveBeenCalled();
		});
		expect(onSelect).not.toHaveBeenCalled();
	});

	it("falls back to the plain workspace picker when the repository catalog is not configured (404)", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockRejectedValue(axios404());

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[
					{
						id: "ws-1",
						name: "aimemory-repo-luis",
						organization_id: "org-1",
					},
				]}
				selectedWorkspaceId={null}
				onSelect={vi.fn()}
			/>,
		);

		await waitFor(() => {
			expect(
				screen.getByPlaceholderText("Search workspaces..."),
			).toBeInTheDocument();
		});
		expect(
			screen.queryByPlaceholderText("Search repositories..."),
		).not.toBeInTheDocument();
		expect(screen.getByText("aimemory-repo-luis")).toBeInTheDocument();
	});

	it("switches to the existing-workspaces tab and selects a workspace by name directly, without touching the repository catalog's attach flow", async () => {
		vi.spyOn(API, "getRepositoryCatalog").mockResolvedValue({
			repositories: [],
		});
		const attachSpy = vi.spyOn(API, "attachRepositoryWorkspace");
		const onSelect = vi.fn();

		render(
			<RepositoryWorkspacePickerList
				workspaceOptions={[
					{
						id: "ws-multi",
						name: "o360-multi-repo",
						organization_id: "org-1",
					},
				]}
				selectedWorkspaceId={null}
				onSelect={onSelect}
			/>,
		);

		// Repositories is still the default tab.
		await waitFor(() => screen.getByText("Repositories"));
		expect(
			screen.getByPlaceholderText("Search repositories..."),
		).toBeInTheDocument();

		await userEvent.click(screen.getByText("Existing workspaces"));

		expect(
			screen.getByPlaceholderText("Search workspaces..."),
		).toBeInTheDocument();
		expect(
			screen.queryByPlaceholderText("Search repositories..."),
		).not.toBeInTheDocument();

		await userEvent.click(screen.getByText("o360-multi-repo"));

		expect(onSelect).toHaveBeenCalledWith("ws-multi");
		expect(attachSpy).not.toHaveBeenCalled();
	});
});
