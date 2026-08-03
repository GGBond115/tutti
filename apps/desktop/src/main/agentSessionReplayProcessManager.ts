import { spawn } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import type {
  DesktopAgentSessionReplayLaunchPlaybackMode,
  DesktopAgentSessionReplayStatus
} from "../shared/contracts/ipc.ts";
import type { DesktopLogger } from "./logging.ts";
import {
  createAgentSessionReplayCassetteStatusWriter,
  type AgentSessionReplayCassetteStatusWriter
} from "./agentSessionReplayStatus.ts";
import {
  monitorManagedReplayWorkspace,
  type ManagedReplayChild
} from "./agentSessionReplayProcessMonitor.ts";

const defaultLaunchTimeoutMs = 180_000;

export interface AgentSessionReplayCassetteLaunch {
  cassetteDirectory: string;
  rootAgentSessionId: string;
  cassetteId: string;
}

export interface AgentSessionReplayWorkspaceLaunch {
  launchId: string;
  cassettes: AgentSessionReplayCassetteLaunch[];
  playbackMode: DesktopAgentSessionReplayLaunchPlaybackMode;
  workspaceId: string;
}

export interface AgentSessionReplayLaunchResult {
  launchId: string;
  cassetteIds: string[];
  workspaceId: string;
}

export interface AgentSessionReplayProcessManager {
  dispose(): void;
  launch(
    input: AgentSessionReplayWorkspaceLaunch
  ): Promise<AgentSessionReplayLaunchResult>;
  shutdown(): Promise<void>;
  waitForCompletion(input: {
    cassetteId: string;
    launchId: string;
  }): Promise<{ cassetteId: string }>;
}

interface CreateAgentSessionReplayProcessManagerInput {
  electronEntry?: string | null;
  electronExecutable: string;
  environment?: NodeJS.ProcessEnv;
  launchTimeoutMs?: number;
  logger: DesktopLogger;
  nodeExecutable: string;
  repositoryRoot: string | null;
  createCassetteStatusWriter?: (
    path: string
  ) => AgentSessionReplayCassetteStatusWriter;
  spawnProcess?: SpawnManagedReplayProcess;
  writeManifest?: typeof writeReplayWorkspaceManifest;
}

type SpawnManagedReplayProcess = (
  command: string,
  args: readonly string[],
  options: {
    cwd: string;
    detached: boolean;
    env: NodeJS.ProcessEnv;
    stdio: ["ignore", "pipe", "pipe"];
  }
) => ManagedReplayChild;

interface ManagedCassetteState {
  completion: Promise<{ cassetteId: string }>;
  launch: AgentSessionReplayCassetteLaunch;
  rejectCompletion(error: Error): void;
  resolveCompletion(result: { cassetteId: string }): void;
  status: DesktopAgentSessionReplayStatus;
  statusWriter: AgentSessionReplayCassetteStatusWriter;
  surfaceReady: boolean;
  terminal: boolean;
}

interface ManagedWorkspaceState {
  child: ManagedReplayChild;
  cleanupManifest(): Promise<void>;
  closed: Promise<void>;
  fatalError: Error | null;
  launch: AgentSessionReplayWorkspaceLaunch;
  cassettes: Map<string, ManagedCassetteState>;
}

export function createAgentSessionReplayProcessManager(
  input: CreateAgentSessionReplayProcessManagerInput
): AgentSessionReplayProcessManager {
  const workspaces = new Map<string, ManagedWorkspaceState>();
  const pendingLaunchIds = new Set<string>();
  const createCassetteStatusWriter =
    input.createCassetteStatusWriter ??
    createAgentSessionReplayCassetteStatusWriter;
  const writeManifest = input.writeManifest ?? writeReplayWorkspaceManifest;
  const spawnProcess: SpawnManagedReplayProcess =
    input.spawnProcess ??
    ((command, args, options) =>
      spawn(command, args, options) as ManagedReplayChild);

  return {
    async launch(launchInput) {
      let reservedLaunchId: string | null = null;
      let manifest: Awaited<
        ReturnType<typeof writeReplayWorkspaceManifest>
      > | null = null;
      let states: Map<string, ManagedCassetteState> | null = null;
      let supervisionOwnsSettlement = false;
      try {
        const launch = normalizeLaunch(launchInput);
        if (
          workspaces.has(launch.launchId) ||
          pendingLaunchIds.has(launch.launchId)
        ) {
          throw new Error(
            `Agent Session Replay launch is already active: ${launch.launchId}`
          );
        }
        reservedLaunchId = launch.launchId;
        pendingLaunchIds.add(reservedLaunchId);
        if (!input.repositoryRoot) {
          throw new Error(
            "Agent Session Replay is only available in a development checkout"
          );
        }
        manifest = await writeManifest(launch);
        const statusWriter = createCassetteStatusWriter(
          manifest.surfaceStatusPath
        );
        states = new Map(
          launch.cassettes.map((cassette) => [
            cassette.cassetteId,
            createCassetteState(cassette, statusWriter)
          ])
        );
        await Promise.all(
          [...states.values()].map((state) =>
            statusWriter.write(state.launch.cassetteId, state.status)
          )
        );
        const workspace = spawnWorkspace(launch, states, manifest);
        workspaces.set(launch.launchId, workspace);
        releaseReservation(reservedLaunchId);
        reservedLaunchId = null;
        supervisionOwnsSettlement = true;
        await workspaceReady(workspace);
        return {
          launchId: launch.launchId,
          cassetteIds: launch.cassettes.map(({ cassetteId }) => cassetteId),
          workspaceId: launch.workspaceId
        };
      } catch (error) {
        if (!supervisionOwnsSettlement) {
          releaseReservation(reservedLaunchId);
          await manifest?.cleanup().catch(() => undefined);
        }
        throw error;
      }
    },

    async waitForCompletion({ cassetteId, launchId }) {
      const state = workspaces
        .get(launchId.trim())
        ?.cassettes.get(cassetteId.trim());
      if (!state) {
        throw new Error(
          `Agent Session Replay Cassette is not active: ${launchId}/${cassetteId}`
        );
      }
      return state.completion;
    },

    async shutdown() {
      const active = [...workspaces.values()];
      for (const workspace of active) {
        workspace.child.kill("SIGTERM");
      }
      await Promise.allSettled(
        active.flatMap((workspace) =>
          [...workspace.cassettes.values()].map((cassette) =>
            settleCanceled(cassette)
          )
        )
      );
      await Promise.allSettled(
        active.map((workspace) => workspace.cleanupManifest())
      );
      workspaces.clear();
    },

    dispose() {
      for (const workspace of workspaces.values()) {
        workspace.child.kill("SIGTERM");
        void workspace.cleanupManifest();
      }
      workspaces.clear();
    }
  };

  function releaseReservation(launchId: string | null): void {
    if (launchId) pendingLaunchIds.delete(launchId);
  }

  function spawnWorkspace(
    launch: AgentSessionReplayWorkspaceLaunch,
    states: Map<string, ManagedCassetteState>,
    manifest: {
      cleanup(): Promise<void>;
      path: string;
      surfaceStatusPath: string;
    }
  ): ManagedWorkspaceState {
    const repositoryRoot = resolve(input.repositoryRoot!);
    const environment: NodeJS.ProcessEnv = {
      ...input.environment,
      TUTTI_AGENT_SESSION_REPLAY_ELECTRON_ENTRY:
        input.electronEntry?.trim() ?? "",
      TUTTI_AGENT_SESSION_REPLAY_ELECTRON_EXECUTABLE: input.electronExecutable,
      TUTTI_AGENT_SESSION_REPLAY_PARENT_PID: String(process.pid),
      TUTTI_AGENT_SESSION_REPLAY_SURFACE_STATUS_PATH: manifest.surfaceStatusPath
    };
    delete environment.ELECTRON_RUN_AS_NODE;
    delete environment.TUTTID_ACCESS_TOKEN;
    const args = [
      resolve(repositoryRoot, "tools/scripts/run-agent-session-replay.mjs"),
      "--replay-workspace-manifest",
      manifest.path,
      "--managed"
    ];
    const child = spawnProcess(input.nodeExecutable, args, {
      cwd: repositoryRoot,
      detached: process.platform !== "win32",
      env: environment,
      stdio: ["ignore", "pipe", "pipe"]
    });
    const workspace: ManagedWorkspaceState = {
      child,
      cleanupManifest: manifest.cleanup,
      closed: Promise.resolve(),
      fatalError: null,
      launch,
      cassettes: states
    };
    workspace.closed = monitorManagedReplayWorkspace(child, {
      expectedCassetteIds: new Set(states.keys()),
      logger: input.logger,
      onCheckpoint(cassetteId, checkpoint, totalCheckpoints, totalDurationMs) {
        updateCheckpoint(
          workspace,
          cassetteId,
          checkpoint,
          totalCheckpoints,
          totalDurationMs
        );
      },
      onComplete(cassetteId) {
        void settleComplete(states.get(cassetteId)!).catch((error) =>
          logSettlementError(cassetteId, error)
        );
      },
      onFailed(cassetteId, error) {
        const cause = replayFailureCause(error);
        input.logger.error("managed Agent Session Replay Cassette failed", {
          error: error.message,
          ...(cause
            ? {
                error_cause_code: cause.code,
                error_cause_message: cause.message
              }
            : {}),
          launch_id: launch.launchId,
          replay_cassette_id: cassetteId,
          workspace_id: launch.workspaceId
        });
        void settleFailed(states.get(cassetteId)!, error).catch(
          (settlementError) => logSettlementError(cassetteId, settlementError)
        );
      },
      onReady(cassetteId) {
        const cassette = states.get(cassetteId)!;
        cassette.surfaceReady = true;
        void writeCassetteStatus(cassette, {
          phase: "verifying"
        });
      },
      async onTerminated(error) {
        workspace.fatalError = error;
        await Promise.allSettled(
          [...states.values()].map((cassette) =>
            error ? settleFailed(cassette, error) : settleCanceled(cassette)
          )
        );
        workspaces.delete(launch.launchId);
        await manifest.cleanup();
      },
      timeoutMs: input.launchTimeoutMs ?? defaultLaunchTimeoutMs
    });
    child.once("close", (code, signal) => {
      input.logger.info("managed Agent Session Replay Workspace exited", {
        exit_code: code,
        replay_cassette_ids: launch.cassettes.map(
          ({ cassetteId }) => cassetteId
        ),
        signal,
        workspace_id: launch.workspaceId
      });
    });
    input.logger.info("managed Agent Session Replay Workspace starting", {
      replay_cassette_ids: launch.cassettes.map(({ cassetteId }) => cassetteId),
      workspace_id: launch.workspaceId
    });
    return workspace;
  }

  function updateCheckpoint(
    workspace: ManagedWorkspaceState,
    cassetteId: string,
    checkpoint: number,
    totalCheckpoints: number,
    totalDurationMs: number
  ): void {
    const cassette = workspace.cassettes.get(cassetteId)!;
    if (cassette.terminal) return;
    void writeCassetteStatus(cassette, {
      currentCheckpoint: checkpoint,
      phase: "replaying",
      totalDurationMs,
      totalCheckpoints
    });
  }

  async function settleComplete(cassette: ManagedCassetteState): Promise<void> {
    if (cassette.terminal) return;
    cassette.terminal = true;
    await writeCassetteStatus(cassette, { phase: "complete" });
    cassette.resolveCompletion({ cassetteId: cassette.launch.cassetteId });
  }

  async function settleFailed(
    cassette: ManagedCassetteState,
    error: unknown
  ): Promise<void> {
    if (cassette.terminal) return;
    cassette.terminal = true;
    const failure = toError(error);
    const cause = replayFailureCause(failure);
    await writeCassetteStatus(cassette, {
      ...(cause ? { errorCause: cause } : {}),
      errorMessage: failure.message,
      phase: "failed"
    });
    cassette.rejectCompletion(failure);
  }

  function replayFailureCause(
    error: Error
  ): { code: string; message: string } | null {
    const cause = error.cause as { code?: unknown; message?: unknown };
    if (
      !cause ||
      typeof cause !== "object" ||
      typeof cause.code !== "string" ||
      !cause.code.trim() ||
      typeof cause.message !== "string" ||
      !cause.message.trim()
    ) {
      return null;
    }
    return {
      code: cause.code.trim(),
      message: cause.message.trim()
    };
  }

  async function settleCanceled(cassette: ManagedCassetteState): Promise<void> {
    if (cassette.terminal) return;
    cassette.terminal = true;
    const closed = new Error("Agent Session Replay Workspace closed");
    await writeCassetteStatus(cassette, {
      active: false,
      errorMessage: closed.message,
      phase: "failed"
    });
    cassette.rejectCompletion(closed);
  }

  function logSettlementError(cassetteId: string, error: unknown): void {
    input.logger.error("Agent Session Replay Cassette settlement failed", {
      error: toError(error).message,
      replay_cassette_id: cassetteId
    });
  }

  function writeCassetteStatus(
    cassette: ManagedCassetteState,
    patch: Partial<DesktopAgentSessionReplayStatus>
  ): Promise<void> {
    cassette.status = { ...cassette.status, ...patch };
    return cassette.statusWriter.write(
      cassette.launch.cassetteId,
      cassette.status
    );
  }
}

function normalizeLaunch(
  input: AgentSessionReplayWorkspaceLaunch
): AgentSessionReplayWorkspaceLaunch {
  const launchId = requiredIdentity(input.launchId, "Replay launch");
  const workspaceId = requiredIdentity(input.workspaceId, "Workspace");
  const playbackMode = input.playbackMode;
  if (playbackMode !== "automatic" && playbackMode !== "manual") {
    throw new Error("Agent Session Replay playback mode is invalid");
  }
  if (!Array.isArray(input.cassettes) || input.cassettes.length === 0) {
    throw new Error(
      "Agent Session Replay Workspace requires at least one Cassette"
    );
  }
  const cassetteIds = new Set<string>();
  const rootSessionIds = new Set<string>();
  const cassettes = input.cassettes.map((cassette) => {
    const normalized = {
      cassetteDirectory: resolve(cassette.cassetteDirectory),
      cassetteId: requiredIdentity(cassette.cassetteId, "Replay Cassette"),
      rootAgentSessionId: requiredIdentity(
        cassette.rootAgentSessionId,
        "Root Agent Session"
      )
    };
    if (
      cassetteIds.has(normalized.cassetteId) ||
      rootSessionIds.has(normalized.rootAgentSessionId)
    ) {
      throw new Error(
        "Agent Session Replay Workspace contains duplicate identities"
      );
    }
    cassetteIds.add(normalized.cassetteId);
    rootSessionIds.add(normalized.rootAgentSessionId);
    return normalized;
  });
  return { launchId, cassettes, playbackMode, workspaceId };
}

async function writeReplayWorkspaceManifest(
  launch: AgentSessionReplayWorkspaceLaunch
): Promise<{
  cleanup(): Promise<void>;
  path: string;
  surfaceStatusPath: string;
}> {
  const directory = await mkdtemp(
    join(tmpdir(), "tutti-agent-session-replay-workspace-")
  );
  const path = join(directory, "manifest.json");
  const surfaceStatusPath = join(directory, "surface-status.json");
  await writeFile(path, JSON.stringify(launch), { mode: 0o600 });
  let cleaned = false;
  return {
    async cleanup() {
      if (cleaned) return;
      cleaned = true;
      await rm(directory, { force: true, recursive: true });
    },
    path,
    surfaceStatusPath
  };
}

function createCassetteState(
  launch: AgentSessionReplayCassetteLaunch,
  statusWriter: AgentSessionReplayCassetteStatusWriter
): ManagedCassetteState {
  let resolveCompletion!: (result: { cassetteId: string }) => void;
  let rejectCompletion!: (error: Error) => void;
  const completion = new Promise<{ cassetteId: string }>((resolve, reject) => {
    resolveCompletion = resolve;
    rejectCompletion = reject;
  });
  void completion.catch(() => undefined);
  return {
    completion,
    launch,
    rejectCompletion,
    resolveCompletion,
    status: {
      active: true,
      cassetteId: launch.cassetteId,
      currentCheckpoint: 0,
      phase: "replaying",
      totalCheckpoints: 1
    },
    statusWriter,
    surfaceReady: false,
    terminal: false
  };
}

function workspaceReady(workspace: ManagedWorkspaceState): Promise<void> {
  if (
    [...workspace.cassettes.values()].every(
      (cassette) => cassette.surfaceReady || cassette.terminal
    )
  ) {
    return Promise.resolve();
  }
  return new Promise<void>((resolveReady, rejectReady) => {
    const interval = setInterval(() => {
      if (workspace.fatalError) {
        clearInterval(interval);
        rejectReady(workspace.fatalError);
        return;
      }
      if (
        [...workspace.cassettes.values()].every(
          (cassette) => cassette.surfaceReady || cassette.terminal
        )
      ) {
        clearInterval(interval);
        resolveReady();
      }
    }, 10);
    void workspace.closed.then(() => {
      clearInterval(interval);
      if (workspace.fatalError) {
        rejectReady(workspace.fatalError);
      } else if (
        [...workspace.cassettes.values()].every(
          (cassette) => cassette.surfaceReady || cassette.terminal
        )
      ) {
        resolveReady();
      } else {
        rejectReady(
          new Error(
            "Agent Session Replay Workspace exited before it became ready"
          )
        );
      }
    });
  });
}

function requiredIdentity(value: string | undefined, label: string): string {
  const identity = value?.trim() ?? "";
  if (!identity) throw new Error(`${label} id is required`);
  return identity;
}

function toError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}
