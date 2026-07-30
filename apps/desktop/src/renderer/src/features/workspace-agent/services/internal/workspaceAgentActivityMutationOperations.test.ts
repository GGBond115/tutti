import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentActivityAdapter,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { WorkspaceAgentActivityMutationOperations } from "./workspaceAgentActivityMutationOperations.ts";

test("WorkspaceAgentActivityMutationOperations sends existing-session Tutti activation updates through the facade-owned adapter", async () => {
  const updateInputs: Parameters<
    AgentActivityAdapter["updateTuttiModeActivation"]
  >[0][] = [];
  const activation = {
    agentSessionId: "session-1",
    createdAtUnixMs: 10,
    currentRevision: {
      activationId: "activation-1",
      createdAtUnixMs: 20,
      revision: 2,
      source: "badge_remove" as const,
      status: "inactive" as const
    },
    id: "activation-1",
    status: "inactive" as const,
    updatedAtUnixMs: 20,
    workspaceId: "ws-1"
  };
  const adapter = {
    async updateTuttiModeActivation(
      input: Parameters<AgentActivityAdapter["updateTuttiModeActivation"]>[0]
    ) {
      updateInputs.push(input);
      return { activation, changed: true };
    }
  } as AgentActivityAdapter;
  const operations = createOperations({ adapter });

  const result = await operations.updateTuttiModeActivation({
    agentSessionId: "session-1",
    expectedRevision: 1,
    source: "badge_remove",
    status: "inactive",
    workspaceId: "ws-1"
  });

  assert.deepEqual(updateInputs, [
    {
      agentSessionId: "session-1",
      expectedRevision: 1,
      source: "badge_remove",
      status: "inactive",
      workspaceId: "ws-1"
    }
  ]);
  assert.deepEqual(result, { activation, changed: true });
});

function createOperations(input: {
  adapter: AgentActivityAdapter;
  upsertAuthoritativeSession?: (
    session: AgentActivitySession,
    source: string
  ) => void;
}): WorkspaceAgentActivityMutationOperations {
  return new WorkspaceAgentActivityMutationOperations({
    runtimeApi: { logTerminalDiagnostic: async () => {} },
    sessionCommandTarget: () => ({ adapter: input.adapter }),
    tuttidClient: {} as TuttidClient,
    upsertAuthoritativeSession: input.upsertAuthoritativeSession ?? (() => {})
  });
}
