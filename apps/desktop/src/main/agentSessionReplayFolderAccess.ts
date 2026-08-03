import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopRevealAgentSessionReplayCassetteInput } from "../shared/contracts/ipc";

export interface AgentSessionReplayFolderAccess {
  reveal(input: DesktopRevealAgentSessionReplayCassetteInput): Promise<void>;
}

export function createAgentSessionReplayFolderAccess(
  client: Pick<TuttidClient, "prepareAgentSessionReplayWorkspace">,
  showItemInFolder: (path: string) => void
): AgentSessionReplayFolderAccess {
  return {
    async reveal(input) {
      const workspaceId = input.workspaceId.trim();
      const cassetteId = input.cassetteId.trim();
      if (!workspaceId || !cassetteId) {
        throw new Error("Replay Workspace and Cassette ids are required");
      }
      const prepared = await client.prepareAgentSessionReplayWorkspace(
        workspaceId,
        { cassetteIds: [cassetteId] }
      );
      const directory = prepared.launches
        .find((launch) => launch.cassetteId === cassetteId)
        ?.cassetteDirectory.trim();
      if (!directory) {
        throw new Error("Replay Cassette directory is unavailable");
      }
      showItemInFolder(directory);
    }
  };
}
