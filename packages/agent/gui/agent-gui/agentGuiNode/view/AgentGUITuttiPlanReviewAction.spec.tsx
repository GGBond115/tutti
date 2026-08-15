import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentGUITuttiPlanReviewAction } from "./AgentGUITuttiPlanReviewAction";

describe("AgentGUITuttiPlanReviewAction", () => {
  it("renders the localized request-changes action and dispatches it explicitly", () => {
    const onRequestChanges = vi.fn();
    render(
      <AgentGUITuttiPlanReviewAction
        label="请求修改"
        onRequestChanges={onRequestChanges}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: "请求修改" }));

    expect(onRequestChanges).toHaveBeenCalledTimes(1);
  });
});
