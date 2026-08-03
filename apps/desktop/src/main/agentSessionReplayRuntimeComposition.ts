import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { desktopIpcChannels } from "../shared/contracts/ipc.ts";
import { createAgentSessionReplayFolderAccess } from "./agentSessionReplayFolderAccess.ts";
import { createAgentSessionReplayImportAccess } from "./agentSessionReplayImportAccess.ts";
import { createAgentSessionReplayPlaybackAccess } from "./agentSessionReplayPlaybackAccess.ts";
import { createAgentSessionReplayProcessManager } from "./agentSessionReplayProcessManager.ts";
import {
  createAgentSessionReplayControlWriter,
  readAgentSessionReplayStatus
} from "./agentSessionReplayStatus.ts";
import type { DesktopFileDialogAccess } from "./host/desktopFileDialogAccess.ts";
import type { registerDesktopIpcHandler } from "./ipc/handle.ts";
import type { resolveOwnerWindowFromEvent } from "./ipc/ownerWindow.ts";
import type { DesktopLogger } from "./logging.ts";

type AgentSessionReplayTuttidClient = Pick<
  TuttidClient,
  | "getAgentSessionReplayTransportPlayback"
  | "importAgentSessionCassettes"
  | "prepareAgentSessionReplayWorkspace"
  | "updateAgentSessionReplayTransportPlayback"
>;

export interface AgentSessionReplayRuntimeCompositionInput {
  electronEntry: string | null;
  electronExecutable: string;
  enabled: boolean;
  environment: NodeJS.ProcessEnv;
  fileDialogs: Pick<DesktopFileDialogAccess, "selectUploadFiles">;
  isPackaged: boolean;
  logger: DesktopLogger;
  nodeExecutable: string;
  registerIpcHandler: typeof registerDesktopIpcHandler;
  resolveOwnerWindow: typeof resolveOwnerWindowFromEvent;
  showItemInFolder(path: string): void;
  tuttidClient: AgentSessionReplayTuttidClient;
}

export function installAgentSessionReplayRuntimeComposition(
  input: AgentSessionReplayRuntimeCompositionInput
): ReturnType<typeof createAgentSessionReplayProcessManager> | null {
  if (!input.enabled) {
    return null;
  }
  const folder = createAgentSessionReplayFolderAccess(
    input.tuttidClient,
    input.showItemInFolder
  );
  const importer = createAgentSessionReplayImportAccess(
    input.tuttidClient,
    input.fileDialogs
  );
  const manager = createAgentSessionReplayProcessManager({
    electronEntry: input.electronEntry,
    electronExecutable: input.electronExecutable,
    environment: input.environment,
    logger: input.logger,
    nodeExecutable: input.nodeExecutable,
    repositoryRoot: resolveAgentSessionReplayRoot(undefined, input.isPackaged)
  });
  const playback = createAgentSessionReplayPlaybackAccess(input.tuttidClient);
  const sendControl = createAgentSessionReplayControlWriter();

  input.registerIpcHandler(
    desktopIpcChannels.runtime.launchAgentSessionReplay,
    (_event, request) => manager.launch(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.getAgentSessionReplayPlayback,
    (_event, request) => playback.get(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.getAgentSessionReplayStatus,
    (_event, request) => readAgentSessionReplayStatus(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.importAgentSessionReplayCassettes,
    (event, request) =>
      importer.importCassettes(request, input.resolveOwnerWindow(event))
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.revealAgentSessionReplayCassette,
    (_event, request) => folder.reveal(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.setAgentSessionReplayPlayback,
    (_event, request) => playback.update(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.sendAgentSessionReplayControl,
    (_event, request) => sendControl(request)
  );
  input.registerIpcHandler(
    desktopIpcChannels.runtime.waitForAgentSessionReplay,
    (_event, request) => manager.waitForCompletion(request)
  );
  return manager;
}

export function resolveAgentSessionReplayRoot(
  currentDirectory = dirname(fileURLToPath(import.meta.url)),
  isPackaged = false
): string | null {
  if (isPackaged) {
    return null;
  }
  let candidate = resolve(currentDirectory);
  for (;;) {
    if (
      existsSync(join(candidate, "pnpm-workspace.yaml")) &&
      existsSync(
        join(candidate, "tools", "scripts", "run-agent-session-replay.mjs")
      )
    ) {
      return candidate;
    }
    const parent = dirname(candidate);
    if (parent === candidate) {
      return null;
    }
    candidate = parent;
  }
}
