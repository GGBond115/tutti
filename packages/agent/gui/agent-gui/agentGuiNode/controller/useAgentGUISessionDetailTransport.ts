import {
  selectLatestActivationForSession,
  selectSessionMessages,
  selectSessionMessageWindow,
  type SessionReconcileScope,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { RefObject } from "react";
import { useCallback, useMemo } from "react";
import type { AgentActivityRuntime } from "../../../agentActivityRuntime";
import { useAgentSessionControllerState } from "../../../contexts/workspace/presentation/renderer/agentSessions/useAgentSessionControllerState";
import type { AgentGUINodeData } from "../../../types";
import {
  reportAgentGUIMessagePageDiagnostic,
  reportAgentGUIRuntimeError
} from "./agentGuiController.reporting";
import { useAgentConversationMessagePaging } from "./useAgentConversationMessagePaging";
import { useEngineSelector } from "../../../shared/engine/useEngineSelector";

export function useAgentGUISessionDetailTransport(input: {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  agentActivityRuntime: AgentActivityRuntime;
  agentActivityRuntimeOrigin: string;
  dataRef: RefObject<AgentGUINodeData>;
  isMountedRef: RefObject<boolean>;
  reloadSelectedConversationRef: RefObject<
    (
      agentSessionId: string,
      options: { reloadConversations: boolean; reloadDetail: boolean }
    ) => void
  >;
  sessionEngine: AgentSessionEngine;
  syncConversationListProjectionRef: RefObject<
    (agentSessionId?: string | null) => Promise<void>
  >;
  workspaceId: string;
}) {
  const {
    activeConversationId,
    activeConversationIdRef,
    agentActivityRuntime,
    agentActivityRuntimeOrigin,
    dataRef,
    isMountedRef,
    reloadSelectedConversationRef,
    sessionEngine,
    syncConversationListProjectionRef,
    workspaceId
  } = input;
  const sessionViewRef = useCallback(
    (agentSessionId: string | null | undefined) => ({
      workspaceId,
      agentSessionId,
      origin: agentActivityRuntimeOrigin
    }),
    [agentActivityRuntimeOrigin, workspaceId]
  );
  const activeCanonicalMessages = useEngineSelector(
    sessionEngine,
    (engineState) => selectSessionMessages(engineState, activeConversationId)
  );
  const activeCanonicalWindow = useEngineSelector(
    sessionEngine,
    (engineState) =>
      selectSessionMessageWindow(engineState, activeConversationId)
  );
  const state = useAgentSessionControllerState(
    sessionViewRef(activeConversationId),
    activeCanonicalMessages,
    activeCanonicalWindow
  );
  const resolveSessionMessages = useCallback(
    (agentSessionId: string | null | undefined) => {
      const normalized = agentSessionId?.trim() ?? "";
      return normalized
        ? selectSessionMessages(sessionEngine.getSnapshot(), normalized)
        : [];
    },
    [sessionEngine]
  );
  const { loadSessionState, refreshMessagesFromSnapshot } = useMemo(() => {
    const reconcileSession = (
      agentSessionId: string,
      scope: SessionReconcileScope
    ) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return;
      sessionEngine.dispatch({
        agentSessionId: normalized,
        needsMessages: scope !== "state",
        needsState: scope !== "messages",
        type: "session/reconcileRequested",
        workspaceId
      });
    };
    return {
      loadSessionState: (agentSessionId: string, _cause?: unknown) =>
        reconcileSession(agentSessionId, "state"),
      refreshMessagesFromSnapshot: (agentSessionId: string) =>
        reconcileSession(agentSessionId, "messages")
    };
  }, [sessionEngine, workspaceId]);
  const paging = useAgentConversationMessagePaging({
    diagnostics: {
      error: ({ agentSessionId, context, error, phase }) =>
        reportAgentGUIRuntimeError({
          agentSessionId,
          context,
          error,
          phase,
          provider: dataRef.current.provider,
          runtime: agentActivityRuntime,
          workspaceId
        }),
      page: ({ agentSessionId, details, event, level, messages }) =>
        reportAgentGUIMessagePageDiagnostic({
          agentSessionId,
          details,
          event,
          level,
          messages,
          runtime: agentActivityRuntime,
          workspaceId
        })
    },
    getActiveSessionId: () => activeConversationIdRef.current,
    isMounted: () => isMountedRef.current,
    onOlderPageLoadingChanged: (loading) =>
      state.setAgentSessionViewOlderMessagesLoading(
        sessionViewRef(activeConversationIdRef.current),
        loading
      ),
    reload: {
      getActivationStatus: (agentSessionId) =>
        selectLatestActivationForSession(
          sessionEngine.getSnapshot(),
          agentSessionId
        )?.status ?? null,
      syncConversationList: (agentSessionId) =>
        void syncConversationListProjectionRef.current(agentSessionId)
    },
    runtime: agentActivityRuntime,
    sessionEngine,
    workspaceId
  });
  reloadSelectedConversationRef.current = paging.reloadSelectedConversation;
  const markSelectedConversationDetailPending = useCallback(
    (agentSessionId: string) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return null;
      const ref = sessionViewRef(normalized);
      state.setAgentSessionViewError(ref, null);
      return normalized;
    },
    [sessionViewRef, state]
  );

  return {
    ...state,
    loadOlderConversationMessages: paging.loadOlderMessages,
    loadSelectedConversationMessages: paging.loadInitialMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    refreshMessagesFromSnapshot,
    reloadSelectedConversation: paging.reloadSelectedConversation,
    resolveSessionMessages,
    setActiveMessageSession: paging.setActiveSession,
    sessionViewRef
  };
}
