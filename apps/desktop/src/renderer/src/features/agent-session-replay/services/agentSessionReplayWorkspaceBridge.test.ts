import assert from "node:assert/strict";
import test from "node:test";
import type { AgentSessionActivityReplayDriver } from "./agentSessionActivityReplayDriver.ts";
import {
  installAgentSessionReplayWorkspaceBridge,
  type AgentSessionReplayWorkspaceBridge
} from "./agentSessionReplayWorkspaceBridge.ts";
import { AgentSessionReplayWorkspaceCoordinator } from "./agentSessionReplayWorkspaceCoordinator.ts";

interface ReplayGlobal {
  __tuttiAgentSessionReplayDriver?: AgentSessionActivityReplayDriver;
  __tuttiAgentSessionReplayWorkspace?: AgentSessionReplayWorkspaceBridge;
}

test("publishes the CDP bridge and registers every Cassette before Node launch", async () => {
  const events: string[] = [];
  const replayGlobal = globalThis as typeof globalThis & ReplayGlobal;
  replayGlobal.__tuttiAgentSessionReplayDriver = {
    dispatchCassetteIntent() {},
    dispose() {},
    hasRegisteredCassettes() {
      return true;
    },
    observeCommand() {},
    observeIntent() {},
    registerCassette(registration) {
      events.push(`register:${registration.cassetteId}`);
      return {
        dispatchIntent() {},
        dispose() {},
        async verifyEffect() {}
      };
    },
    removeCassette(cassetteId) {
      events.push(`remove:${cassetteId}`);
    },
    async verifyCassetteEffect() {}
  };
  const installed = installAgentSessionReplayWorkspaceBridge({
    arrangeNodes(nodeIds) {
      events.push(`arrange:${nodeIds.join(",")}`);
    },
    coordinator: new AgentSessionReplayWorkspaceCoordinator("workspace-1"),
    async launchNode(cassette) {
      events.push(`launch:${cassette.cassetteId}`);
      return `node-${cassette.cassetteId}`;
    }
  });

  const snapshot =
    await replayGlobal.__tuttiAgentSessionReplayWorkspace?.bootstrap([
      {
        agentTargetId: "local:codex",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1",
        mode: "create-session"
      },
      {
        agentTargetId: "local:claude-code",
        rootAgentSessionId: "session-2",
        cassetteId: "cassette-2",
        mode: "continue-session"
      }
    ]);

  assert.deepEqual(events, [
    "register:cassette-1",
    "register:cassette-2",
    "launch:cassette-1",
    "launch:cassette-2",
    "arrange:node-cassette-1,node-cassette-2"
  ]);
  assert.deepEqual(
    snapshot?.cassettes.map((cassette) => [
      cassette.cassetteId,
      cassette.nodeId
    ]),
    [
      ["cassette-1", "node-cassette-1"],
      ["cassette-2", "node-cassette-2"]
    ]
  );
  await installed.bridge.activate("cassette-1");
  assert.equal(events.at(-1), "launch:cassette-1");
  installed.dispose();
  assert.equal(replayGlobal.__tuttiAgentSessionReplayWorkspace, undefined);
  delete replayGlobal.__tuttiAgentSessionReplayDriver;
});

test("arranges a single Replay Agent Node", async () => {
  const replayGlobal = globalThis as typeof globalThis & ReplayGlobal;
  replayGlobal.__tuttiAgentSessionReplayDriver = {
    dispatchCassetteIntent() {},
    dispose() {},
    hasRegisteredCassettes() {
      return true;
    },
    observeCommand() {},
    observeIntent() {},
    registerCassette() {
      return {
        dispatchIntent() {},
        dispose() {},
        async verifyEffect() {}
      };
    },
    removeCassette() {},
    async verifyCassetteEffect() {}
  };
  const arrangements: string[][] = [];
  const installed = installAgentSessionReplayWorkspaceBridge({
    arrangeNodes(nodeIds) {
      arrangements.push([...nodeIds]);
    },
    coordinator: new AgentSessionReplayWorkspaceCoordinator("workspace-single"),
    async launchNode() {
      return "node-single";
    }
  });

  await installed.bridge.bootstrap([
    {
      agentTargetId: "local:codex",
      rootAgentSessionId: "session-single",
      cassetteId: "cassette-single",
      mode: "continue-session"
    }
  ]);

  assert.deepEqual(arrangements, [["node-single"]]);
  installed.dispose();
  delete replayGlobal.__tuttiAgentSessionReplayDriver;
});

test("fails before creating Nodes when the activity driver is not ready", async () => {
  const replayGlobal = globalThis as typeof globalThis & ReplayGlobal;
  delete replayGlobal.__tuttiAgentSessionReplayDriver;
  let launchCount = 0;
  const installed = installAgentSessionReplayWorkspaceBridge({
    arrangeNodes() {},
    coordinator: new AgentSessionReplayWorkspaceCoordinator("workspace-1"),
    async launchNode() {
      launchCount += 1;
      return "node-1";
    }
  });

  await assert.rejects(
    installed.bridge.bootstrap([
      {
        agentTargetId: "local:codex",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1",
        mode: "create-session"
      }
    ]),
    /driver is not ready/u
  );
  assert.equal(launchCount, 0);
  installed.dispose();
});

test("dispose clears canonical readiness before the same Session replays again", async () => {
  const replayGlobal = globalThis as typeof globalThis & ReplayGlobal;
  replayGlobal.__tuttiAgentSessionReplayDriver = {
    dispatchCassetteIntent() {},
    dispose() {},
    hasRegisteredCassettes() {
      return true;
    },
    observeCommand() {},
    observeIntent() {},
    registerCassette() {
      return {
        dispatchIntent() {},
        dispose() {},
        async verifyEffect() {}
      };
    },
    removeCassette() {},
    async verifyCassetteEffect() {}
  };
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  const install = () =>
    installAgentSessionReplayWorkspaceBridge({
      arrangeNodes() {},
      coordinator,
      async launchNode() {
        return "node-1";
      }
    });
  const cassette = {
    agentTargetId: "local:codex",
    rootAgentSessionId: "same-session",
    cassetteId: "same-cassette",
    mode: "continue-session" as const
  };

  const first = install();
  await first.bridge.bootstrap([cassette]);
  coordinator.reportSessionCanonicalObservation("same-session", {
    messageVersion: 9,
    updatedAtUnixMs: 99
  });
  assert.deepEqual(first.bridge.observedSession("same-session"), {
    messageVersion: 9,
    updatedAtUnixMs: 99
  });
  first.dispose();
  assert.equal(first.bridge.observedSession("same-session"), null);

  const second = install();
  await second.bridge.bootstrap([cassette]);
  assert.equal(second.bridge.observedSession("same-session"), null);
  coordinator.reportSessionCanonicalObservation("same-session", {
    messageVersion: 1,
    updatedAtUnixMs: 2
  });
  assert.deepEqual(second.bridge.observedSession("same-session"), {
    messageVersion: 1,
    updatedAtUnixMs: 2
  });
  second.dispose();
  delete replayGlobal.__tuttiAgentSessionReplayDriver;
});
