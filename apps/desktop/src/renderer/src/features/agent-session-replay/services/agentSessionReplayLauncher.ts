import type { DesktopRuntimeApi } from "@preload/types";
import type { DesktopAgentSessionReplayLaunchPlaybackMode } from "@shared/contracts/ipc";
import type { AgentSessionReplayService } from "./agentSessionReplayService.ts";

export interface AgentSessionReplayLauncher {
  launch(
    cassetteIds: readonly string[],
    playbackMode: DesktopAgentSessionReplayLaunchPlaybackMode
  ): Promise<{
    completion: Promise<void>;
  }>;
}

export function createAgentSessionReplayLauncher(input: {
  createLaunchId?: () => string;
  createReplayWorkspaceId?: () => string;
  runtimeApi: Pick<
    DesktopRuntimeApi,
    "launchAgentSessionReplay" | "waitForAgentSessionReplay"
  >;
  service: Pick<AgentSessionReplayService, "prepareReplayWorkspace">;
}): AgentSessionReplayLauncher {
  return {
    async launch(cassetteIds, playbackMode) {
      validateCassetteIds(cassetteIds);
      const prepared = await input.service.prepareReplayWorkspace(cassetteIds);
      const cassettes = prepared.launches;
      const launchId = input.createLaunchId
        ? input.createLaunchId()
        : crypto.randomUUID();
      const workspaceId = input.createReplayWorkspaceId
        ? input.createReplayWorkspaceId()
        : crypto.randomUUID();
      await input.runtimeApi.launchAgentSessionReplay({
        launchId,
        cassettes,
        playbackMode,
        workspaceId
      });
      const completion = (async () => {
        await Promise.all(
          cassettes.map((cassette) =>
            input.runtimeApi.waitForAgentSessionReplay({
              cassetteId: cassette.cassetteId,
              launchId
            })
          )
        );
      })();
      void completion.catch(() => undefined);
      return { completion };
    }
  };
}

function validateCassetteIds(cassetteIds: readonly string[]): void {
  if (cassetteIds.length === 0) {
    throw new Error("Replay Workspace requires at least one Cassette");
  }
  const uniqueIds = new Set<string>();
  for (const cassetteId of cassetteIds) {
    const normalized = cassetteId.trim();
    if (!normalized || uniqueIds.has(normalized)) {
      throw new Error("Replay Workspace Cassette identities must be unique");
    }
    uniqueIds.add(normalized);
  }
}
