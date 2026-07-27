import assert from "node:assert/strict";
import test from "node:test";
import type { TuttiModeActivationUpdateCommand } from "@tutti-os/agent-activity-core";
import { executeWorkspaceAgentTuttiModeUpdateCommand } from "./workspaceAgentSessionEngineHost.ts";

test("Tutti mode update command preserves CAS revision and zero intensity", async () => {
  const controller = new AbortController();
  let received: unknown;
  await executeWorkspaceAgentTuttiModeUpdateCommand(
    {
      updateTuttiModeActivation: async (input) => {
        received = input;
        return {} as never;
      }
    },
    {
      agentSessionId: "session-1",
      commandId: "tutti-1",
      expectedRevision: 3,
      orchestrationIntensity: 0,
      source: "slash_command",
      status: "active",
      type: "tuttiMode/update",
      workspaceId: "workspace-1"
    } satisfies TuttiModeActivationUpdateCommand,
    controller.signal
  );

  assert.deepEqual(received, {
    agentSessionId: "session-1",
    expectedRevision: 3,
    orchestrationIntensity: 0,
    signal: controller.signal,
    source: "slash_command",
    status: "active",
    workspaceId: "workspace-1"
  });
});
