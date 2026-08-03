import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NativeAvatar } from "./avatar";

vi.mock("react-native", () => ({
  ActivityIndicator: () => <span role="progressbar" />,
  Image: ({
    onError,
    onLoad,
    source
  }: {
    onError(): void;
    onLoad(): void;
    source: { uri: string };
  }) => (
    <img alt="" data-source={source.uri} onError={onError} onLoad={onLoad} />
  ),
  StyleSheet: {
    create: (styles: unknown) => styles,
    hairlineWidth: 1
  },
  Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  View: ({ children }: { children: ReactNode }) => <div>{children}</div>
}));

vi.mock("./theme-provider", () => ({
  useNativeTheme: () => ({
    color: {
      border: "#000",
      muted: "#000",
      panelRaised: "#fff",
      text: "#000"
    },
    control: { compact: 40, regular: 48, row: 60 },
    space: { medium: 16 }
  })
}));

describe("NativeAvatar", () => {
  it("derives a Unicode-safe initial and accepts an explicit initial", () => {
    const { rerender } = render(<NativeAvatar label="阿丽塔" />);
    expect(screen.getByText("阿")).toBeInTheDocument();

    rerender(<NativeAvatar initial="T" label="阿丽塔" />);
    expect(screen.getByText("T")).toBeInTheDocument();
  });

  it("shows loading feedback until an image loads", () => {
    render(<NativeAvatar label="Alice" src="avatar-a.png" />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.queryByText("A")).not.toBeInTheDocument();

    fireEvent.load(screen.getByRole("presentation"));
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("falls back after an image error", () => {
    render(<NativeAvatar label="Alice" src="avatar-a.png" />);
    fireEvent.error(screen.getByRole("presentation"));
    expect(screen.getByText("A")).toBeInTheDocument();
  });

  it("resets image state when the source changes and permits a retry", () => {
    const { rerender } = render(
      <NativeAvatar label="Alice" src="avatar-a.png" />
    );
    fireEvent.error(screen.getByRole("presentation"));

    rerender(<NativeAvatar label="Alice" src="avatar-b.png" />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    fireEvent.error(screen.getByRole("presentation"));

    rerender(<NativeAvatar label="Alice" src="avatar-a.png" />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.queryByText("A")).not.toBeInTheDocument();
  });

  it("supports an explicit loading state without exposing fallback copy", () => {
    render(<NativeAvatar label="Alice" loading src="avatar-a.png" />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.queryByText("A")).not.toBeInTheDocument();
    expect(screen.queryByRole("presentation")).not.toBeInTheDocument();
  });
});
