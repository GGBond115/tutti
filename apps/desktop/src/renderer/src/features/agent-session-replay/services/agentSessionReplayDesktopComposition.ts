import type {
  AgentSessionEngine,
  EngineExternalCommand,
  EngineIntent
} from "@tutti-os/agent-activity-core";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopRuntimeApi } from "@preload/types";
import { AgentSessionReplayService } from "./agentSessionReplayService.ts";

export interface AgentSessionReplayActivityPort {
  addSessionEngineActivityObserver(
    workspaceId: string,
    observer: {
      observeCommand(command: EngineExternalCommand): void;
      observeIntent(intent: EngineIntent): void;
    }
  ): () => void;
  armNextSessionRecording(workspaceId: string, recordingId: string): void;
  clearNextSessionRecording(workspaceId: string, recordingId?: string): void;
  discardSessionActivityEventRecording(
    workspaceId: string,
    recordingId: string
  ): void;
  getSessionEngine(workspaceId: string): AgentSessionEngine;
  sealSessionActivityEventRecording(
    workspaceId: string,
    recordingId: string
  ): Promise<void>;
  startSessionActivityEventRecording(
    workspaceId: string,
    recordingId: string
  ): void;
}

export interface AgentSessionReplayDesktopComposition {
  activityPort: AgentSessionReplayActivityPort;
  service: AgentSessionReplayService;
}

export function createAgentSessionReplayDesktopComposition(input: {
  activityPort: AgentSessionReplayActivityPort;
  runtimeApi: Pick<DesktopRuntimeApi, "importAgentSessionReplayCassettes">;
  tuttidClient: TuttidClient;
  workspaceId: string;
}): AgentSessionReplayDesktopComposition {
  const { activityPort, runtimeApi, tuttidClient, workspaceId } = input;
  return {
    activityPort,
    service: new AgentSessionReplayService({
      armNextSessionRecording: (recordingId) =>
        activityPort.armNextSessionRecording(workspaceId, recordingId),
      clearNextSessionRecording: (recordingId) =>
        activityPort.clearNextSessionRecording(workspaceId, recordingId),
      discardActivityEventRecording: (recordingId) =>
        activityPort.discardSessionActivityEventRecording(
          workspaceId,
          recordingId
        ),
      importCassettes: () =>
        runtimeApi.importAgentSessionReplayCassettes({ workspaceId }),
      sealActivityEventRecording: (recordingId) =>
        activityPort.sealSessionActivityEventRecording(
          workspaceId,
          recordingId
        ),
      startActivityEventRecording: (recordingId) =>
        activityPort.startSessionActivityEventRecording(
          workspaceId,
          recordingId
        ),
      tuttidClient,
      workspaceId
    })
  };
}
