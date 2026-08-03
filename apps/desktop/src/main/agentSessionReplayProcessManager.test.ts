import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { access, readFile } from "node:fs/promises";
import { PassThrough } from "node:stream";
import test from "node:test";
import type { DesktopLogger } from "./logging.ts";
import {
  createAgentSessionReplayProcessManager,
  type AgentSessionReplayWorkspaceLaunch
} from "./agentSessionReplayProcessManager.ts";
import { monitorManagedReplayWorkspace } from "./agentSessionReplayProcessMonitor.ts";

const launch: AgentSessionReplayWorkspaceLaunch = {
  launchId: "launch-1",
  cassettes: [
    {
      cassetteDirectory: "/cassettes/one",
      cassetteId: "cassette-1",
      rootAgentSessionId: "session-1"
    },
    {
      cassetteDirectory: "/cassettes/two",
      cassetteId: "cassette-2",
      rootAgentSessionId: "session-2"
    }
  ],
  playbackMode: "automatic",
  workspaceId: "workspace-1"
};

test("launches one child with one fixed Cassette Workspace manifest", async () => {
  const child = createChild();
  let invocation:
    | { args: readonly string[]; command: string; env: NodeJS.ProcessEnv }
    | undefined;
  const manager = createAgentSessionReplayProcessManager({
    electronEntry: "/repo/apps/desktop/out/main/index.js",
    electronExecutable: "/electron",
    environment: { TUTTID_ACCESS_TOKEN: "must-not-leak" },
    logger: createLogger(),
    nodeExecutable: "/node",
    repositoryRoot: "/repo",
    spawnProcess(command, args, options) {
      invocation = { args, command, env: options.env };
      queueMicrotask(() => {
        writeEvent(child, "ready", { cassetteId: "cassette-1" });
        writeEvent(child, "ready", { cassetteId: "cassette-2" });
      });
      return child;
    }
  });
  try {
    assert.deepEqual(await manager.launch(launch), {
      launchId: "launch-1",
      cassetteIds: ["cassette-1", "cassette-2"],
      workspaceId: "workspace-1"
    });
    const manifestPath =
      invocation?.args[
        invocation.args.indexOf("--replay-workspace-manifest") + 1
      ];
    assert.ok(manifestPath);
    assert.deepEqual(JSON.parse(await readFile(manifestPath, "utf8")), launch);
    assert.equal(invocation?.env.TUTTID_ACCESS_TOKEN, undefined);
    assert.ok(invocation?.env.TUTTI_AGENT_SESSION_REPLAY_SURFACE_STATUS_PATH);
    await manager.shutdown();
    await assert.rejects(access(manifestPath));
  } finally {
    manager.dispose();
  }
});

test("settles interleaved Cassettes through local status only", async () => {
  const child = createChild();
  const errors: {
    fields?: Record<string, unknown>;
    message: string;
  }[] = [];
  let surfaceStatusPath = "";
  const manager = createManager(child, {
    logger: {
      ...createLogger(),
      error(message, fields) {
        errors.push({ fields, message });
      }
    },
    onSpawn(environment) {
      surfaceStatusPath =
        environment.TUTTI_AGENT_SESSION_REPLAY_SURFACE_STATUS_PATH ?? "";
    }
  });
  try {
    const launched = manager.launch(launch);
    writeEvent(child, "ready", { cassetteId: "cassette-1" });
    writeEvent(child, "ready", { cassetteId: "cassette-2" });
    await launched;
    const first = manager.waitForCompletion({
      cassetteId: "cassette-1",
      launchId: launch.launchId
    });
    const second = manager.waitForCompletion({
      cassetteId: "cassette-2",
      launchId: launch.launchId
    });
    writeEvent(child, "failed", {
      cassetteId: "cassette-1",
      cause: {
        code: "managed_process_stderr",
        message: "unsupported process cassette schema version 2"
      },
      error: "Replay Workspace failed to start"
    });
    writeEvent(child, "checkpoint", {
      cassetteId: "cassette-2",
      checkpoint: 3,
      totalDurationMs: 12_000,
      totalCheckpoints: 8
    });
    writeEvent(child, "complete", { cassetteId: "cassette-2" });
    await assert.rejects(first, /Replay Workspace failed to start/u);
    assert.deepEqual(await second, { cassetteId: "cassette-2" });
    assert.deepEqual(errors, [
      {
        fields: {
          error: "Replay Workspace failed to start",
          error_cause_code: "managed_process_stderr",
          error_cause_message: "unsupported process cassette schema version 2",
          launch_id: "launch-1",
          replay_cassette_id: "cassette-1",
          workspace_id: "workspace-1"
        },
        message: "managed Agent Session Replay Cassette failed"
      }
    ]);
    await waitFor(async () => {
      const statuses = JSON.parse(
        await readFile(surfaceStatusPath, "utf8")
      ).cassettes;
      return (
        statuses["cassette-1"].phase === "failed" &&
        statuses["cassette-1"].errorCause?.code === "managed_process_stderr" &&
        statuses["cassette-1"].errorCause?.message ===
          "unsupported process cassette schema version 2" &&
        statuses["cassette-2"].phase === "complete" &&
        statuses["cassette-2"].currentCheckpoint === 3 &&
        statuses["cassette-2"].totalDurationMs === 12_000 &&
        statuses["cassette-2"].totalCheckpoints === 8
      );
    });
  } finally {
    await manager.shutdown();
  }
});

test("allows the same Cassette in different launches", async () => {
  const children = [createChild(), createChild()];
  let index = 0;
  const manager = createAgentSessionReplayProcessManager({
    electronExecutable: "/electron",
    logger: createLogger(),
    nodeExecutable: "/node",
    repositoryRoot: "/repo",
    spawnProcess() {
      const child = children[index++]!;
      queueMicrotask(() =>
        writeEvent(child, "ready", { cassetteId: "cassette-1" })
      );
      return child;
    }
  });
  try {
    await Promise.all([
      manager.launch({
        ...launch,
        launchId: "launch-a",
        cassettes: [launch.cassettes[0]!]
      }),
      manager.launch({
        ...launch,
        launchId: "launch-b",
        cassettes: [launch.cassettes[0]!]
      })
    ]);
    assert.equal(index, 2);
  } finally {
    await manager.shutdown();
  }
});

test("reserves launch identity before the first async boundary", async () => {
  let releaseManifest!: (value: {
    cleanup(): Promise<void>;
    path: string;
    surfaceStatusPath: string;
  }) => void;
  const manifest = new Promise<{
    cleanup(): Promise<void>;
    path: string;
    surfaceStatusPath: string;
  }>((resolve) => {
    releaseManifest = resolve;
  });
  const manager = createAgentSessionReplayProcessManager({
    createCassetteStatusWriter: () => ({ async write() {} }),
    electronExecutable: "/electron",
    logger: createLogger(),
    nodeExecutable: "/node",
    repositoryRoot: "/repo",
    spawnProcess() {
      const child = createChild();
      queueMicrotask(() => {
        writeEvent(child, "ready", { cassetteId: "cassette-1" });
        writeEvent(child, "ready", { cassetteId: "cassette-2" });
      });
      return child;
    },
    writeManifest: () => manifest
  });
  const first = manager.launch(launch);
  await assert.rejects(manager.launch(launch), /launch is already active/u);
  releaseManifest({
    async cleanup() {},
    path: "/tmp/replay-manifest.json",
    surfaceStatusPath: "/tmp/replay-surface-status.json"
  });
  await first;
  await manager.shutdown();
});

test("rejects duplicate Cassette identities inside one batch", async () => {
  const manager = createManager(createChild());
  await assert.rejects(
    manager.launch({
      ...launch,
      cassettes: [launch.cassettes[0]!, launch.cassettes[0]!]
    }),
    /duplicate identities/u
  );
  manager.dispose();
});

test("rejects an unknown launch playback mode", async () => {
  const manager = createManager(createChild());
  await assert.rejects(
    manager.launch({
      ...launch,
      playbackMode: "unexpected"
    } as unknown as AgentSessionReplayWorkspaceLaunch),
    /playback mode is invalid/u
  );
  manager.dispose();
});

test("keeps the final child diagnostic emitted between exit and close", async () => {
  const child = createChild();
  let terminatedMessage = "";
  let failureLog:
    | { fields?: Record<string, unknown>; message: string }
    | undefined;
  const monitored = monitorManagedReplayWorkspace(child, {
    expectedCassetteIds: new Set(["cassette-1"]),
    logger: {
      ...createLogger(),
      error(message, fields) {
        failureLog = { fields, message };
      }
    },
    onCheckpoint() {},
    onComplete() {},
    onFailed() {},
    onReady() {},
    async onTerminated(error) {
      terminatedMessage = error?.message ?? "";
    },
    timeoutMs: 10_000
  });

  child.emit("exit", 1, null);
  child.stderr.write("final replay failure");
  child.emit("close", 1, null);
  await monitored;

  assert.match(terminatedMessage, /final replay failure/u);
  assert.match(
    failureLog?.message ?? "",
    /managed Agent Session Replay Workspace failed/u
  );
  assert.equal(failureLog?.fields?.diagnostics, "final replay failure");
  assert.equal(failureLog?.fields?.exit_code, 1);
  assert.deepEqual(failureLog?.fields?.expected_cassette_ids, ["cassette-1"]);
  assert.deepEqual(failureLog?.fields?.ready_cassette_ids, []);
  assert.deepEqual(failureLog?.fields?.terminal_cassette_ids, []);
});

function createManager(
  child: ReturnType<typeof createChild>,
  options: {
    logger?: DesktopLogger;
    onSpawn?: (environment: NodeJS.ProcessEnv) => void;
  } = {}
) {
  return createAgentSessionReplayProcessManager({
    electronExecutable: "/electron",
    logger: options.logger ?? createLogger(),
    nodeExecutable: "/node",
    repositoryRoot: "/repo",
    spawnProcess(_command, _args, spawnOptions) {
      options.onSpawn?.(spawnOptions.env);
      return child;
    }
  });
}

function writeEvent(
  child: ReturnType<typeof createChild>,
  event: "checkpoint" | "complete" | "failed" | "ready",
  payload: Record<string, unknown>
): void {
  child.stdout.write(
    `[tutti-agent-session-replay-${event}] ${JSON.stringify(payload)}\n`
  );
}

function createChild() {
  const child = new EventEmitter() as EventEmitter & {
    exitCode: number | null;
    killed: boolean;
    kill(signal?: NodeJS.Signals): boolean;
    signalCode: NodeJS.Signals | null;
    stderr: PassThrough;
    stdout: PassThrough;
  };
  child.stdout = new PassThrough();
  child.stderr = new PassThrough();
  child.exitCode = null;
  child.signalCode = null;
  child.killed = false;
  child.kill = () => {
    child.killed = true;
    return true;
  };
  return child;
}

function createLogger(): DesktopLogger {
  return {
    async close() {},
    debug() {},
    error() {},
    info() {},
    warn() {}
  };
}

async function waitFor(predicate: () => boolean | Promise<boolean>) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (await predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("condition was not met");
}
