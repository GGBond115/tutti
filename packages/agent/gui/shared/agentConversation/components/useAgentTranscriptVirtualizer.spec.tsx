import { act, renderHook } from "@testing-library/react";
import { createRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  useAgentTranscriptVirtualizer,
  type AgentTranscriptViewportSnapshot,
  type AgentTranscriptVirtualScrollController
} from "./useAgentTranscriptVirtualizer";

class TestResizeObserver implements ResizeObserver {
  disconnect(): void {}
  observe(): void {}
  unobserve(): void {}
}

describe("useAgentTranscriptVirtualizer", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", TestResizeObserver);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(480);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("cancels a pending scrollToKey mount wait immediately", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    const cancelAnimationFrame = vi
      .spyOn(window, "cancelAnimationFrame")
      .mockImplementation(() => {});
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.spyOn(performance, "now").mockReturnValue(0);
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    timeline.append(host);
    document.body.append(timeline);
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-cancel",
        entries: [{ gapAfterPx: 0, key: "turn-1" }],
        hasMovingTurnDisclosure: false
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
    });
    const abortController = new AbortController();

    const targetPromise = result.current.rowVirtualizer.scrollToKey(
      "turn-1",
      () => null,
      { signal: abortController.signal }
    );
    expect(animationFrames).toHaveLength(1);
    abortController.abort();

    await expect(targetPromise).resolves.toBeNull();
    expect(cancelAnimationFrame).toHaveBeenCalledWith(1);
    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("stops waiting when the virtualizer host disconnects", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.spyOn(performance, "now").mockReturnValue(0);
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    timeline.append(host);
    document.body.append(timeline);
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-disconnect",
        entries: [{ gapAfterPx: 0, key: "turn-1" }],
        hasMovingTurnDisclosure: false
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
    });

    const targetPromise = result.current.rowVirtualizer.scrollToKey(
      "turn-1",
      () => null
    );
    host.remove();
    act(() => animationFrames.shift()?.(16));

    await expect(targetPromise).resolves.toBeNull();
    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("finishes scrollToKey with exact mounted-target alignment", async () => {
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    const target = document.createElement("div");
    host.append(target);
    timeline.append(host);
    document.body.append(timeline);
    timeline.getBoundingClientRect = () => rect(0, 480);
    target.getBoundingClientRect = () => rect(-120 - timeline.scrollTop, 40);
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-exact-locate",
        entries: [{ gapAfterPx: 0, key: "turn-1" }],
        hasMovingTurnDisclosure: false
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
    });

    await act(async () => {
      await result.current.rowVirtualizer.scrollToKey("turn-1", () => target, {
        align: "top",
        behavior: "auto"
      });
    });

    expect(timeline.scrollTop).toBe(-120);
    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("builds the rendered range from the scrollTop accepted by the browser", () => {
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    timeline.append(host);
    document.body.append(timeline);
    let actualScrollTop = 0;
    Object.defineProperty(timeline, "scrollTop", {
      configurable: true,
      get: () => actualScrollTop,
      set: (next: number) => {
        actualScrollTop = Math.max(-7_000, Math.min(0, next));
      }
    });
    const controller = createRef<AgentTranscriptVirtualScrollController>();
    const entries = Array.from({ length: 120 }, (_, index) => ({
      gapAfterPx: 0,
      key: `turn-${index}`
    }));
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-clamped-scroll",
        entries,
        hasMovingTurnDisclosure: false,
        virtualScrollControllerRef: controller
      })
    );
    let snapshot = null as AgentTranscriptViewportSnapshot | null;
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
      controller.current?.subscribeViewport((next) => {
        snapshot = next;
      });
      result.current.rowVirtualizer.scrollToIndex(0, {
        align: "top",
        behavior: "auto"
      });
    });

    expect(actualScrollTop).toBe(-7_000);
    expect(snapshot?.distanceFromBottomPx).toBe(7_000);
    expect(
      result.current.rowVirtualizer
        .getVirtualItems()
        .some((item) => item.key === "turn-0")
    ).toBe(false);

    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("keeps the rendered window on stable turn keys during a prepend render", () => {
    const baseEntries = Array.from({ length: 20 }, (_, index) => ({
      gapAfterPx: 0,
      key: `turn-${index}`
    }));
    const { result, rerender, unmount } = renderHook(
      ({ entries }) =>
        useAgentTranscriptVirtualizer({
          agentSessionId: "session-prepend-projection",
          entries,
          hasMovingTurnDisclosure: false
        }),
      { initialProps: { entries: baseEntries } }
    );
    const firstRenderedKey = result.current.virtualItems[0]?.key;

    rerender({
      entries: [{ gapAfterPx: 0, key: "older" }, ...baseEntries]
    });

    expect(result.current.virtualItems[0]?.key).toBe(firstRenderedKey);
    unmount();
  });

  it("publishes measured height, layout, and rendered items in one commit", async () => {
    const entries = Array.from({ length: 6 }, (_, index) => ({
      gapAfterPx: 0,
      key: `turn-${index}`
    }));
    const element = document.createElement("div");
    element.dataset.agentTranscriptVirtualTurn = "turn-5";
    vi.spyOn(element, "offsetHeight", "get").mockReturnValue(500);
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-measurement-commit",
        entries,
        hasMovingTurnDisclosure: false
      })
    );

    act(() => result.current.rowVirtualizer.measureElement("turn-5", element));
    await act(async () => Promise.resolve());

    expect(result.current.totalHeightPx).toBe(5 * 280 + 500);
    expect(
      result.current.virtualItems.find((item) => item.key === "turn-5")
    ).toMatchObject({ measured: true, size: 500 });
    unmount();
  });

  it("owns normalized user-scroll intent for the connected viewport", () => {
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    const input = document.createElement("input");
    timeline.append(host, input);
    document.body.append(timeline);
    Object.defineProperty(timeline, "scrollHeight", {
      configurable: true,
      value: 1_200
    });
    const controller = createRef<AgentTranscriptVirtualScrollController>();
    const listener = vi.fn();
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-user-intent",
        entries: [{ gapAfterPx: 0, key: "turn-1" }],
        hasMovingTurnDisclosure: false,
        virtualScrollControllerRef: controller
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
    });
    const unsubscribe = controller.current!.subscribeUserScroll(listener);

    act(() => {
      timeline.dispatchEvent(
        new WheelEvent("wheel", {
          bubbles: true,
          deltaMode: WheelEvent.DOM_DELTA_LINE,
          deltaY: -1
        })
      );
      timeline.scrollTop = -100;
      timeline.dispatchEvent(new Event("scroll"));
      input.dispatchEvent(
        new KeyboardEvent("keydown", { bubbles: true, key: "PageUp" })
      );
      timeline.dispatchEvent(
        new KeyboardEvent("keydown", { bubbles: true, key: "PageDown" })
      );
      timeline.scrollTop = -50;
      timeline.dispatchEvent(new Event("scroll"));
    });

    expect(listener.mock.calls).toEqual([["away"], ["toward-end"]]);
    unsubscribe();
    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("stops top loading after committed DOM content moves the viewport away", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    timeline.append(host);
    document.body.append(timeline);
    let scrollHeight = 520;
    Object.defineProperty(timeline, "scrollHeight", {
      configurable: true,
      get: () => scrollHeight
    });
    const controller = createRef<AgentTranscriptVirtualScrollController>();
    const loadOlder = vi.fn(async () => {
      scrollHeight = 1_200;
    });
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-top-loading",
        entries: Array.from({ length: 8 }, (_, index) => ({
          gapAfterPx: 0,
          key: `turn-${index}`
        })),
        hasMovingTurnDisclosure: false,
        virtualScrollControllerRef: controller
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
      controller.current?.setTopLoadingHandler(loadOlder);
      timeline.scrollTop = -40;
      timeline.dispatchEvent(new Event("scroll"));
      timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -20 }));
    });
    await act(async () => {
      await Promise.resolve();
      animationFrames.shift()?.(16);
      await Promise.resolve();
    });

    expect(loadOlder).toHaveBeenCalledOnce();

    act(() => result.current.setVirtualizerHostElement(null));
    unmount();
    timeline.remove();
  });

  it("cancels an active smooth scroll when the viewport disconnects", () => {
    const frames: FrameRequestCallback[] = [];
    const cancelAnimationFrame = vi
      .spyOn(window, "cancelAnimationFrame")
      .mockImplementation(() => undefined);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.spyOn(performance, "now").mockReturnValue(0);
    const timeline = document.createElement("div");
    const host = document.createElement("div");
    timeline.append(host);
    document.body.append(timeline);
    const controller = createRef<AgentTranscriptVirtualScrollController>();
    const { result, unmount } = renderHook(() =>
      useAgentTranscriptVirtualizer({
        agentSessionId: "session-scroll-cleanup",
        entries: Array.from({ length: 4 }, (_, index) => ({
          gapAfterPx: 12,
          key: `turn-${index}`
        })),
        hasMovingTurnDisclosure: false,
        virtualScrollControllerRef: controller
      })
    );
    act(() => {
      result.current.setVirtualizerHostElement(host);
      result.current.rowVirtualizer.connectScrollElement(timeline);
      timeline.scrollTop = -400;
      controller.current?.scrollToEnd({ behavior: "smooth" });
    });
    expect(frames).toHaveLength(1);

    act(() => result.current.setVirtualizerHostElement(null));

    expect(cancelAnimationFrame).toHaveBeenCalledWith(1);
    unmount();
    timeline.remove();
  });
});

function rect(top: number, height: number): DOMRect {
  return {
    bottom: top + height,
    height,
    left: 0,
    right: 100,
    top,
    width: 100,
    x: 0,
    y: top,
    toJSON: () => ({})
  } as DOMRect;
}
