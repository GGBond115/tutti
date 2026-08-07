import type { DesktopCaptureSelectionInput } from "../../shared/contracts/capture.ts";

export interface CaptureBounds {
  height: number;
  width: number;
  x: number;
  y: number;
}

export function normalizeCaptureSelection(
  input: DesktopCaptureSelectionInput,
  display: Pick<CaptureBounds, "height" | "width">
): DesktopCaptureSelectionInput {
  if (
    ![input.x, input.y, input.width, input.height].every(Number.isFinite) ||
    input.width <= 0 ||
    input.height <= 0
  ) {
    throw new Error("Screenshot selection is invalid");
  }
  const x = Math.max(0, Math.min(display.width, Math.round(input.x)));
  const y = Math.max(0, Math.min(display.height, Math.round(input.y)));
  const width = Math.min(Math.round(input.width), display.width - x);
  const height = Math.min(Math.round(input.height), display.height - y);
  if (width < 8 || height < 8) {
    throw new Error("Screenshot selection is too small");
  }
  return { x, y, width, height };
}

export function resolveCaptureComposerBounds(input: {
  composerHeight: number;
  composerWidth: number;
  displayBounds: CaptureBounds;
  selection: DesktopCaptureSelectionInput;
  workArea: CaptureBounds;
}): CaptureBounds {
  const preferredX =
    input.displayBounds.x +
    input.selection.x +
    input.selection.width -
    input.composerWidth;
  const preferredY =
    input.displayBounds.y + input.selection.y + input.selection.height + 12;
  const x = Math.max(
    input.workArea.x,
    Math.min(
      preferredX,
      input.workArea.x + input.workArea.width - input.composerWidth
    )
  );
  const belowFits =
    preferredY + input.composerHeight <=
    input.workArea.y + input.workArea.height;
  const y = belowFits
    ? preferredY
    : Math.max(
        input.workArea.y,
        input.displayBounds.y + input.selection.y - input.composerHeight - 12
      );
  return {
    height: input.composerHeight,
    width: input.composerWidth,
    x: Math.round(x),
    y: Math.round(y)
  };
}

export function resolveCaptureTitle(note: string, displayName: string): string {
  const firstLine = note.split(/\r?\n/u, 1)[0]?.trim() ?? "";
  if (firstLine) {
    return firstLine.slice(0, 120);
  }
  const extensionIndex = displayName.lastIndexOf(".");
  return extensionIndex > 0
    ? displayName.slice(0, extensionIndex)
    : displayName;
}
