import { DesktopCaptureWindowController } from "./desktopCaptureWindowController.ts";
import {
  createDesktopCaptureAgentTargetPreference,
  resolveDesktopCapturePreferenceStorage
} from "./desktopCaptureAgentTargetPreference.ts";

export interface DesktopCaptureWindowContainer {
  controller: DesktopCaptureWindowController;
}

export function createDesktopCaptureWindowContainer(): DesktopCaptureWindowContainer {
  if (!window.tuttiCapture) {
    throw new Error("capture preload bridge is unavailable");
  }
  return {
    controller: new DesktopCaptureWindowController(
      window.tuttiCapture,
      createDesktopCaptureAgentTargetPreference(
        resolveDesktopCapturePreferenceStorage()
      )
    )
  };
}
