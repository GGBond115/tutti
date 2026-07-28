import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useAgentTranscriptMeasurements } from "./useAgentTranscriptMeasurements";

class TestResizeObserver implements ResizeObserver {
  disconnect(): void {}
  observe(): void {}
  unobserve(): void {}
}

describe("useAgentTranscriptMeasurements", () => {
  it("synchronously remeasures the mounted latest turn in layout effect", () => {
    vi.stubGlobal("ResizeObserver", TestResizeObserver);
    const element = document.createElement("div");
    element.dataset.agentTranscriptVirtualTurn = "latest";
    vi.spyOn(element, "offsetHeight", "get").mockReturnValue(460);
    const onCommit = vi.fn();
    const { result, rerender, unmount } = renderHook(() =>
      useAgentTranscriptMeasurements({}, undefined, onCommit, "latest")
    );

    act(() => result.current.measureElement("latest", element));
    rerender();

    expect(result.current.measuredHeightsByKey.latest).toBe(460);
    expect(onCommit).toHaveBeenCalledWith({ latest: 460 });
    unmount();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });
});
