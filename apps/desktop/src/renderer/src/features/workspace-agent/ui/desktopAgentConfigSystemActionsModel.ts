import type { DesktopWorkspaceUiMode } from "@shared/preferences";

export function shouldShowDesktopAgentConfigSystemActions(
  workspaceUiMode: DesktopWorkspaceUiMode
): boolean {
  return workspaceUiMode === "agent";
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
