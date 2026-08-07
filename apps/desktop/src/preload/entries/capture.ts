import { contextBridge } from "electron";
import { desktopIpcChannels } from "../../shared/contracts/ipc.ts";
import type { DesktopCaptureApi } from "../../shared/contracts/capture.ts";
import { invokeDesktopApi } from "../api/invoke.ts";

const captureApi: DesktopCaptureApi = {
  cancel: () => invokeDesktopApi(desktopIpcChannels.capture.cancel),
  getState: () => invokeDesktopApi(desktopIpcChannels.capture.getState),
  select: (input) => invokeDesktopApi(desktopIpcChannels.capture.select, input),
  submit: (input) => invokeDesktopApi(desktopIpcChannels.capture.submit, input)
};

contextBridge.exposeInMainWorld("tuttiCapture", captureApi);
