import type { WebContents } from "electron";
import {
  desktopFeatureAvailabilityIpcChannels,
  type DesktopFeatureAvailabilityRuntime,
  type DesktopFeatureAvailabilitySnapshot
} from "../contracts/index.ts";
import { isValidDesktopFeatureKey } from "../feature-availability/core.ts";

export interface DesktopFeatureAvailabilityIpcRuntime {
  ipcMain: typeof import("electron").ipcMain;
  broadcast(
    channel: string,
    snapshot: DesktopFeatureAvailabilitySnapshot
  ): void;
  isTrustedSender(sender: WebContents): boolean;
}

export function registerDesktopFeatureAvailabilityIpc(input: {
  electron: DesktopFeatureAvailabilityIpcRuntime;
  runtime: DesktopFeatureAvailabilityRuntime;
}): { dispose(): void } {
  const assertTrustedSender = (sender: WebContents): void => {
    if (!input.electron.isTrustedSender(sender)) {
      throw new Error("desktop feature availability IPC sender is not trusted");
    }
  };
  input.electron.ipcMain.handle(
    desktopFeatureAvailabilityIpcChannels.getSnapshot,
    (event) => {
      assertTrustedSender(event.sender);
      return input.runtime.getSnapshot();
    }
  );
  input.electron.ipcMain.handle(
    desktopFeatureAvailabilityIpcChannels.isSupported,
    (event, key: unknown) => {
      assertTrustedSender(event.sender);
      if (typeof key !== "string" || !isValidDesktopFeatureKey(key)) {
        throw new Error("desktop feature key is invalid");
      }
      return input.runtime.isSupported(key);
    }
  );
  const unsubscribe = input.runtime.subscribe((snapshot) => {
    input.electron.broadcast(
      desktopFeatureAvailabilityIpcChannels.changed,
      snapshot
    );
  });
  return {
    dispose() {
      unsubscribe();
      input.electron.ipcMain.removeHandler(
        desktopFeatureAvailabilityIpcChannels.getSnapshot
      );
      input.electron.ipcMain.removeHandler(
        desktopFeatureAvailabilityIpcChannels.isSupported
      );
    }
  };
}
