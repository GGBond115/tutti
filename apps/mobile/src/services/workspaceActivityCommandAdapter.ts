import type {
  AgentActivitySession,
  AgentSessionEngine,
  EngineExternalCommand,
  PromptQueueSendCommand,
  SessionActivateCommand
} from "@tutti-os/agent-activity-core";
import {
  agentActivityComposerOptionsFromTuttidResult,
  agentActivitySessionFromTuttidSession,
  agentActivityTurnFromTuttidTurn
} from "@tutti-os/agent-activity-tuttid-adapter";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { mobileLocale } from "../i18n";
import { toTuttidPromptContent } from "./workspaceActivityCommandSupport";

interface WorkspaceActivityCommandContext {
  client: TuttidClient;
  engine: AgentSessionEngine;
  loadComposerOptions(options?: { force?: boolean }): void;
  mapSession(
    session: Parameters<typeof agentActivitySessionFromTuttidSession>[1]
  ): AgentActivitySession;
  reconcileSession(agentSessionId: string): Promise<unknown>;
  reconcileWorkspace(): Promise<unknown>;
}

export function executeWorkspaceActivityCommand(
  context: WorkspaceActivityCommandContext,
  command: EngineExternalCommand,
  signal?: AbortSignal
): Promise<unknown> {
  switch (command.type) {
    case "engine/probe":
      return Promise.resolve({ ok: true });
    case "engine/reconcileWorkspace":
      return context.reconcileWorkspace();
    case "session/activate":
      return activateSession(context, command, signal);
    case "queue/sendPrompt":
      return sendPrompt(context, command);
    case "turn/cancel":
      return context.client
        .cancelWorkspaceAgentTurn(
          command.workspaceId,
          command.agentSessionId,
          command.turnId
        )
        .then((response) => ({
          ...response,
          ...(response.turn
            ? { turn: agentActivityTurnFromTuttidTurn(response.turn) }
            : {})
        }));
    case "interaction/respond":
      return context.client
        .submitWorkspaceAgentInteractive(
          command.workspaceId,
          command.agentSessionId,
          command.requestId,
          {
            action: command.action ?? null,
            optionId: command.optionId ?? null,
            payload: command.payload ?? null,
            turnId: command.turnId
          }
        )
        .then((session) => ({ session: context.mapSession(session) }));
    case "session/reconcile":
      return context.reconcileSession(command.agentSessionId);
    case "composerOptions/load":
      return context.client
        .getAgentProviderComposerOptions(
          command.provider as Parameters<
            TuttidClient["getAgentProviderComposerOptions"]
          >[0],
          {
            agentTargetId: command.targetKey,
            ...(command.cwd ? { cwd: command.cwd } : {}),
            locale: mobileLocale,
            workspaceId: command.workspaceId,
            settings: command.settings ?? {}
          },
          { signal }
        )
        .then((result) =>
          agentActivityComposerOptionsFromTuttidResult(command.provider, result)
        );
    case "session/updateSettings":
      return context.client
        .updateWorkspaceAgentSessionSettings(
          command.workspaceId,
          command.agentSessionId,
          command.settings
        )
        .then((session) => {
          const activitySession = context.mapSession(session);
          context.engine.dispatch({
            session: activitySession,
            type: "session/upserted"
          });
          const options = activitySession.agentTargetId
            ? context.engine.getSnapshot().composerOptions.optionsByTargetKey[
                activitySession.agentTargetId
              ]
            : null;
          if (options?.behavior.refreshModelOptionsAfterSettings === true) {
            context.loadComposerOptions({ force: true });
          }
          return { session: activitySession };
        });
    case "session/setPinned":
      return context.client
        .updateWorkspaceAgentSessionPin(
          command.workspaceId,
          command.agentSessionId,
          { pinned: command.pinned }
        )
        .then((session) => ({ session: context.mapSession(session) }));
    case "sessions/delete":
      return context.client
        .deleteWorkspaceAgentSessionsBatch(
          command.workspaceId,
          { sessionIds: [...command.agentSessionIds] },
          { signal }
        )
        .then((response) => ({
          cleanupFailedSessionIds: response.cleanupFailedSessionIds,
          removedMessages: response.removedMessages,
          removedSessionIds: response.removedSessionIds,
          removedSessions: response.removedSessions
        }));
    default:
      return Promise.reject(
        new Error(`unsupported mobile agent command: ${command.type}`)
      );
  }
}

async function activateSession(
  context: WorkspaceActivityCommandContext,
  command: SessionActivateCommand,
  signal?: AbortSignal
): Promise<unknown> {
  if (command.mode === "existing") {
    if (signal?.aborted) throw signal.reason;
    const detail = await context.client.getWorkspaceAgentSession(
      command.workspaceId,
      command.agentSessionId
    );
    const session = context.mapSession(detail.session);
    context.engine.dispatch({ session, type: "session/upserted" });
    return {
      activation: { mode: "existing", status: "already_attached" },
      session
    };
  }
  const session = await context.client.createWorkspaceAgentSession(
    command.workspaceId,
    {
      agentSessionId: command.agentSessionId,
      agentTargetId: command.agentTargetId,
      clientSubmitId: command.clientSubmitId,
      cwd: command.cwd ?? null,
      initialContent: toTuttidPromptContent(command.initialContent ?? []),
      initialDisplayPrompt: command.initialDisplayPrompt ?? null,
      ...(command.settings?.model ? { model: command.settings.model } : {}),
      ...(command.settings?.reasoningEffort
        ? { reasoningEffort: command.settings.reasoningEffort }
        : {}),
      ...(command.settings?.speed ? { speed: command.settings.speed } : {}),
      ...(command.settings?.permissionModeId
        ? { permissionModeId: command.settings.permissionModeId }
        : {}),
      ...(typeof command.settings?.planMode === "boolean"
        ? { planMode: command.settings.planMode }
        : {}),
      ...(typeof command.settings?.browserUse === "boolean"
        ? { browserUse: command.settings.browserUse }
        : {}),
      ...(typeof command.settings?.computerUse === "boolean"
        ? { computerUse: command.settings.computerUse }
        : {}),
      submitDiagnostics: command.submitDiagnostics,
      title: command.title ?? null,
      visible: command.visible ?? true
    },
    { signal }
  );
  const activitySession = context.mapSession(session);
  context.engine.dispatch({
    session: activitySession,
    type: "session/upserted"
  });
  return {
    activation: { mode: "new", status: "attached" },
    session: activitySession
  };
}

async function sendPrompt(
  context: WorkspaceActivityCommandContext,
  command: PromptQueueSendCommand
): Promise<unknown> {
  const result = await context.client.sendWorkspaceAgentSessionInput(
    command.workspaceId,
    command.agentSessionId,
    {
      clientSubmitId: command.clientSubmitId,
      content: toTuttidPromptContent(command.content),
      displayPrompt: command.displayPrompt ?? null,
      guidance: command.guidance ?? false,
      submitDiagnostics: command.submitDiagnostics
    }
  );
  if (result.kind === "goalControl") {
    return {
      kind: "goalControl",
      goal: result.goal ?? result.session.goal ?? null,
      session: context.mapSession(result.session)
    };
  }
  return {
    kind: "turn",
    session: context.mapSession(result.session),
    turn: agentActivityTurnFromTuttidTurn(result.turn),
    turnId: result.turnId
  };
}
