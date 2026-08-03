import type {
  AgentActivityAdapter,
  AgentActivityGoalControlResult,
  AgentActivitySession,
  EngineEffectOptions
} from "@tutti-os/agent-activity-core";
import { tuttiAgentSessionComposerSettingsFromActivity } from "@tutti-os/agent-activity-tuttid-adapter";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopRuntimeApi } from "@preload/types";
import {
  agentActivitySessionFromTuttidSession,
  type DesktopAgentActivityCommandAdapter
} from "../desktopAgentActivityAdapter.ts";
import { reportAgentSubmitTraceDiagnostic } from "../desktopAgentRuntimeSubmitDiagnostics.ts";
import type { IWorkspaceAgentActivityService } from "../workspaceAgentActivityService.interface.ts";
import { normalizeComposerSettings } from "./desktopAgentHostProjection.ts";
import { normalizeWorkspaceId } from "./workspaceAgentActivityDiagnostics.ts";

interface WorkspaceAgentActivityMutationCommandTarget {
  adapter: DesktopAgentActivityCommandAdapter;
}

export interface WorkspaceAgentActivityMutationOperationsDependencies {
  runtimeApi: Pick<DesktopRuntimeApi, "logTerminalDiagnostic">;
  sessionCommandTarget(
    workspaceId: string
  ): WorkspaceAgentActivityMutationCommandTarget;
  tuttidClient: TuttidClient;
  upsertAuthoritativeSession(
    session: AgentActivitySession,
    source: string
  ): void;
}

export class WorkspaceAgentActivityMutationOperations {
  private readonly dependencies: WorkspaceAgentActivityMutationOperationsDependencies;

  constructor(
    dependencies: WorkspaceAgentActivityMutationOperationsDependencies
  ) {
    this.dependencies = dependencies;
  }

  async createSession(
    input: Parameters<AgentActivityAdapter["createSession"]>[0]
  ): Promise<AgentActivitySession> {
    return this.createSessionWithOptions(input);
  }

  executeEngineCreateSession(
    input: Parameters<AgentActivityAdapter["createSession"]>[0],
    options: EngineEffectOptions
  ): Promise<AgentActivitySession> {
    return this.createSessionWithOptions(input, options);
  }

  private async createSessionWithOptions(
    input: Parameters<AgentActivityAdapter["createSession"]>[0],
    options?: EngineEffectOptions
  ): Promise<AgentActivitySession> {
    reportAgentSubmitTraceDiagnostic(this.dependencies.runtimeApi, {
      agentSessionId: input.agentSessionId?.trim() ?? null,
      clientSubmitId: input.clientSubmitId,
      event: "activity_service.create.entered",
      provider: null,
      submitDiagnostics: input.submitDiagnostics,
      workspaceId: input.workspaceId,
      fields: { agentTargetId: input.agentTargetId ?? null }
    });
    const target = this.dependencies.sessionCommandTarget(input.workspaceId);
    reportAgentSubmitTraceDiagnostic(this.dependencies.runtimeApi, {
      agentSessionId: input.agentSessionId?.trim() ?? null,
      clientSubmitId: input.clientSubmitId,
      event: "activity_service.create.adapter_requested",
      provider: null,
      submitDiagnostics: input.submitDiagnostics,
      workspaceId: input.workspaceId,
      fields: { agentTargetId: input.agentTargetId ?? null }
    });
    const session = options
      ? await target.adapter.createSession(input, options)
      : await target.adapter.createSession(input);
    reportAgentSubmitTraceDiagnostic(this.dependencies.runtimeApi, {
      agentSessionId: session.agentSessionId,
      clientSubmitId: input.clientSubmitId,
      event: "activity_service.create.adapter_resolved",
      provider: session.provider,
      submitDiagnostics: input.submitDiagnostics,
      workspaceId: input.workspaceId,
      fields: { activeTurnPhase: session.activeTurn?.phase ?? null }
    });
    this.dependencies.upsertAuthoritativeSession(
      session,
      "create_session_result"
    );
    reportAgentSubmitTraceDiagnostic(this.dependencies.runtimeApi, {
      agentSessionId: session.agentSessionId,
      clientSubmitId: input.clientSubmitId,
      event: "activity_service.create.resolved",
      provider: session.provider,
      submitDiagnostics: input.submitDiagnostics,
      workspaceId: input.workspaceId,
      fields: { activeTurnPhase: session.activeTurn?.phase ?? null }
    });
    return session;
  }

  async cancelTurn(input: {
    agentSessionId: string;
    signal?: AbortSignal;
    turnId: string;
    workspaceId: string;
  }): Promise<
    import("@tutti-os/agent-activity-core").AgentActivityTurnCancelResponse
  > {
    return this.cancelTurnWithOptions(input);
  }

  executeEngineCancelTurn(
    input: {
      agentSessionId: string;
      signal?: AbortSignal;
      turnId: string;
      workspaceId: string;
    },
    options: EngineEffectOptions
  ): Promise<
    import("@tutti-os/agent-activity-core").AgentActivityTurnCancelResponse
  > {
    return this.cancelTurnWithOptions(input, options);
  }

  private async cancelTurnWithOptions(
    input: {
      agentSessionId: string;
      signal?: AbortSignal;
      turnId: string;
      workspaceId: string;
    },
    options?: EngineEffectOptions
  ): Promise<
    import("@tutti-os/agent-activity-core").AgentActivityTurnCancelResponse
  > {
    const workspaceId = normalizeWorkspaceId(input.workspaceId);
    return this.dependencies.tuttidClient.cancelWorkspaceAgentTurn(
      workspaceId,
      input.agentSessionId,
      input.turnId,
      agentCommandRequestOptions(options, input.signal)
    );
  }

  async goalControl(
    input: Parameters<AgentActivityAdapter["goalControl"]>[0]
  ): Promise<AgentActivityGoalControlResult> {
    return this.goalControlWithOptions(input);
  }

  executeEngineGoalControl(
    input: Parameters<AgentActivityAdapter["goalControl"]>[0],
    options?: EngineEffectOptions
  ): Promise<AgentActivityGoalControlResult> {
    return this.goalControlWithOptions(input, options);
  }

  private async goalControlWithOptions(
    input: Parameters<AgentActivityAdapter["goalControl"]>[0],
    options?: EngineEffectOptions
  ): Promise<AgentActivityGoalControlResult> {
    const target = this.dependencies.sessionCommandTarget(input.workspaceId);
    const result = options
      ? await target.adapter.goalControl(input, options)
      : await target.adapter.goalControl(input);
    this.dependencies.upsertAuthoritativeSession(
      result.session,
      "goal_control_result"
    );
    return result;
  }

  async submitInteractive(
    input: Parameters<AgentActivityAdapter["submitInteractive"]>[0]
  ): ReturnType<IWorkspaceAgentActivityService["submitInteractive"]> {
    return this.submitInteractiveWithOptions(input);
  }

  executeEngineSubmitInteractive(
    input: Parameters<AgentActivityAdapter["submitInteractive"]>[0],
    options: EngineEffectOptions
  ): ReturnType<IWorkspaceAgentActivityService["submitInteractive"]> {
    return this.submitInteractiveWithOptions(input, options);
  }

  private submitInteractiveWithOptions(
    input: Parameters<AgentActivityAdapter["submitInteractive"]>[0],
    options?: EngineEffectOptions
  ): ReturnType<IWorkspaceAgentActivityService["submitInteractive"]> {
    const adapter = this.dependencies.sessionCommandTarget(
      input.workspaceId
    ).adapter;
    return options
      ? adapter.submitInteractive(input, options)
      : adapter.submitInteractive(input);
  }

  async submitPlanDecision(
    input: Parameters<IWorkspaceAgentActivityService["submitPlanDecision"]>[0]
  ) {
    return this.submitPlanDecisionWithOptions(input);
  }

  executeEngineSubmitPlanDecision(
    input: Parameters<IWorkspaceAgentActivityService["submitPlanDecision"]>[0],
    options: EngineEffectOptions
  ) {
    return this.submitPlanDecisionWithOptions(input, options);
  }

  private async submitPlanDecisionWithOptions(
    input: Parameters<IWorkspaceAgentActivityService["submitPlanDecision"]>[0],
    options?: EngineEffectOptions
  ) {
    const request = {
      action: input.action,
      idempotencyKey: input.idempotencyKey,
      promptKind: input.promptKind
    };
    return options
      ? this.dependencies.tuttidClient.submitWorkspaceAgentPlanDecision(
          input.workspaceId,
          input.agentSessionId,
          input.turnId,
          input.requestId,
          request,
          agentCommandRequestOptions(options)
        )
      : this.dependencies.tuttidClient.submitWorkspaceAgentPlanDecision(
          input.workspaceId,
          input.agentSessionId,
          input.turnId,
          input.requestId,
          request
        );
  }

  async updateSessionSettings(input: {
    agentSessionId: string;
    signal?: AbortSignal;
    settings: Parameters<typeof normalizeComposerSettings>[0];
    workspaceId: string;
  }): ReturnType<IWorkspaceAgentActivityService["updateSessionSettings"]> {
    return this.updateSessionSettingsWithOptions(input);
  }

  executeEngineUpdateSessionSettings(
    input: {
      agentSessionId: string;
      signal?: AbortSignal;
      settings: Parameters<typeof normalizeComposerSettings>[0];
      workspaceId: string;
    },
    options: EngineEffectOptions
  ): ReturnType<IWorkspaceAgentActivityService["updateSessionSettings"]> {
    return this.updateSessionSettingsWithOptions(input, options);
  }

  private async updateSessionSettingsWithOptions(
    input: {
      agentSessionId: string;
      signal?: AbortSignal;
      settings: Parameters<typeof normalizeComposerSettings>[0];
      workspaceId: string;
    },
    options?: EngineEffectOptions
  ): ReturnType<IWorkspaceAgentActivityService["updateSessionSettings"]> {
    const normalizedSettings = normalizeComposerSettings(input.settings);
    const settingsInput =
      tuttiAgentSessionComposerSettingsFromActivity(normalizedSettings);
    const session =
      await this.dependencies.tuttidClient.updateWorkspaceAgentSessionSettings(
        input.workspaceId,
        input.agentSessionId,
        settingsInput,
        agentCommandRequestOptions(options, input.signal)
      );
    const settings = session.settings
      ? normalizeComposerSettings(session.settings)
      : normalizedSettings;
    return {
      agentSessionId: input.agentSessionId,
      settings,
      session: agentActivitySessionFromTuttidSession(input.workspaceId, session)
    };
  }

  updateTuttiModeActivation(
    input: Parameters<
      IWorkspaceAgentActivityService["updateTuttiModeActivation"]
    >[0]
  ): ReturnType<IWorkspaceAgentActivityService["updateTuttiModeActivation"]> {
    return this.dependencies
      .sessionCommandTarget(input.workspaceId)
      .adapter.updateTuttiModeActivation(input);
  }

  unactivateSession(
    input: Parameters<IWorkspaceAgentActivityService["unactivateSession"]>[0]
  ): ReturnType<IWorkspaceAgentActivityService["unactivateSession"]> {
    return Promise.resolve({
      agentSessionId: input.agentSessionId,
      buffered: false
    });
  }
}

function agentCommandRequestOptions(
  options: EngineEffectOptions | undefined,
  signal?: AbortSignal
) {
  return options?.origin === "engine"
    ? {
        agentCommandOrigin: "renderer-engine" as const,
        signal
      }
    : { signal };
}
