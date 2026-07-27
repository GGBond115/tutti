import type {
  AgentActivityDurableMessage,
  AgentActivityTurn
} from "../types.ts";
import type { AgentActivitySessionMessageWindow } from "../messageWindow.types.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";

export type SessionReconcileScope = "messages" | "state" | "state_and_messages";

export interface SessionReconcileRecord {
  agentSessionId: string;
  errorCode: string | null;
  errorMessage: string | null;
  inFlightCommandId: string | null;
  inFlightScope: SessionReconcileScope | null;
  messagesHydrated: boolean;
  pendingMessages: boolean;
  pendingState: boolean;
  workspaceId: string;
}

export interface SessionReconcileState {
  nextCommandSequence: number;
  recordsBySessionId: Readonly<Record<string, SessionReconcileRecord>>;
}

export interface SessionReconcileRequestedIntent {
  type: "session/reconcileRequested";
  agentSessionId: string;
  needsMessages: boolean;
  needsState: boolean;
  workspaceId: string;
}

export interface SessionActivityObservedIntent {
  type: "session/activityObserved";
  agentSessionId: string;
  eventType: string;
  hasCachedSession: boolean;
  hasInlineMessages: boolean;
  inlineApplied: boolean;
  workspaceId: string;
}

export interface SessionDetailSnapshotReceivedIntent {
  type: "session/detailSnapshotReceived";
  childSessions: readonly AgentActivitySessionInput[];
  live?: boolean;
  messages?: readonly AgentActivityDurableMessage[];
  session: AgentActivitySessionInput;
  sessionMessageWindows?: readonly (AgentActivitySessionMessageWindow & {
    agentSessionId: string;
  })[];
  turns: readonly AgentActivityTurn[];
  workspaceId: string;
}

export type SessionReconcileIntent =
  | SessionActivityObservedIntent
  | SessionDetailSnapshotReceivedIntent
  | SessionReconcileRequestedIntent;

export interface SessionReconcileCommand {
  type: "session/reconcile";
  agentSessionId: string;
  commandId: string;
  scope: SessionReconcileScope;
  timeoutMs?: number;
  workspaceId: string;
}
