import { contextBridge } from "electron";
import { desktopIpcChannels } from "../../shared/contracts/ipc.ts";
import type { DesktopCaptureApi } from "../../shared/contracts/capture.ts";
import { invokeDesktopApi } from "../api/invoke.ts";

const captureApi: DesktopCaptureApi = {
  cancel: () => invokeDesktopApi(desktopIpcChannels.capture.cancel),
  getState: () => invokeDesktopApi(desktopIpcChannels.capture.getState),
  queryMentionDirectory: (input) =>
    invokeDesktopApi(desktopIpcChannels.capture.queryMentionDirectory, input),
  queryMentions: (input) =>
    invokeDesktopApi(desktopIpcChannels.capture.queryMentions, input),
  resolveMention: (input) =>
    invokeDesktopApi(desktopIpcChannels.capture.resolveMention, input),
  select: (input) => invokeDesktopApi(desktopIpcChannels.capture.select, input),
  selectFiles: () => invokeDesktopApi(desktopIpcChannels.capture.selectFiles),
  submit: (input) => invokeDesktopApi(desktopIpcChannels.capture.submit, input)
};

contextBridge.exposeInMainWorld("tuttiCapture", captureApi);
