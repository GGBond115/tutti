import type {
  AgentActivityAdapter,
  AgentActivityGoalControlResult,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import { tuttiAgentSessionComposerSettingsFromActivity } from "@tutti-os/agent-activity-tuttid-adapter";
import type { AgentActivityRuntime } from "@tutti-os/agent-gui";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopRuntimeApi } from "@preload/types";
import { agentActivitySessionFromTuttidSession } from "../desktopAgentActivityAdapter.ts";
import { reportAgentSubmitTraceDiagnostic } from "../desktopAgentRuntimeSubmitDiagnostics.ts";
import type { IWorkspaceAgentActivityService } from "../workspaceAgentActivityService.interface.ts";
import { normalizeComposerSettings } from "./desktopAgentHostProjection.ts";
import { normalizeWorkspaceId } from "./workspaceAgentActivityDiagnostics.ts";

interface WorkspaceAgentActivityMutationCommandTarget {
  adapter: AgentActivityAdapter;
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
    const session = await target.adapter.createSession(input);
    reportAgentSubmitTraceDiagnostic(this.dependencies.runtimeApi, {
      agentSessionId: session.agentSessionId,
      clientSubmitId: input.clientSubmitId,
      event: "activity_service.create.adapter_resolved",
      provider: session.provider,
      submitDiagnostics: input.submitDiagnostics,
      workspaceId: input.workspaceId,
      fields: { activeTurnPhase: session.activeTurn?.phase ?? null }
    });
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
    const workspaceId = normalizeWorkspaceId(input.workspaceId);
    return input.signal
      ? this.dependencies.tuttidClient.cancelWorkspaceAgentTurn(
          workspaceId,
          input.agentSessionId,
          input.turnId,
          { signal: input.signal }
        )
      : this.dependencies.tuttidClient.cancelWorkspaceAgentTurn(
          workspaceId,
          input.agentSessionId,
          input.turnId
        );
  }

  async goalControl(
    input: Parameters<AgentActivityAdapter["goalControl"]>[0]
  ): Promise<AgentActivityGoalControlResult> {
    const target = this.dependencies.sessionCommandTarget(input.workspaceId);
    const result = await target.adapter.goalControl(input);
    this.dependencies.upsertAuthoritativeSession(
      result.session,
      "goal_control_result"
    );
    return result;
  }

  async submitInteractive(
    input: Parameters<AgentActivityAdapter["submitInteractive"]>[0]
  ): ReturnType<IWorkspaceAgentActivityService["submitInteractive"]> {
    return this.dependencies
      .sessionCommandTarget(input.workspaceId)
      .adapter.submitInteractive(input);
  }

  async submitPlanDecision(
    input: Parameters<IWorkspaceAgentActivityService["submitPlanDecision"]>[0]
  ) {
    return this.dependencies.tuttidClient.submitWorkspaceAgentPlanDecision(
      input.workspaceId,
      input.agentSessionId,
      input.turnId,
      input.requestId,
      {
        action: input.action,
        idempotencyKey: input.idempotencyKey,
        promptKind: input.promptKind
      }
    );
  }

  async updateSessionSettings(input: {
    agentSessionId: string;
    signal?: AbortSignal;
    settings: Parameters<typeof normalizeComposerSettings>[0];
    workspaceId: string;
  }): ReturnType<IWorkspaceAgentActivityService["updateSessionSettings"]> {
    const normalizedSettings = normalizeComposerSettings(input.settings);
    const settingsInput =
      tuttiAgentSessionComposerSettingsFromActivity(normalizedSettings);
    const session = input.signal
      ? await this.dependencies.tuttidClient.updateWorkspaceAgentSessionSettings(
          input.workspaceId,
          input.agentSessionId,
          settingsInput,
          { signal: input.signal }
        )
      : await this.dependencies.tuttidClient.updateWorkspaceAgentSessionSettings(
          input.workspaceId,
          input.agentSessionId,
          settingsInput
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
    input: Parameters<AgentActivityRuntime["updateTuttiModeActivation"]>[0]
  ): ReturnType<AgentActivityRuntime["updateTuttiModeActivation"]> {
    return this.dependencies
      .sessionCommandTarget(input.workspaceId)
      .adapter.updateTuttiModeActivation(input);
  }

  unactivateSession(
    input: Parameters<AgentActivityRuntime["unactivateSession"]>[0]
  ): ReturnType<IWorkspaceAgentActivityService["unactivateSession"]> {
    return Promise.resolve({
      agentSessionId: input.agentSessionId,
      buffered: false
    });
  }
}
