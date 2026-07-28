import type {
  AgentSideCapabilities,
  AgentSideConversationRuntime,
  AgentSideConversationSnapshot,
  AgentSideConversationState,
  AgentSideInteraction,
  AgentSideMessage
} from "./agentSideConversationRuntime";
import type { AgentSideUpdatedPayloadV1 } from "@tutti-os/event-protocol";

export type { AgentSideConversationRuntime } from "./agentSideConversationRuntime";

export interface AgentSideConversationTransport {
  resolveCapabilities(
    workspaceId: string,
    sourceAgentSessionId: string
  ): Promise<AgentSideCapabilities>;
  open(input: {
    workspaceId: string;
    sourceAgentSessionId: string;
    sideAgentSessionId: string;
    requestId: string;
  }): Promise<{ status: string }>;
  send(input: {
    workspaceId: string;
    sideAgentSessionId: string;
    turnId: string;
    clientSubmitId: string;
    content: Parameters<AgentSideConversationRuntime["send"]>[0]["content"];
    displayPrompt?: string;
  }): Promise<void>;
  cancel(input: {
    workspaceId: string;
    sideAgentSessionId: string;
    turnId: string;
  }): Promise<void>;
  respond(
    input: Parameters<AgentSideConversationRuntime["respond"]>[0]
  ): Promise<void>;
  close(input: {
    workspaceId: string;
    sideAgentSessionId: string;
  }): Promise<void>;
  subscribe(
    listener: (event: AgentSideConversationStreamEvent) => void
  ): () => void;
  subscribeConnectionState(
    listener: (
      state: "connected" | "connecting" | "disconnected" | "disposed"
    ) => void
  ): () => void;
  getConnectionState():
    | "connected"
    | "connecting"
    | "disconnected"
    | "disposed";
}

export type AgentSideConversationStreamEvent = AgentSideUpdatedPayloadV1;

function newId(): string {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  );
}

function eventText(data: unknown): string {
  if (!data || typeof data !== "object") return "";
  const value = data as Record<string, unknown>;
  if (typeof value.text === "string") return value.text;
  if (typeof value.content === "string") return value.content;
  if (typeof value.contentDelta === "string") return value.contentDelta;
  if (value.content && typeof value.content === "object") {
    const content = value.content as Record<string, unknown>;
    if (typeof content.text === "string") return content.text;
    if (typeof content.value === "string") return content.value;
  }
  if (value.payload && typeof value.payload === "object") {
    const payload = value.payload as Record<string, unknown>;
    if (typeof payload.text === "string") return payload.text;
    if (typeof payload.content === "string") return payload.content;
  }
  return "";
}

function eventTextOperation(data: unknown): "append" | "set" {
  if (!data || typeof data !== "object") return "set";
  const value = data as Record<string, unknown>;
  if (!value.content || typeof value.content !== "object") return "set";
  const operation = (value.content as Record<string, unknown>).operation;
  return operation === "append_text" ? "append" : "set";
}

function eventIdentity(data: unknown): {
  messageId: string;
  role: AgentSideMessage["role"];
  turnId: string | null;
} {
  const value =
    data && typeof data === "object" ? (data as Record<string, unknown>) : {};
  const role =
    value.role === "user" || value.role === "system" ? value.role : "assistant";
  return {
    messageId:
      typeof value.messageId === "string"
        ? value.messageId
        : typeof value.id === "string"
          ? value.id
          : `event-${newId()}`,
    role,
    turnId: typeof value.turnId === "string" ? value.turnId : null
  };
}

function interactionFromStatePatch(
  statePatch: Record<string, unknown> | null
): AgentSideInteraction | null | undefined {
  const raw = statePatch?.interactionTransition;
  if (!raw || typeof raw !== "object") return undefined;
  const transition = raw as Record<string, unknown>;
  const requestId =
    typeof transition.requestId === "string" ? transition.requestId : "";
  const turnId = typeof transition.turnId === "string" ? transition.turnId : "";
  if (!requestId || !turnId) return undefined;
  const status =
    typeof transition.status === "string"
      ? transition.status.trim().toLowerCase()
      : "";
  if (
    status === "answered" ||
    status === "superseded" ||
    status === "interrupted"
  ) {
    return null;
  }
  const metadata =
    transition.metadata && typeof transition.metadata === "object"
      ? (transition.metadata as Record<string, unknown>)
      : {};
  const rawActions = Array.isArray(metadata.actions) ? metadata.actions : [];
  const actions = rawActions.flatMap((rawAction) => {
    if (!rawAction || typeof rawAction !== "object") return [];
    const action = rawAction as Record<string, unknown>;
    const id = typeof action.id === "string" ? action.id : "";
    if (!id) return [];
    return [
      {
        id,
        label: typeof action.label === "string" ? action.label : id,
        semantic: typeof action.semantic === "string" ? action.semantic : ""
      }
    ];
  });
  const kind =
    transition.kind === "question"
      ? "question"
      : transition.kind === "plan"
        ? "plan"
        : "approval";
  return {
    requestId,
    turnId,
    kind,
    toolName:
      typeof transition.toolName === "string" ? transition.toolName : null,
    input:
      transition.input && typeof transition.input === "object"
        ? (transition.input as Record<string, unknown>)
        : {},
    actions
  };
}

export function createAgentSideConversationRuntime(
  transport: AgentSideConversationTransport
): AgentSideConversationRuntime & { dispose(): void } {
  const snapshots = new Map<string, AgentSideConversationSnapshot>();
  const pendingCloses = new Map<
    string,
    { workspaceId: string; sideAgentSessionId: string }
  >();
  const listeners = new Map<string, Set<() => void>>();
  const notify = (workspaceId: string) => {
    for (const listener of listeners.get(workspaceId) ?? []) listener();
  };
  const setActive = (
    workspaceId: string,
    active: AgentSideConversationState | null
  ) => {
    snapshots.set(workspaceId, { workspaceId, active });
    notify(workspaceId);
  };
  const closeWithTombstone = async (closeIdentity: {
    workspaceId: string;
    sideAgentSessionId: string;
  }) => {
    pendingCloses.set(closeIdentity.workspaceId, closeIdentity);
    await transport.close(closeIdentity);
    if (
      pendingCloses.get(closeIdentity.workspaceId)?.sideAgentSessionId ===
      closeIdentity.sideAgentSessionId
    ) {
      pendingCloses.delete(closeIdentity.workspaceId);
    }
  };
  const expireAndClose = (
    workspaceId: string,
    active: AgentSideConversationState
  ) => {
    const closeIdentity = {
      workspaceId,
      sideAgentSessionId: active.sideAgentSessionId
    };
    setActive(workspaceId, null);
    void closeWithTombstone(closeIdentity).catch(() => {});
  };
  const handleEvent = (event: AgentSideConversationStreamEvent) => {
    const snapshot = snapshots.get(event.workspaceId);
    const active = snapshot?.active;
    if (!active || active.sideAgentSessionId !== event.sideAgentSessionId) {
      return;
    }
    if (active.status === "expired") return;
    if (
      event.sourceAgentSessionId !== active.sourceAgentSessionId ||
      event.sequence > active.sequence + 1
    ) {
      expireAndClose(event.workspaceId, active);
      return;
    }
    if (event.sequence <= active.sequence) return;
    const identity = eventIdentity(event.data);
    const text = eventText(event.data);
    let messages = active.messages;
    if (
      text &&
      (event.eventType === "message_delta" ||
        event.eventType === "message_update")
    ) {
      let index = messages.findIndex(
        (message) => message.id === identity.messageId
      );
      if (index < 0 && identity.role === "user" && identity.turnId) {
        index = messages.findIndex(
          (message) =>
            message.role === "user" && message.turnId === identity.turnId
        );
      }
      if (index >= 0) {
        messages = messages.map((message, messageIndex) =>
          messageIndex === index
            ? {
                ...message,
                id: identity.messageId,
                text:
                  event.eventType === "message_delta" &&
                  identity.role !== "user" &&
                  eventTextOperation(event.data) === "append"
                    ? message.text + text
                    : text
              }
            : message
        );
      } else {
        messages = [
          ...messages,
          {
            id: identity.messageId,
            role: identity.role,
            text,
            turnId: identity.turnId
          }
        ];
      }
    }
    const statePatch =
      event.eventType === "state_patch" &&
      event.data &&
      typeof event.data === "object"
        ? (event.data as Record<string, unknown>)
        : null;
    const turnLifecycle =
      statePatch?.turnLifecycle && typeof statePatch.turnLifecycle === "object"
        ? (statePatch.turnLifecycle as Record<string, unknown>)
        : null;
    const interaction = interactionFromStatePatch(statePatch);
    const activeTurnId =
      typeof turnLifecycle?.activeTurnId === "string"
        ? turnLifecycle.activeTurnId
        : turnLifecycle?.activeTurnId === null
          ? null
          : active.activeTurnId;
    const lifecycleStatus =
      typeof statePatch?.lifecycleStatus === "string"
        ? statePatch.lifecycleStatus.trim().toLowerCase()
        : "";
    const currentPhase =
      typeof statePatch?.currentPhase === "string"
        ? statePatch.currentPhase.trim().toLowerCase()
        : "";
    const terminal =
      lifecycleStatus === "completed" ||
      lifecycleStatus === "failed" ||
      lifecycleStatus === "ended" ||
      statePatch?.status === "expired" ||
      statePatch?.status === "completed" ||
      statePatch?.status === "failed";
    const status = terminal
      ? ("expired" as const)
      : activeTurnId ||
          (currentPhase !== "" &&
            currentPhase !== "idle" &&
            currentPhase !== "settled")
        ? ("running" as const)
        : currentPhase === "idle" || currentPhase === "settled"
          ? ("idle" as const)
          : active.status;
    setActive(event.workspaceId, {
      ...active,
      activeTurnId: terminal ? null : activeTurnId,
      status,
      messages,
      pendingInteraction: terminal
        ? null
        : interaction === undefined
          ? active.pendingInteraction
          : interaction,
      sequence: event.sequence
    });
  };
  const handleConnectionState = (
    state: "connected" | "connecting" | "disconnected" | "disposed"
  ) => {
    if (state !== "disconnected" && state !== "disposed") return;
    for (const [workspaceId, snapshot] of snapshots) {
      if (!snapshot.active || snapshot.active.status === "expired") continue;
      expireAndClose(workspaceId, snapshot.active);
    }
  };
  let eventUnsubscribe: (() => void) | null = null;
  let connectionUnsubscribe: (() => void) | null = null;
  let connectionState = transport.getConnectionState();
  const ensureTransportSubscriptions = () => {
    if (eventUnsubscribe) return;
    connectionState = transport.getConnectionState();
    eventUnsubscribe = transport.subscribe(handleEvent);
    connectionUnsubscribe = transport.subscribeConnectionState((state) => {
      connectionState = state;
      handleConnectionState(state);
    });
    connectionState = transport.getConnectionState();
    handleConnectionState(connectionState);
  };
  const releaseTransportSubscriptionsIfUnused = () => {
    const hasListeners = [...listeners.values()].some(
      (workspaceListeners) => workspaceListeners.size > 0
    );
    const hasActiveSide = [...snapshots.values()].some(
      (snapshot) => snapshot.active !== null
    );
    if (hasListeners || hasActiveSide) return;
    eventUnsubscribe?.();
    connectionUnsubscribe?.();
    eventUnsubscribe = null;
    connectionUnsubscribe = null;
  };

  return {
    resolveCapabilities: ({ workspaceId, sourceAgentSessionId }) =>
      transport.resolveCapabilities(workspaceId, sourceAgentSessionId),
    async open({ workspaceId, sourceAgentSessionId }) {
      ensureTransportSubscriptions();
      connectionState = transport.getConnectionState();
      if (connectionState !== "connected") {
        releaseTransportSubscriptionsIfUnused();
        throw new Error("event_stream_unavailable");
      }
      if (snapshots.get(workspaceId)?.active) {
        throw new Error("A Side conversation is already active.");
      }
      const pendingClose = pendingCloses.get(workspaceId);
      if (pendingClose) {
        await transport.close(pendingClose);
        pendingCloses.delete(workspaceId);
      }
      connectionState = transport.getConnectionState();
      if (connectionState !== "connected") {
        releaseTransportSubscriptionsIfUnused();
        throw new Error("event_stream_unavailable");
      }
      const sideAgentSessionId = newId();
      const state: AgentSideConversationState = {
        workspaceId,
        sourceAgentSessionId,
        sideAgentSessionId,
        status: "opening",
        activeTurnId: null,
        messages: [],
        pendingInteraction: null,
        error: null,
        sequence: 0
      };
      setActive(workspaceId, state);
      try {
        await transport.open({
          workspaceId,
          sourceAgentSessionId,
          sideAgentSessionId,
          requestId: newId()
        });
        const current = snapshots.get(workspaceId)?.active;
        if (!current || current.sideAgentSessionId !== sideAgentSessionId) {
          await closeWithTombstone({ workspaceId, sideAgentSessionId });
          throw new Error("Side conversation identity changed while opening.");
        }
        const opened: AgentSideConversationState = {
          ...current,
          status:
            current.status === "opening"
              ? current.activeTurnId
                ? "running"
                : "idle"
              : current.status,
          error: current.status === "opening" ? null : current.error
        };
        setActive(workspaceId, opened);
        return opened;
      } catch (error) {
        const current = snapshots.get(workspaceId)?.active;
        if (
          current?.sideAgentSessionId === sideAgentSessionId &&
          current.status === "opening"
        ) {
          setActive(workspaceId, {
            ...current,
            status: "error",
            error: "side_open_failed"
          });
        }
        throw error;
      }
    },
    async send(input) {
      const turnId = newId();
      const snapshot = snapshots.get(input.workspaceId);
      const active = snapshot?.active;
      if (!active || active.sideAgentSessionId !== input.sideAgentSessionId) {
        throw new Error("Side conversation is not active.");
      }
      if (active.status !== "idle" || active.activeTurnId) {
        throw new Error("Side conversation is not ready for input.");
      }
      if (input.content.some((block) => block.type === "file")) {
        setActive(input.workspaceId, {
          ...active,
          error: "content_unsupported"
        });
        throw new Error("content_unsupported");
      }
      const text =
        input.displayPrompt ??
        input.content
          .filter((block) => block.type === "text")
          .map((block) => block.text ?? "")
          .join("");
      setActive(input.workspaceId, {
        ...active,
        status: "running",
        activeTurnId: turnId,
        messages: [
          ...active.messages,
          { id: `user-${turnId}`, role: "user", text, turnId }
        ]
      });
      try {
        await transport.send({
          ...input,
          turnId,
          clientSubmitId: newId()
        });
      } catch (error) {
        const current = snapshots.get(input.workspaceId)?.active;
        if (current?.sideAgentSessionId === input.sideAgentSessionId) {
          setActive(input.workspaceId, {
            ...current,
            status: "error",
            activeTurnId: null,
            error: "side_send_failed"
          });
        }
        throw error;
      }
    },
    async cancel(input) {
      const active = snapshots.get(input.workspaceId)?.active;
      if (
        !active ||
        active.sideAgentSessionId !== input.sideAgentSessionId ||
        active.activeTurnId !== input.turnId
      ) {
        throw new Error("Side turn is not active.");
      }
      await transport.cancel(input);
    },
    async respond(input) {
      const active = snapshots.get(input.workspaceId)?.active;
      if (
        !active ||
        active.sideAgentSessionId !== input.sideAgentSessionId ||
        active.pendingInteraction?.turnId !== input.turnId ||
        active.pendingInteraction.requestId !== input.requestId
      ) {
        throw new Error("Side interaction is not active.");
      }
      await transport.respond(input);
    },
    async close(input) {
      const active = snapshots.get(input.workspaceId)?.active;
      if (active?.sideAgentSessionId === input.sideAgentSessionId) {
        setActive(input.workspaceId, null);
      }
      releaseTransportSubscriptionsIfUnused();
      await closeWithTombstone(input);
    },
    getSnapshot(workspaceId) {
      return snapshots.get(workspaceId) ?? { workspaceId, active: null };
    },
    subscribe(workspaceId, listener) {
      ensureTransportSubscriptions();
      let workspaceListeners = listeners.get(workspaceId);
      if (!workspaceListeners) {
        workspaceListeners = new Set();
        listeners.set(workspaceId, workspaceListeners);
      }
      workspaceListeners.add(listener);
      return () => {
        workspaceListeners?.delete(listener);
        if (workspaceListeners?.size === 0) listeners.delete(workspaceId);
        releaseTransportSubscriptionsIfUnused();
      };
    },
    dispose() {
      eventUnsubscribe?.();
      connectionUnsubscribe?.();
      eventUnsubscribe = null;
      connectionUnsubscribe = null;
      for (const snapshot of snapshots.values()) {
        if (!snapshot.active) continue;
        void transport
          .close({
            workspaceId: snapshot.workspaceId,
            sideAgentSessionId: snapshot.active.sideAgentSessionId
          })
          .catch(() => {});
      }
      for (const pendingClose of pendingCloses.values()) {
        void transport.close(pendingClose).catch(() => {});
      }
      listeners.clear();
      snapshots.clear();
      pendingCloses.clear();
    }
  };
}
