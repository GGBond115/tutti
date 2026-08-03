import { desktopIpcChannels } from "../../shared/contracts/ipc";
import type { DesktopRuntimeApi } from "../types";
import { invokeDesktopApi } from "./invoke";

export function createRuntimeDesktopApi(): DesktopRuntimeApi {
  return {
    getAgentSessionReplayPlayback(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.getAgentSessionReplayPlayback,
        input
      );
    },
    getAgentSessionReplayStatus(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.getAgentSessionReplayStatus,
        input
      );
    },
    getBackendConfig() {
      return invokeDesktopApi(desktopIpcChannels.runtime.getBackendConfig);
    },
    getBusinessEventStreamUrl() {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.getBusinessEventStreamUrl
      );
    },
    importAgentSessionReplayCassettes(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.importAgentSessionReplayCassettes,
        input
      );
    },
    isAgentSessionReplayRuntime() {
      // The replay runner launches the isolated Desktop with this variable;
      // renderer processes inherit it, so preload can read it synchronously.
      return process.env.TUTTI_AGENT_CASSETTE_MODE === "replay";
    },
    listWorkspaceAgentProbes(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.listWorkspaceAgentProbes,
        input
      );
    },
    launchAgentSessionReplay(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.launchAgentSessionReplay,
        input
      );
    },
    revealAgentSessionReplayCassette(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.revealAgentSessionReplayCassette,
        input
      );
    },
    setAgentSessionReplayPlayback(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.setAgentSessionReplayPlayback,
        input
      );
    },
    sendAgentSessionReplayControl(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.sendAgentSessionReplayControl,
        input
      );
    },
    waitForAgentSessionReplay(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.waitForAgentSessionReplay,
        input
      );
    },
    logRendererDiagnostic(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.logRendererDiagnostic,
        input
      );
    },
    logTerminalDiagnostic(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.logTerminalDiagnostic,
        input
      );
    },
    getTerminalStreamUrl(input) {
      return invokeDesktopApi(
        desktopIpcChannels.runtime.getTerminalStreamUrl,
        input
      );
    }
  };
}
