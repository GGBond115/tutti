import {
  createAgentActivityOptimisticMessageOverlay,
  parseAgentActivityMessageDeltaEvent,
  type AgentActivityLiveEvent,
  type AgentActivitySnapshot,
  type AgentActivityTurn,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type {
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

  constructor(private readonly options: WorkspaceAgentLiveLaneOptions) {}

  start(): void {
    this.active = true;
    if (!this.options.deviceLink || this.subscription) return;
    this.retryTask?.cancel();
    this.retryTask = null;
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
      this.setConnected(false);
      this.scheduleRetry();
      return;
    }
    if (delivery.kind === "discontinuity") {
      this.reconcileDiscontinuity(delivery);
      return;
    }
    this.applyEvent(delivery.event);
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
        this.scheduleRailReconcile();
        if (event.data.turn.phase === "settled") {
          this.requestSessionReconcile(event.agentSessionId, true);
        }
        return;
      case "interaction_update":
        this.options.engine.dispatch({
          interaction: event.data.interaction,
          type: "interaction/upserted"
        });
        this.scheduleRailReconcile();
        return;
      case "session_audit":
        this.requestSessionReconcile(event.agentSessionId, true);
        this.scheduleRailReconcile();
    }
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
