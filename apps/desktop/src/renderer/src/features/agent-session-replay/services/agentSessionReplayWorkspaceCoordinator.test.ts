import assert from "node:assert/strict";
import test from "node:test";
import { AgentSessionReplayWorkspaceCoordinator } from "./agentSessionReplayWorkspaceCoordinator.ts";

test("caller-owned coordinator survives child remounts and resets between runs", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-remount",
        rootAgentSessionId: "session-remount",
        mode: "create-session"
      }
    ],
    async () => "node-remount"
  );
  assert.equal(
    coordinator.getCassetteForNode("node-remount")?.cassetteId,
    "cassette-remount"
  );
  coordinator.reset();
  assert.equal(coordinator.getSnapshot().cassettes.length, 0);
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "target-1",
        cassetteId: "cassette-remount",
        rootAgentSessionId: "session-remount",
        mode: "create-session"
      }
    ],
    async () => "node-remount"
  );
  assert.equal(
    coordinator.getCassetteForNode("node-remount")?.cassetteId,
    "cassette-remount"
  );
});

test("creates one new Agent Node per Cassette and preserves all identities", async () => {
  const requests: unknown[] = [];
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");

  const bindings = await coordinator.bootstrap(
    [
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
    ],
    async (request) => {
      requests.push(request);
      return `node-${requests.length}`;
    }
  );

  assert.deepEqual(requests, [
    {
      agentTargetId: "local:codex",
      cassetteId: "cassette-1",
      workspaceId: "workspace-1"
    },
    {
      agentTargetId: "local:claude-code",
      agentSessionId: "session-2",
      cassetteId: "cassette-2",
      workspaceId: "workspace-1"
    }
  ]);
  assert.deepEqual(
    bindings.map(({ nodeId, rootAgentSessionId, cassetteId }) => ({
      nodeId,
      rootAgentSessionId,
      cassetteId
    })),
    [
      {
        nodeId: "node-1",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1"
      },
      {
        nodeId: "node-2",
        rootAgentSessionId: "session-2",
        cassetteId: "cassette-2"
      }
    ]
  );
});

test("requires mounted, root Session selected, and detail hydrated", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "local:codex",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1",
        mode: "continue-session"
      }
    ],
    async () => "node-1"
  );

  coordinator.reportNodeMounted("node-1", true);
  coordinator.reportSelectedAgentSession("node-1", "child-session");
  coordinator.reportSessionDetailHydrated("session-1", true);
  assert.equal(coordinator.getSnapshot().ready, false);

  coordinator.reportSelectedAgentSession("node-1", "session-1");
  assert.equal(coordinator.getSnapshot().ready, true);
  coordinator.reportSessionCanonicalObservation("session-1", {
    messageVersion: 7,
    updatedAtUnixMs: 42
  });
  assert.equal(
    coordinator.getCassetteForNode("node-1")?.canonicalMessageVersion,
    7
  );
  assert.equal(
    coordinator.getCassetteForNode("node-1")?.canonicalSessionUpdatedAtUnixMs,
    42
  );

  coordinator.reportNodeMounted("node-1", false);
  assert.equal(coordinator.getSnapshot().ready, false);
});

test("reactivates a created Session through its existing Agent Node", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  const requests: unknown[] = [];
  const launchNode = async (request: unknown) => {
    requests.push(request);
    return "node-1";
  };
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "local:codex",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1",
        mode: "create-session"
      }
    ],
    launchNode
  );

  await coordinator.activateCassette("cassette-1", launchNode);

  assert.equal(requests.length, 2);
  assert.deepEqual(requests, [
    {
      agentTargetId: "local:codex",
      cassetteId: "cassette-1",
      workspaceId: "workspace-1"
    },
    {
      agentTargetId: "local:codex",
      agentSessionId: "session-1",
      cassetteId: "cassette-1",
      nodeId: "node-1",
      workspaceId: "workspace-1"
    }
  ]);
});

test("resolves controls by current Node and fails closed for unknown Nodes", async () => {
  const coordinator = new AgentSessionReplayWorkspaceCoordinator("workspace-1");
  await coordinator.bootstrap(
    [
      {
        agentTargetId: "local:codex",
        rootAgentSessionId: "session-1",
        cassetteId: "cassette-1",
        mode: "continue-session"
      },
      {
        agentTargetId: "local:claude-code",
        rootAgentSessionId: "session-2",
        cassetteId: "cassette-2",
        mode: "continue-session"
      }
    ],
    async ({ agentSessionId }) =>
      agentSessionId === "session-1" ? "node-1" : "node-2"
  );

  assert.equal(coordinator.resolveCassetteIdForNode("node-2"), "cassette-2");
  assert.deepEqual(
    coordinator.createCassetteScopedControl("node-2", { command: "pause" }),
    { command: "pause", cassetteId: "cassette-2" }
  );
  assert.throws(
    () => coordinator.resolveCassetteIdForNode("unknown-node"),
    /not bound/u
  );
});

test("fails closed on duplicate identities or duplicate Node ids", async () => {
  const duplicateRuns = new AgentSessionReplayWorkspaceCoordinator(
    "workspace-1"
  );
  await assert.rejects(
    duplicateRuns.bootstrap(
      [
        {
          agentTargetId: "local:codex",
          rootAgentSessionId: "session-1",
          cassetteId: "cassette-1",
          mode: "create-session"
        },
        {
          agentTargetId: "local:claude-code",
          rootAgentSessionId: "session-2",
          cassetteId: "cassette-1",
          mode: "create-session"
        }
      ],
      async () => "unused"
    ),
    /unique/u
  );

  const duplicateNodes = new AgentSessionReplayWorkspaceCoordinator(
    "workspace-1"
  );
  await assert.rejects(
    duplicateNodes.bootstrap(
      [
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
          mode: "create-session"
        }
      ],
      async () => "node-1"
    ),
    /already bound/u
  );
});
