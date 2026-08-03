import type { BrowserWindow } from "electron";
import { stat } from "node:fs/promises";
import { dirname } from "node:path";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopFileDialogAccess } from "./host/desktopFileDialogAccess.ts";
import type {
  DesktopImportAgentSessionReplayCassettesInput,
  DesktopImportAgentSessionReplayCassettesResult
} from "../shared/contracts/ipc.ts";

export function createAgentSessionReplayImportAccess(
  client: Pick<TuttidClient, "importAgentSessionCassettes">,
  fileDialogs: Pick<DesktopFileDialogAccess, "selectUploadFiles">,
  isDirectory: (path: string) => Promise<boolean> = async (path) =>
    (await stat(path)).isDirectory()
) {
  return {
    async importCassettes(
      input: DesktopImportAgentSessionReplayCassettesInput,
      ownerWindow?: BrowserWindow | null
    ): Promise<DesktopImportAgentSessionReplayCassettesResult> {
      const workspaceId = input.workspaceId.trim();
      if (!workspaceId) {
        throw new Error("Replay Workspace id is required");
      }
      const selectedPaths = await fileDialogs.selectUploadFiles(ownerWindow, {
        allowDirectories: true
      });
      if (selectedPaths.length === 0) {
        return { canceled: true, failedCount: 0, importedCount: 0 };
      }
      const directories = await Promise.all(
        selectedPaths.map(async (selectedPath) =>
          (await isDirectory(selectedPath))
            ? selectedPath
            : dirname(selectedPath)
        )
      );
      const sourceDirectories = [...new Set(directories)];
      const result = await client.importAgentSessionCassettes(workspaceId, {
        sourceDirectories
      });
      return {
        canceled: false,
        failedCount: result.failures.length,
        importedCount: result.recordings.length
      };
    }
  };
}
