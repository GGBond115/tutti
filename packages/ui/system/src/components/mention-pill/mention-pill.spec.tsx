import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MentionPill } from "./mention-pill";

describe("MentionPill", () => {
  it("reveals the semantic fallback when a custom icon fails", () => {
    const { container } = render(
      <MentionPill
        iconUrl="https://example.test/missing.png"
        kind="app"
        label="Weather"
      />
    );
    const image = container.querySelector("img");

    expect(image).toBeInTheDocument();
    expect(
      container.querySelector('[data-mention-pill-fallback-icon="true"]')
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll("img, svg")).toHaveLength(1);

    fireEvent.error(image!);

    expect(container.querySelector("img")).not.toBeInTheDocument();
    expect(
      container.querySelector('[data-mention-pill-fallback-icon="true"]')
    ).toBeInTheDocument();
    expect(container.querySelectorAll("img, svg")).toHaveLength(1);
  });

  it("retries image loading when the resolved URL changes", () => {
    const { container, rerender } = render(
      <MentionPill
        iconUrl="https://example.test/old.png"
        kind="app"
        label="Weather"
      />
    );

    fireEvent.error(container.querySelector("img")!);
    expect(container.querySelector("img")).not.toBeInTheDocument();

    rerender(
      <MentionPill
        iconUrl="https://example.test/new.png"
        kind="app"
        label="Weather"
      />
    );
    expect(container.querySelector("img")).toHaveAttribute(
      "src",
      "https://example.test/new.png"
    );
    expect(
      container.querySelector('[data-mention-pill-fallback-icon="true"]')
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll("img, svg")).toHaveLength(1);
  });

  it("renders only the custom icon for a transparent image", () => {
    const transparentPixel =
      "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==";
    const { container } = render(
      <MentionPill iconUrl={transparentPixel} kind="app" label="Weather" />
    );

    expect(container.querySelector("img")).toHaveAttribute(
      "src",
      transparentPixel
    );
    expect(
      container.querySelector('[data-mention-pill-fallback-icon="true"]')
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll("img, svg")).toHaveLength(1);
  });

  it("uses a semantic icon when no image URL is available", () => {
    const { container } = render(<MentionPill kind="issue" label="Issue" />);

    expect(container.querySelector("img")).not.toBeInTheDocument();
    expect(
      container.querySelector('[data-mention-pill-fallback-icon="true"]')
    ).toBeInTheDocument();
    expect(container.querySelectorAll("img, svg")).toHaveLength(1);
  });
});
