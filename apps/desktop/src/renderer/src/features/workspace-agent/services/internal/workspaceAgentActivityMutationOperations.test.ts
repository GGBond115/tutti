import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentActivityAdapter,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopAgentActivityCommandAdapter } from "../desktopAgentActivityAdapter.ts";
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

test("direct mutation transports preserve Engine provenance and omit it for direct calls", async () => {
  const received: Array<{ kind: string; options: unknown }> = [];
  const tuttidClient: Partial<TuttidClient> = {
    async cancelWorkspaceAgentTurn(
      _workspaceId,
      _agentSessionId,
      _turnId,
      options
    ) {
      received.push({ kind: "cancel", options });
      return { cancel: { canceled: true, reason: "turn_canceled" } };
    },
    async submitWorkspaceAgentPlanDecision(
      workspaceId,
      agentSessionId,
      turnId,
      requestId,
      request,
      options
    ) {
      received.push({ kind: "plan", options });
      return {
        operation: {
          agentSessionId,
          idempotencyKey: request.idempotencyKey,
          operationId: "operation-1",
          requestId,
          status: "completed",
          turnId,
          workspaceId
        }
      };
    },
    async updateWorkspaceAgentSessionSettings(
      _workspaceId,
      _agentSessionId,
      _settings,
      options
    ) {
      received.push({ kind: "settings", options });
      throw new Error("settings transport observed");
    }
  };
  const operations = createOperations({
    adapter: {} as AgentActivityAdapter,
    tuttidClient: tuttidClient as TuttidClient
  });
  const engineOptions = {
    commandId: "engine-command-1",
    origin: "engine" as const
  };

  const cancelInput = {
    agentSessionId: "session-1",
    turnId: "turn-1",
    workspaceId: "ws-1"
  };
  const planInput = {
    action: "implement" as const,
    agentSessionId: "session-1",
    idempotencyKey: "decision-1",
    promptKind: "plan-implementation" as const,
    requestId: "request-1",
    turnId: "turn-1",
    workspaceId: "ws-1"
  };
  const settingsInput = {
    agentSessionId: "session-1",
    settings: {},
    workspaceId: "ws-1"
  };

  await operations.executeEngineCancelTurn(cancelInput, engineOptions);
  await operations.executeEngineSubmitPlanDecision(planInput, engineOptions);
  await assert.rejects(
    operations.executeEngineUpdateSessionSettings(settingsInput, engineOptions),
    /settings transport observed/
  );
  await operations.cancelTurn(cancelInput);
  await operations.submitPlanDecision(planInput);
  await assert.rejects(
    operations.updateSessionSettings(settingsInput),
    /settings transport observed/
  );

  assert.deepEqual(
    received.map(({ kind, options }) => ({
      kind,
      origin: (options as { agentCommandOrigin?: string } | undefined)
        ?.agentCommandOrigin
    })),
    [
      {
        kind: "cancel",
        origin: "renderer-engine"
      },
      {
        kind: "plan",
        origin: "renderer-engine"
      },
      {
        kind: "settings",
        origin: "renderer-engine"
      },
      { kind: "cancel", origin: undefined },
      { kind: "plan", origin: undefined },
      { kind: "settings", origin: undefined }
    ]
  );
});

function createOperations(input: {
  adapter: AgentActivityAdapter;
  tuttidClient?: TuttidClient;
  upsertAuthoritativeSession?: (
    session: AgentActivitySession,
    source: string
  ) => void;
}): WorkspaceAgentActivityMutationOperations {
  return new WorkspaceAgentActivityMutationOperations({
    runtimeApi: { logTerminalDiagnostic: async () => {} },
    sessionCommandTarget: () => ({
      adapter: input.adapter as DesktopAgentActivityCommandAdapter
    }),
    tuttidClient: input.tuttidClient ?? ({} as TuttidClient),
    upsertAuthoritativeSession: input.upsertAuthoritativeSession ?? (() => {})
  });
}
