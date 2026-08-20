import type { DesktopWorkspaceUiMode } from "@shared/preferences";

export function shouldShowDesktopAgentConfigSystemActions(
  workspaceUiMode: DesktopWorkspaceUiMode
): boolean {
  return workspaceUiMode === "agent";
}

export function shouldKeepOpenSubmenuOnTriggerPointerDown(input: {
  button: number;
  ctrlKey: boolean;
  open: boolean;
}): boolean {
  return input.open && input.button === 0 && !input.ctrlKey;
}

export function shouldKeepOpenSubmenuOnTriggerKeyDown(input: {
  key: string;
  open: boolean;
}): boolean {
  return input.open && (input.key === "Enter" || input.key === " ");
}

export interface SubmenuGraceCloseController {
  cancel(): void;
  schedule(): void;
}

export function createSubmenuGraceCloseController(input: {
  close: () => void;
  createTimeoutSignal?: () => AbortSignal;
}): SubmenuGraceCloseController {
  let request = 0;
  const createTimeoutSignal =
    input.createTimeoutSignal ?? (() => AbortSignal.timeout(120));

  return {
    cancel() {
      request += 1;
    },
    schedule() {
      const scheduledRequest = ++request;
      createTimeoutSignal().addEventListener(
        "abort",
        () => {
          if (scheduledRequest === request) {
            input.close();
          }
        },
        { once: true }
      );
    }
  };
}
