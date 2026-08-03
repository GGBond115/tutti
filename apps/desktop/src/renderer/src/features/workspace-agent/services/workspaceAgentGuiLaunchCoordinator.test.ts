import assert from "node:assert/strict";
import test from "node:test";
import {
  registerWorkspaceAgentGuiLaunchHandler,
  requestWorkspaceAgentGuiLaunch,
  requestWorkspaceAgentGuiNodeLaunch
} from "./workspaceAgentGuiLaunchCoordinator.ts";

test("workspace Agent GUI launch coordinator dispatches to the workspace handler", async () => {
  const requests: unknown[] = [];
  const dispose = registerWorkspaceAgentGuiLaunchHandler(
    " workspace-1 ",
    (request) => {
      requests.push(request);
    }
  );

  const launched = await requestWorkspaceAgentGuiLaunch({
    agentSessionId: "session-1",
    workspaceId: " workspace-1 "
  });
  dispose();

  assert.equal(launched, true);
  assert.deepEqual(requests, [
    {
      agentSessionId: "session-1",
      workspaceId: " workspace-1 "
    }
  ]);
});

test("workspace Agent GUI launch coordinator keeps a replacement handler registered", async () => {
  const handled: string[] = [];
  const disposeFirst = registerWorkspaceAgentGuiLaunchHandler(
    "workspace-2",
    () => {
      handled.push("first");
    }
  );
  const disposeSecond = registerWorkspaceAgentGuiLaunchHandler(
    "workspace-2",
    () => {
      handled.push("second");
    }
  );

  disposeFirst();
  assert.equal(
    await requestWorkspaceAgentGuiLaunch({ workspaceId: "workspace-2" }),
    true
  );
  disposeSecond();
  assert.equal(
    await requestWorkspaceAgentGuiLaunch({ workspaceId: "workspace-2" }),
    false
  );
  assert.deepEqual(handled, ["second"]);
});

test("workspace Agent GUI launch coordinator rejects empty workspace ids", async () => {
  const dispose = registerWorkspaceAgentGuiLaunchHandler(" ", () => {});
  dispose();

  assert.equal(
    await requestWorkspaceAgentGuiLaunch({ workspaceId: " " }),
    false
  );
});

test("workspace Agent GUI launch coordinator returns the launched node id", async () => {
  const requests: unknown[] = [];
  const dispose = registerWorkspaceAgentGuiLaunchHandler(
    "workspace-node",
    (request) => {
      requests.push(request);
      return "node-1";
    }
  );

  assert.equal(
    await requestWorkspaceAgentGuiNodeLaunch({
      agentSessionId: "session-1",
      agentTargetId: "target-1",
      forceNewInstance: true,
      workspaceId: "workspace-node"
    }),
    "node-1"
  );
  assert.deepEqual(requests, [
    {
      agentSessionId: "session-1",
      agentTargetId: "target-1",
      forceNewInstance: true,
      workspaceId: "workspace-node"
    }
  ]);
  dispose();
});
