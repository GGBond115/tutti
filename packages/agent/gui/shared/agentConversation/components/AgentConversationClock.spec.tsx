import { act, render, screen } from "@testing-library/react";
import type { JSX } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AgentConversationClockProvider } from "./AgentConversationClock";
import { useElapsedSeconds } from "./useElapsedSeconds";

describe("AgentConversationClock", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shares one interval and pauses it while the AgentGUI is hidden", () => {
    const setInterval = vi.spyOn(window, "setInterval");
    const clearInterval = vi.spyOn(window, "clearInterval");
    const { rerender } = render(<ElapsedPair isVisible />);

    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("0");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("0");
    expect(setInterval).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(2_000);
    });
    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("2");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("2");

    rerender(<ElapsedPair isVisible={false} />);
    expect(clearInterval).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(vi.getTimerCount()).toBe(0);

    rerender(<ElapsedPair isVisible />);
    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("7");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("7");
    expect(setInterval).toHaveBeenCalledTimes(2);
  });
});

function ElapsedPair({ isVisible }: { isVisible: boolean }): JSX.Element {
  return (
    <AgentConversationClockProvider isVisible={isVisible}>
      <ElapsedValue testId="elapsed-a" />
      <ElapsedValue testId="elapsed-b" />
    </AgentConversationClockProvider>
  );
}

function ElapsedValue({ testId }: { testId: string }): JSX.Element {
  const elapsedSeconds = useElapsedSeconds(1_000);
  return <span data-testid={testId}>{elapsedSeconds}</span>;
}
