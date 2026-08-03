import assert from "node:assert/strict";
import test from "node:test";
import type {
  DesktopAgentSessionReplayPlayback,
  DesktopAgentSessionReplayStatus
} from "@shared/contracts/ipc";
import { createAgentSessionReplayNodeRuntime } from "./agentSessionReplayNodeRuntime.ts";
import { AgentSessionReplayWorkspaceCoordinator } from "./agentSessionReplayWorkspaceCoordinator.ts";

const activePlayback: DesktopAgentSessionReplayPlayback = {
  active: true,
  paused: false,
  playbackElapsedMs: 42,
  speed: 1,
  timingMode: "realtime"
};
const inactivePlayback: DesktopAgentSessionReplayPlayback = {
  active: false,
  paused: false,
  playbackElapsedMs: 0,
  speed: 1,
  timingMode: "realtime"
};
const replayingStatus: DesktopAgentSessionReplayStatus = {
  active: true,
  currentCheckpoint: 0,
  phase: "replaying",
  totalCheckpoints: 2
};

test("node runtime waits for a coordinator binding and owns one polling loop", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  const playbackRequests: unknown[] = [];
  const statusRequests: unknown[] = [];
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
  let nextTimerId = 0;
  const runtime = createAgentSessionReplayNodeRuntime({
    clearTimeout: (timer) => timers.delete(timer),
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async (input) => {
        playbackRequests.push(input);
        return activePlayback;
      },
      getAgentSessionReplayStatus: async (input) => {
        statusRequests.push(input);
        return replayingStatus;
      },
      sendAgentSessionReplayControl: async () => {},
      setAgentSessionReplayPlayback: async () => activePlayback
    },
    setTimeout: (callback) => {
      const timer = ++nextTimerId as unknown as ReturnType<typeof setTimeout>;
      timers.set(timer, callback);
      return timer;
    }
  });

  const unsubscribe = runtime.subscribe(() => {});
  assert.equal(playbackRequests.length, 0);
  assert.equal(statusRequests.length, 0);
  assert.equal(timers.size, 0);

  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  await settleAsyncWork();

  assert.deepEqual(playbackRequests, [{ cassetteId: "cassette-1" }]);
  assert.deepEqual(statusRequests, [{ cassetteId: "cassette-1" }]);
  assert.equal(runtime.getSnapshot()?.cassetteId, "cassette-1");
  assert.equal(timers.size, 1);

  const pollAgain = [...timers.values()][0];
  timers.clear();
  pollAgain?.();
  await settleAsyncWork();

  assert.equal(playbackRequests.length, 2);
  assert.equal(statusRequests.length, 2);
  assert.equal(timers.size, 1);

  unsubscribe();
  assert.equal(timers.size, 0);
  runtime.dispose();
});

test("node runtime can subscribe again after React-style subscription cleanup", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
  let nextTimerId = 0;
  let playbackRequests = 0;
  const runtime = createAgentSessionReplayNodeRuntime({
    clearTimeout: (timer) => timers.delete(timer),
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async () => {
        playbackRequests += 1;
        return activePlayback;
      },
      getAgentSessionReplayStatus: async () => replayingStatus,
      sendAgentSessionReplayControl: async () => {},
      setAgentSessionReplayPlayback: async () => activePlayback
    },
    setTimeout: (callback) => {
      const timer = ++nextTimerId as unknown as ReturnType<typeof setTimeout>;
      timers.set(timer, callback);
      return timer;
    }
  });

  const unsubscribeFirst = runtime.subscribe(() => {});
  await settleAsyncWork();
  assert.equal(playbackRequests, 1);
  assert.equal(runtime.getSnapshot()?.cassetteId, "cassette-1");

  unsubscribeFirst();
  assert.equal(timers.size, 0);
  assert.equal(runtime.getSnapshot(), null);

  const unsubscribeSecond = runtime.subscribe(() => {});
  await settleAsyncWork();
  assert.equal(playbackRequests, 2);
  assert.equal(runtime.getSnapshot()?.cassetteId, "cassette-1");
  assert.equal(timers.size, 1);

  unsubscribeSecond();
  runtime.dispose();
});

test("node runtime scopes playback and control commands to its cassette", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  const controls: unknown[] = [];
  const playbackCommands: unknown[] = [];
  const runtime = createAgentSessionReplayNodeRuntime({
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async () => activePlayback,
      getAgentSessionReplayStatus: async () => ({
        ...replayingStatus,
        phase: "complete"
      }),
      sendAgentSessionReplayControl: async (input) => {
        controls.push(input);
      },
      setAgentSessionReplayPlayback: async (input) => {
        playbackCommands.push(input);
        return { ...activePlayback, speed: 2 };
      }
    }
  });
  const unsubscribe = runtime.subscribe(() => {});
  await settleAsyncWork();

  await runtime.sendControl("next-checkpoint");
  await runtime.updatePlayback({ command: "set-speed", speed: 2 });

  assert.deepEqual(controls, [
    { cassetteId: "cassette-1", command: "next-checkpoint" }
  ]);
  assert.deepEqual(playbackCommands, [
    { cassetteId: "cassette-1", command: "set-speed", speed: 2 }
  ]);
  assert.equal(runtime.getSnapshot()?.playback.speed, 2);

  unsubscribe();
  runtime.dispose();
});

test("node runtime publishes Replay status before transport playback becomes active", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  const runtime = createAgentSessionReplayNodeRuntime({
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async () => inactivePlayback,
      getAgentSessionReplayStatus: async () => ({
        active: true,
        phase: "complete"
      }),
      sendAgentSessionReplayControl: async () => {},
      setAgentSessionReplayPlayback: async () => inactivePlayback
    }
  });
  const unsubscribe = runtime.subscribe(() => {});
  await settleAsyncWork();

  assert.deepEqual(runtime.getSnapshot(), {
    cassetteId: "cassette-1",
    playback: inactivePlayback,
    status: {
      active: true,
      phase: "complete"
    }
  });

  unsubscribe();
  runtime.dispose();
});

test("node runtime retries when Replay transport is not ready yet", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
  let nextTimerId = 0;
  let playbackRequests = 0;
  const runtime = createAgentSessionReplayNodeRuntime({
    clearTimeout: (timer) => timers.delete(timer),
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async () => {
        playbackRequests += 1;
        if (playbackRequests === 1) {
          throw new Error("Replay transport is not ready");
        }
        return activePlayback;
      },
      getAgentSessionReplayStatus: async () => replayingStatus,
      sendAgentSessionReplayControl: async () => {},
      setAgentSessionReplayPlayback: async () => activePlayback
    },
    setTimeout: (callback) => {
      const timer = ++nextTimerId as unknown as ReturnType<typeof setTimeout>;
      timers.set(timer, callback);
      return timer;
    }
  });
  const unsubscribe = runtime.subscribe(() => {});
  await settleAsyncWork();

  assert.equal(runtime.getSnapshot(), null);
  assert.equal(timers.size, 1);

  const retry = [...timers.values()][0];
  timers.clear();
  retry?.();
  await settleAsyncWork();

  assert.equal(playbackRequests, 2);
  assert.equal(runtime.getSnapshot()?.cassetteId, "cassette-1");

  unsubscribe();
  runtime.dispose();
});

test("node runtime retries while its bound Replay status is not active yet", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-1",
        rootAgentSessionId: "session-1",
        mode: "create-session"
      }
    ],
    async () => "node-1"
  );
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
  let nextTimerId = 0;
  let statusRequests = 0;
  const runtime = createAgentSessionReplayNodeRuntime({
    clearTimeout: (timer) => timers.delete(timer),
    coordinator,
    nodeId: "node-1",
    runtimeApi: {
      getAgentSessionReplayPlayback: async () => inactivePlayback,
      getAgentSessionReplayStatus: async () => {
        statusRequests += 1;
        return statusRequests === 1 ? { active: false } : replayingStatus;
      },
      sendAgentSessionReplayControl: async () => {},
      setAgentSessionReplayPlayback: async () => inactivePlayback
    },
    setTimeout: (callback) => {
      const timer = ++nextTimerId as unknown as ReturnType<typeof setTimeout>;
      timers.set(timer, callback);
      return timer;
    }
  });
  const unsubscribe = runtime.subscribe(() => {});
  await settleAsyncWork();

  assert.equal(runtime.getSnapshot(), null);
  assert.equal(timers.size, 1);

  const retry = [...timers.values()][0];
  timers.clear();
  retry?.();
  await settleAsyncWork();

  assert.equal(statusRequests, 2);
  assert.equal(runtime.getSnapshot()?.cassetteId, "cassette-1");

  unsubscribe();
  assert.equal(timers.size, 0);
  runtime.dispose();
});

async function settleAsyncWork(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
