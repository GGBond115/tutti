import { useService } from "@tutti-os/infra/di";
import { useSnapshot } from "valtio";
import { IWorkspaceSettingsService } from "../services/workspaceSettingsService.interface";

export function useWorkspaceSettingsService() {
  const service = useService(IWorkspaceSettingsService);
  // sync: valtio's default microtask-batched notify re-renders controlled
  // inputs outside the keystroke event, which loses the caret position
  // (typing mid-string jumps to the end and the value visibly flickers).
  const state = useSnapshot(service.store, { sync: true });

  return {
    service,
    state
  };
}
