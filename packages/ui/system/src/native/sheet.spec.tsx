import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NativeSheet } from "./sheet";

const nativeModal = vi.hoisted(() => ({
  keyboardBehavior: null as string | null,
  keyboardVerticalOffset: null as number | null,
  onAccessibilityEscape: null as (() => void) | null,
  onRequestClose: null as (() => void) | null,
  panelStyle: null as unknown
}));

vi.mock("react-native", () => ({
  KeyboardAvoidingView: ({
    accessible,
    behavior,
    children,
    keyboardVerticalOffset,
    onAccessibilityEscape
  }: {
    accessible?: boolean;
    behavior?: string;
    children: ReactNode;
    keyboardVerticalOffset?: number;
    onAccessibilityEscape?(): void;
  }) => {
    nativeModal.keyboardBehavior = behavior ?? null;
    nativeModal.keyboardVerticalOffset = keyboardVerticalOffset ?? null;
    nativeModal.onAccessibilityEscape =
      onAccessibilityEscape ?? nativeModal.onAccessibilityEscape;
    return <div data-accessible={String(accessible)}>{children}</div>;
  },
  Modal: ({
    children,
    onRequestClose,
    visible
  }: {
    children: ReactNode;
    onRequestClose(): void;
    visible: boolean;
  }) => {
    nativeModal.onRequestClose = onRequestClose;
    return visible ? <div>{children}</div> : null;
  },
  Platform: { OS: "android" },
  Pressable: ({
    accessibilityLabel,
    accessibilityRole,
    accessible,
    children,
    onPress,
    style,
    testID
  }: {
    accessibilityLabel?: string;
    accessibilityRole?: "button";
    accessible?: boolean;
    children?: ReactNode;
    onPress(): void;
    style?: unknown;
    testID?: string;
  }) => {
    if (testID === "native-sheet-panel") {
      nativeModal.panelStyle = style;
    }
    return (
      <div
        aria-label={accessibilityLabel}
        data-accessible={String(accessible)}
        data-testid={testID}
        onClick={onPress}
        role={accessibilityRole}
      >
        {children}
      </div>
    );
  },
  StyleSheet: { create: (styles: unknown) => styles }
}));

vi.mock("./theme-provider", () => ({
  useNativeTheme: () => ({
    color: {
      muted: "#000",
      panelRaised: "#fff",
      scrim: "rgba(0, 0, 0, 0.6)"
    },
    radius: { large: 12 },
    space: { small: 10 }
  })
}));

describe("NativeSheet", () => {
  beforeEach(() => {
    nativeModal.keyboardBehavior = null;
    nativeModal.keyboardVerticalOffset = null;
    nativeModal.onAccessibilityEscape = null;
    nativeModal.onRequestClose = null;
    nativeModal.panelStyle = null;
  });

  it("renders its content only while open", () => {
    const { rerender } = render(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        onOpenChange={() => undefined}
        open={false}
      >
        content
      </NativeSheet>
    );

    expect(screen.queryByText("content")).not.toBeInTheDocument();

    rerender(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        onOpenChange={() => undefined}
        open
      >
        content
      </NativeSheet>
    );
    expect(screen.getByText("content")).toBeInTheDocument();

    rerender(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        onOpenChange={() => undefined}
        open={false}
      >
        content
      </NativeSheet>
    );
    expect(screen.queryByText("content")).not.toBeInTheDocument();
  });

  it("reports backdrop and system dismissals to the controlled owner", () => {
    const onOpenChange = vi.fn();
    const { container } = render(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        onOpenChange={onOpenChange}
        open
      >
        content
      </NativeSheet>
    );

    expect(
      container.querySelectorAll('[data-accessible="false"]')
    ).toHaveLength(2);

    fireEvent.click(screen.getByText("content"));
    expect(onOpenChange).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", {
        name: "Close sheet"
      })
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);

    onOpenChange.mockClear();
    nativeModal.onAccessibilityEscape?.();
    expect(onOpenChange).toHaveBeenCalledWith(false);

    onOpenChange.mockClear();
    nativeModal.onRequestClose?.();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("applies one explicit fixed height", () => {
    render(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        height="50%"
        onOpenChange={() => undefined}
        open
      >
        content
      </NativeSheet>
    );

    expect(nativeModal.panelStyle).toEqual(
      expect.arrayContaining([{ height: "50%" }])
    );
  });

  it("keeps the panel above the Android keyboard", () => {
    render(
      <NativeSheet
        closeAccessibilityLabel="Close sheet"
        onOpenChange={() => undefined}
        open
      >
        content
      </NativeSheet>
    );

    expect(nativeModal.keyboardBehavior).toBe("height");
    expect(nativeModal.keyboardVerticalOffset).toBe(0);
  });
});
