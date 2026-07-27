import {
  analyzeAgentActivityEventObservation,
  createAgentActivityOptimisticMessageOverlay,
  parseAgentActivityMessageDeltaEvent,
  type AgentActivityLiveEvent,
  type AgentActivitySnapshot,
  type AgentActivityTurn,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type {
  AgentLiveAttachmentControl,
  AgentLiveDelivery,
  ClockPort,
  DeviceLinkPort
} from "./servicePorts";
import type { WorkspaceConversationRailService } from "./workspaceConversationRailService";
import type { WorkspaceNavigationService } from "./workspaceNavigationService";

const AGENT_LIVE_RETRY_MS = 1_000;
const RAIL_RECONCILE_DELAY_MS = 250;

interface WorkspaceAgentLiveLaneOptions {
  clock: ClockPort;
  deviceLink?: DeviceLinkPort;
  engine: AgentSessionEngine;
  isAvailable(): boolean;
  loadSelectedMessages(authoritative: boolean): Promise<void>;
  navigation: WorkspaceNavigationService;
  onActivityChanged(): void;
  onConnectionChanged(connected: boolean): void;
  rail: WorkspaceConversationRailService;
  readCanonicalActivity(): AgentActivitySnapshot;
  reconcileWorkspace(): Promise<unknown>;
  workspaceId: string;
}

export class WorkspaceAgentLiveLane {
  private readonly optimisticMessages =
    createAgentActivityOptimisticMessageOverlay();
  private active = false;
  private connected = false;
  private retryTask: { cancel(): void } | null = null;
  private railReconcileTask: { cancel(): void } | null = null;
  private subscription: { close(): void } | null = null;
  // Mobile currently opens every native stream from epoch/sequence zero, so
  // this projection has the same lifetime as the subscription. A future
  // persisted resume cursor must persist this fence with it.
  private attachmentFence: {
    attachment: AgentLiveAttachmentControl;
    caughtUp: boolean;
  } | null = null;

  constructor(private readonly options: WorkspaceAgentLiveLaneOptions) {}

  start(): void {
    this.active = true;
    if (!this.options.deviceLink || this.subscription) return;
    this.retryTask?.cancel();
    this.retryTask = null;
    this.attachmentFence = null;
    this.subscription = this.options.deviceLink.subscribeAgentLive(
      this.options.workspaceId,
      (delivery) => this.handleDelivery(delivery)
    );
  }

  stop(): void {
    this.active = false;
    this.retryTask?.cancel();
    this.retryTask = null;
    this.railReconcileTask?.cancel();
    this.railReconcileTask = null;
    this.subscription?.close();
    this.subscription = null;
    this.attachmentFence = null;
    this.setConnected(false);
  }

  isConnected(): boolean {
    return this.connected;
  }

  project(canonical: AgentActivitySnapshot): AgentActivitySnapshot {
    const selectedSessionId =
      this.options.navigation.getSnapshot().selectedAgentSessionId;
    const sessionIds = new Set([
      ...Object.keys(canonical.sessionMessagesById),
      ...(selectedSessionId ? [selectedSessionId] : [])
    ]);
    if (sessionIds.size === 0) return canonical;
    const sessionMessagesById = { ...canonical.sessionMessagesById };
    for (const agentSessionId of sessionIds) {
      sessionMessagesById[agentSessionId] = this.optimisticMessages.project(
        { workspaceId: this.options.workspaceId, agentSessionId },
        canonical.sessionMessagesById[agentSessionId] ?? []
      );
    }
    return { ...canonical, sessionMessagesById };
  }

  reconcileMessages(agentSessionId: string): void {
    const canonical =
      this.options.readCanonicalActivity().sessionMessagesById[
        agentSessionId
      ] ?? [];
    this.optimisticMessages.reconcile(
      { workspaceId: this.options.workspaceId, agentSessionId },
      canonical
    );
  }

  private handleDelivery(delivery: AgentLiveDelivery): void {
    if (!this.active || !this.options.isAvailable()) return;
    if (delivery.kind === "connection") {
      if (delivery.status === "connected") {
        this.setConnected(true);
        void this.options.reconcileWorkspace().catch(() => undefined);
        void this.options.loadSelectedMessages(true);
        return;
      }
      this.subscription?.close();
      this.subscription = null;
      this.attachmentFence = null;
      this.setConnected(false);
      this.scheduleRetry();
      return;
    }
    if (delivery.kind === "discontinuity") {
      this.reconcileDiscontinuity(delivery);
      return;
    }
    if (delivery.kind === "attachment_changed") {
      this.handleAttachmentChanged(delivery.attachment);
      return;
    }
    if (delivery.kind === "attachment_caught_up") {
      this.handleAttachmentCaughtUp(delivery.attachment);
      return;
    }
    this.applyEvent(delivery.event);
  }

  private handleAttachmentChanged(
    attachment: AgentLiveAttachmentControl
  ): void {
    if (attachment.workspaceId !== this.options.workspaceId) {
      this.rejectAttachmentFence("attachment_identity_mismatch");
      return;
    }
    if (this.attachmentFence && !this.attachmentFence.caughtUp) {
      this.rejectAttachmentFence("attachment_changed_before_catch_up");
      return;
    }
    this.attachmentFence = { attachment, caughtUp: false };
    this.reconcileDiscontinuity({
      kind: "discontinuity",
      reason: "attachment_changed",
      reconcileKeys: []
    });
  }

  private handleAttachmentCaughtUp(
    attachment: AgentLiveAttachmentControl
  ): void {
    const current = this.attachmentFence;
    if (!current || !sameAttachmentControl(current.attachment, attachment)) {
      this.rejectAttachmentFence("attachment_catch_up_mismatch");
      return;
    }
    this.attachmentFence = {
      attachment: current.attachment,
      caughtUp: true
    };
  }

  private rejectAttachmentFence(reason: string): void {
    this.attachmentFence = null;
    this.subscription?.close();
    this.subscription = null;
    this.setConnected(false);
    this.reconcileDiscontinuity({
      kind: "discontinuity",
      reason,
      reconcileKeys: []
    });
    this.scheduleRetry();
  }

  private applyEvent(event: AgentActivityLiveEvent): void {
    if (
      event.workspaceId !== this.options.workspaceId ||
      !event.agentSessionId.trim()
    ) {
      this.reconcileDiscontinuity({
        kind: "discontinuity",
        reason: "identity_mismatch",
        reconcileKeys: []
      });
      return;
    }
    switch (event.eventType) {
      case "message_delta": {
        const parsed = parseAgentActivityMessageDeltaEvent(event);
        if (!parsed) {
          this.requestSessionReconcile(event.agentSessionId, true);
          return;
        }
        const applied = this.optimisticMessages.apply(parsed);
        if (applied.applied) this.options.onActivityChanged();
        if (applied.needsReconcile) {
          this.requestSessionReconcile(event.agentSessionId, true);
        }
        return;
      }
      case "turn_update":
        this.options.engine.dispatch({
          turn: agentActivityTurnFromLiveEvent(event),
          type: "turn/upserted"
        });
        this.observeAuthoritativeEvent(event);
        this.scheduleRailReconcile();
        return;
      case "interaction_update":
        this.options.engine.dispatch({
          interaction: event.data.interaction,
          type: "interaction/upserted"
        });
        this.observeAuthoritativeEvent(event);
        this.scheduleRailReconcile();
        return;
      case "session_audit": {
        this.observeAuthoritativeEvent(event);
        this.scheduleRailReconcile();
        return;
      }
    }
  }

  private observeAuthoritativeEvent(
    event: Exclude<AgentActivityLiveEvent, { eventType: "message_delta" }>
  ): void {
    const activity = this.options.readCanonicalActivity();
    const cachedMessages =
      activity.sessionMessagesById[event.agentSessionId] ?? [];
    const observation = analyzeAgentActivityEventObservation({
      cachedMessages,
      event,
      hasCachedSession: activity.sessions.some(
        (session) => session.agentSessionId === event.agentSessionId
      )
    });
    if (observation.canApplyInlineMessages) {
      this.options.engine.dispatch(
        {
          messages: observation.inlineMessages,
          type: "message/snapshotReceived",
          workspaceId: this.options.workspaceId
        },
        { batch: true }
      );
      this.optimisticMessages.reconcile(
        {
          agentSessionId: event.agentSessionId,
          workspaceId: this.options.workspaceId
        },
        [...cachedMessages, ...observation.inlineMessages]
      );
    }
    this.options.engine.dispatch(observation.intent);
  }

  private reconcileDiscontinuity(
    delivery: Extract<AgentLiveDelivery, { kind: "discontinuity" }>
  ): void {
    const sessionIds = new Set(
      delivery.reconcileKeys
        .filter((key) => key.workspaceId === this.options.workspaceId)
        .map((key) => key.agentSessionId?.trim())
        .filter((value): value is string => Boolean(value))
    );
    if (sessionIds.size === 0) {
      const selected = this.options.navigation
        .getSnapshot()
        .selectedAgentSessionId?.trim();
      if (selected) sessionIds.add(selected);
      void this.options.reconcileWorkspace().catch(() => undefined);
    }
    for (const agentSessionId of sessionIds) {
      this.requestSessionReconcile(agentSessionId, true);
    }
    this.scheduleRailReconcile();
  }

  private requestSessionReconcile(
    agentSessionId: string,
    needsMessages: boolean
  ): void {
    this.options.engine.dispatch({
      agentSessionId,
      needsMessages,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: this.options.workspaceId
    });
  }

  private scheduleRetry(): void {
    this.retryTask?.cancel();
    if (!this.active || this.subscription || !this.options.isAvailable())
      return;
    this.retryTask = this.options.clock.schedule(AGENT_LIVE_RETRY_MS, () => {
      this.retryTask = null;
      this.start();
    });
  }

  private scheduleRailReconcile(): void {
    if (this.railReconcileTask || !this.active || !this.options.isAvailable()) {
      return;
    }
    this.railReconcileTask = this.options.clock.schedule(
      RAIL_RECONCILE_DELAY_MS,
      () => {
        this.railReconcileTask = null;
        void this.options.rail.reconcile().catch(() => undefined);
      }
    );
  }

  private setConnected(connected: boolean): void {
    if (this.connected === connected) return;
    this.connected = connected;
    this.options.rail.setLiveConnected(connected);
    this.options.onConnectionChanged(connected);
  }
}

function sameAttachmentControl(
  left: AgentLiveAttachmentControl,
  right: AgentLiveAttachmentControl
): boolean {
  return (
    left.bindingId === right.bindingId &&
    left.workspaceId === right.workspaceId &&
    left.agentSessionId === right.agentSessionId &&
    left.canonicalTurnId === right.canonicalTurnId &&
    left.callerTurnId === right.callerTurnId &&
    left.attachmentRevision === right.attachmentRevision
  );
}

function agentActivityTurnFromLiveEvent(
  event: Extract<AgentActivityLiveEvent, { eventType: "turn_update" }>
): AgentActivityTurn {
  return {
    ...event.data.turn,
    completedCommand: event.data.turn
      .completedCommand as AgentActivityTurn["completedCommand"],
    error: event.data.turn.error as AgentActivityTurn["error"],
    fileChanges: event.data.turn.fileChanges as AgentActivityTurn["fileChanges"]
  };
}
