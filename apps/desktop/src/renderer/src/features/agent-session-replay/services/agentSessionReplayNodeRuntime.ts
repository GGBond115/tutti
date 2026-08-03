import type { DesktopRuntimeApi } from "@preload/types";
import type {
  DesktopAgentSessionReplayPlayback,
  DesktopAgentSessionReplayPlaybackSpeed,
  DesktopAgentSessionReplayStatus,
  DesktopAgentSessionReplayTimingMode,
  DesktopSendAgentSessionReplayControlInput
} from "@shared/contracts/ipc";
import {
  areAgentSessionReplayPlaybackSnapshotsEqual,
  shouldPollAgentSessionReplayPlayback
} from "./agentSessionReplayPlaybackPolling.ts";
import type { AgentSessionReplayWorkspaceCoordinator } from "./agentSessionReplayWorkspaceCoordinator.ts";

const replayPollIntervalMs = 250;

export interface AgentSessionReplayNodeSnapshot {
  cassetteId: string;
  playback: DesktopAgentSessionReplayPlayback;
  status: DesktopAgentSessionReplayStatus;
}

export type AgentSessionReplayPlaybackCommand =
  | { command: "pause" | "resume" }
  | {
      command: "set-speed";
      speed: DesktopAgentSessionReplayPlaybackSpeed;
    }
  | {
      command: "set-timing-mode";
      timingMode: DesktopAgentSessionReplayTimingMode;
    };

type ReplayRuntimeApi = Pick<
  DesktopRuntimeApi,
  | "getAgentSessionReplayPlayback"
  | "getAgentSessionReplayStatus"
  | "sendAgentSessionReplayControl"
  | "setAgentSessionReplayPlayback"
>;

type ReplayWorkspaceCoordinator = Pick<
  AgentSessionReplayWorkspaceCoordinator,
  "getCassetteForNode" | "subscribe"
>;

interface AgentSessionReplayNodeRuntimeOptions {
  clearTimeout?: (timer: ReturnType<typeof setTimeout>) => void;
  coordinator: ReplayWorkspaceCoordinator;
  nodeId: string;
  runtimeApi: ReplayRuntimeApi;
  setTimeout?: (
    callback: () => void,
    delayMs: number
  ) => ReturnType<typeof setTimeout>;
}

export interface AgentSessionReplayNodeRuntime {
  dispose(): void;
  getSnapshot(): AgentSessionReplayNodeSnapshot | null;
  sendControl(
    command: DesktopSendAgentSessionReplayControlInput["command"]
  ): Promise<void>;
  subscribe(listener: () => void): () => void;
  updatePlayback(
    command: AgentSessionReplayPlaybackCommand
  ): Promise<DesktopAgentSessionReplayPlayback>;
}

export function createAgentSessionReplayNodeRuntime(
  options: AgentSessionReplayNodeRuntimeOptions
): AgentSessionReplayNodeRuntime {
  const listeners = new Set<() => void>();
  const clearTimer = options.clearTimeout ?? globalThis.clearTimeout;
  const scheduleTimer = options.setTimeout ?? globalThis.setTimeout;
  let cassetteId: string | null = null;
  let coordinatorUnsubscribe: (() => void) | null = null;
  let disposed = false;
  let requestRevision = 0;
  let snapshot: AgentSessionReplayNodeSnapshot | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const emit = (): void => {
    for (const listener of listeners) listener();
  };

  const publish = (next: AgentSessionReplayNodeSnapshot | null): void => {
    if (
      snapshot === next ||
      (snapshot !== null &&
        next !== null &&
        snapshot.cassetteId === next.cassetteId &&
        areAgentSessionReplayPlaybackSnapshotsEqual(snapshot, next))
    ) {
      return;
    }
    snapshot = next;
    emit();
  };

  const stopTimer = (): void => {
    if (timer === null) return;
    clearTimer(timer);
    timer = null;
  };

  const poll = async (expectedCassetteId: string): Promise<void> => {
    stopTimer();
    const revision = ++requestRevision;
    try {
      const [playback, status] = await Promise.all([
        options.runtimeApi.getAgentSessionReplayPlayback({
          cassetteId: expectedCassetteId
        }),
        options.runtimeApi.getAgentSessionReplayStatus({
          cassetteId: expectedCassetteId
        })
      ]);
      if (
        disposed ||
        listeners.size === 0 ||
        cassetteId !== expectedCassetteId ||
        requestRevision !== revision
      ) {
        return;
      }
      publish(
        status.active
          ? { cassetteId: expectedCassetteId, playback, status }
          : null
      );
      if (
        !status.active ||
        shouldPollAgentSessionReplayPlayback(playback, status)
      ) {
        timer = scheduleTimer(
          () => void poll(expectedCassetteId),
          replayPollIntervalMs
        );
      }
    } catch {
      if (
        !disposed &&
        listeners.size > 0 &&
        cassetteId === expectedCassetteId &&
        requestRevision === revision
      ) {
        publish(null);
        timer = scheduleTimer(
          () => void poll(expectedCassetteId),
          replayPollIntervalMs
        );
      }
    }
  };

  const syncCassette = (): void => {
    const nextCassetteId =
      options.coordinator.getCassetteForNode(options.nodeId)?.cassetteId ??
      null;
    if (nextCassetteId === cassetteId) return;
    cassetteId = nextCassetteId;
    requestRevision += 1;
    stopTimer();
    publish(null);
    if (cassetteId) void poll(cassetteId);
  };

  const start = (): void => {
    if (coordinatorUnsubscribe || disposed) return;
    coordinatorUnsubscribe = options.coordinator.subscribe(syncCassette);
    syncCassette();
  };

  const stop = (): void => {
    coordinatorUnsubscribe?.();
    coordinatorUnsubscribe = null;
    cassetteId = null;
    snapshot = null;
    requestRevision += 1;
    stopTimer();
  };

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      stop();
      listeners.clear();
    },
    getSnapshot() {
      return snapshot;
    },
    async sendControl(command) {
      const activeCassetteId = cassetteId;
      if (!activeCassetteId) {
        throw new Error("Agent Node is not bound to a Replay Cassette");
      }
      await options.runtimeApi.sendAgentSessionReplayControl({
        cassetteId: activeCassetteId,
        command
      });
    },
    subscribe(listener) {
      if (disposed) return () => {};
      listeners.add(listener);
      if (listeners.size === 1) start();
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) stop();
      };
    },
    async updatePlayback(command) {
      const activeCassetteId = cassetteId;
      if (!activeCassetteId) {
        throw new Error("Agent Node is not bound to a Replay Cassette");
      }
      const playback = await options.runtimeApi.setAgentSessionReplayPlayback({
        ...command,
        cassetteId: activeCassetteId
      });
      if (snapshot?.cassetteId === activeCassetteId) {
        publish({ ...snapshot, playback });
      }
      return playback;
    }
  };
}
