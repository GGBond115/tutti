import {
  selectRootAgentActivitySessions,
  selectRootAgentSessionIdsWithPendingInteractions,
  selectSessionMutations,
  type AgentActivitySnapshot,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import { projectCanonicalAgentGUIConversationSummariesFromState } from "@tutti-os/agent-gui/conversation-rail-projection";
import type { AgentTarget } from "@tutti-os/client-tuttid-ts";
import { projectWorkspaceConversationRail } from "./workspaceConversationRailProjection";
import type { WorkspaceConversationRailSnapshot } from "./workspaceConversationRailService";
import type { WorkspaceActivitySnapshot } from "./workspaceActivityTypes";
import type { WorkspaceNavigationSnapshot } from "./workspaceNavigationService";

export function projectWorkspaceActivitySnapshot({
  activity,
  ambiguousSubmission,
  draft,
  errorCode,
  loading,
  navigation,
  rail,
  state,
  targets,
  workspaceId
}: {
  activity: AgentActivitySnapshot;
  ambiguousSubmission: boolean;
  draft: string;
  errorCode: "request_failed" | null;
  loading: boolean;
  navigation: WorkspaceNavigationSnapshot;
  rail: WorkspaceConversationRailSnapshot;
  state: ReturnType<AgentSessionEngine["getSnapshot"]>;
  targets: readonly AgentTarget[];
  workspaceId: string;
}): WorkspaceActivitySnapshot {
  const sessions = selectRootAgentActivitySessions(activity).filter(
    (session) => session.visible
  );
  const selectedSession =
    sessions.find(
      (session) => session.agentSessionId === navigation.selectedAgentSessionId
    ) ?? null;
  const sending =
    Object.values(state.pendingIntents.submitsByClientSubmitId).some(
      (record) =>
        record.agentSessionId === navigation.selectedAgentSessionId &&
        (record.status === "requested" || record.status === "accepted")
    ) ||
    Object.values(state.pendingIntents.activationsByRequestId).some(
      (record) =>
        (navigation.creating ||
          record.agentSessionId === navigation.selectedAgentSessionId) &&
        (record.status === "requested" || record.status === "confirmed")
    );
  const pinningSessionIds = selectSessionMutations(state).flatMap((mutation) =>
    mutation.kind === "pin" && mutation.status === "inFlight"
      ? mutation.agentSessionIds
      : []
  );
  const conversations = projectCanonicalAgentGUIConversationSummariesFromState(
    state,
    {
      rootSessionIdsAwaitingUserAction: new Set(
        selectRootAgentSessionIdsWithPendingInteractions(state)
      ),
      workspaceId
    }
  );

  return {
    activity,
    ambiguousSubmission,
    creating: navigation.creating,
    draft,
    errorCode: errorCode ?? rail.errorCode,
    loading,
    pinningSessionIds,
    railErrorCode: rail.errorCode,
    railSections: projectWorkspaceConversationRail({
      conversations,
      loadingMoreSectionId: rail.loadingMoreSectionId,
      memberships: rail.sections
    }),
    railStatus: rail.status,
    selectedAgentSessionId: navigation.selectedAgentSessionId,
    selectedAgentTargetId: navigation.selectedAgentTargetId,
    selectedSession,
    sending,
    targets
  };
}
