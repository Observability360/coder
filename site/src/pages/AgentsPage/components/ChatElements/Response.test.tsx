import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { ThemeOverride } from "#/contexts/ThemeProvider";
import themes from "#/theme";
import { Response } from "./Response";

const renderResponse = (children: ReactNode) =>
	render(<ThemeOverride theme={themes.dark}>{children}</ThemeOverride>);

const externalImageURL = "https://external-image-host.invalid/image.png";

describe("Response external image consent", () => {
	it("gates external images behind a load button in streaming mode", () => {
		const { container } = renderResponse(
			<Response streaming>{`![diagram](${externalImageURL})`}</Response>,
		);

		expect(
			screen.getByRole("button", { name: /load external image/i }),
		).toBeInTheDocument();
		// The gate must keep the <img> out of the document in streaming
		// mode too: loading would disclose the viewer's IP to the
		// external host (Cure53 CDM-02-006).
		expect(container.querySelector("img")).toBeNull();
	});

	it("gates external images behind a load button in static mode", () => {
		const { container } = renderResponse(
			<Response>{`![diagram](${externalImageURL})`}</Response>,
		);

		expect(
			screen.getByRole("button", { name: /load external image/i }),
		).toBeInTheDocument();
		expect(container.querySelector("img")).toBeNull();
	});
});
