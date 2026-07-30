import { act, renderHook } from "@testing-library/react";
import { createAgentSessionEngine } from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import type {
  AgentGUIObservationGap,
  AgentGUIObservationGapSource,
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState
} from "../../../types";
import { useAgentGUISessionPresentation } from "./useAgentGUISessionPresentation";

class FakeTargetConnectionSource implements AgentGUITargetConnectionSource {
  private readonly listeners = new Set<() => void>();
  private state: AgentGUITargetConnectionState = {
    status: "connecting",
    retryAttempt: 0
  };

  getConnectionState(): AgentGUITargetConnectionState {
    return this.state;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  set(state: AgentGUITargetConnectionState): void {
    this.state = state;
    for (const listener of this.listeners) listener();
  }
}

class FakeObservationGapSource implements AgentGUIObservationGapSource {
  private readonly listeners = new Set<() => void>();
  private gap: AgentGUIObservationGap | null = {
    startedAtUnixMs: 10_000
  };

  getObservationGap(): AgentGUIObservationGap | null {
    return this.gap;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  clear(): void {
    this.gap = null;
    for (const listener of this.listeners) listener();
  }
}

describe("useAgentGUISessionPresentation", () => {
  it("makes a shared Agent composer editable in the same snapshot that its target connects", () => {
    const targetConnectionSource = new FakeTargetConnectionSource();
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: { execute: vi.fn(() => new Promise(() => undefined)) },
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const input = {
      activeConversation: null,
      activeConversationId: null,
      activeEngineActiveTurn: null,
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: null,
      activeEngineSession: null,
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "inactive",
      activeMessages: [],
      activePendingActivation: null,
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: null,
      activationErrorCode: null,
      activationState: null,
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      optimisticGoalControl: null,
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      serverInteractivePrompt: null,
      sessionEngine,
      targetConnectionAgentTargetId: "shared-agent:shared-1",
      targetConnectionSource,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];
    const rendered = renderHook(
      ({ renderRevision }) => {
        void renderRevision;
        return useAgentGUISessionPresentation(input);
      },
      { initialProps: { renderRevision: 0 } }
    );

    expect(rendered.result.current.composerGate).toMatchObject({
      runtime: { status: "blocked", reason: "target_connection" },
      editor: { status: "blocked", reason: "runtime_blocked" },
      submission: { status: "blocked", reason: "runtime_blocked" }
    });

    act(() => {
      targetConnectionSource.set({ status: "connected", retryAttempt: 0 });
    });

    expect(rendered.result.current.composerGate).toMatchObject({
      runtime: { status: "ready", reason: null },
      editor: { status: "editable", reason: null },
      submission: { status: "ready", reason: null }
    });
    const readyGate = rendered.result.current.composerGate;

    rendered.rerender({ renderRevision: 1 });

    expect(rendered.result.current.composerGate).toBe(readyGate);
  });

  it("keeps recovery chrome and commands blocked until an exact observation gap clears", () => {
    const targetConnectionSource = new FakeTargetConnectionSource();
    targetConnectionSource.set({ status: "connected", retryAttempt: 0 });
    const observationGapSource = new FakeObservationGapSource();
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: { execute: vi.fn(() => new Promise(() => undefined)) },
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const input = {
      activeConversation: null,
      activeConversationId: "session-1",
      activeEngineActiveTurn: {
        agentSessionId: "session-1",
        origin: "user_prompt",
        phase: "running",
        startedAtUnixMs: 1,
        turnId: "turn-1",
        updatedAtUnixMs: 1
      },
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: null,
      activeEngineSession: {
        agentSessionId: "session-1",
        goal: null,
        resumable: true
      },
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "active",
      activeMessages: [],
      activePendingActivation: null,
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: null,
      activationErrorCode: null,
      activationState: "active",
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      observationGapSource,
      optimisticGoalControl: null,
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      serverInteractivePrompt: null,
      sessionEngine,
      targetConnectionAgentTargetId: "shared-agent:shared-1",
      targetConnectionSource,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];
    const rendered = renderHook(() => useAgentGUISessionPresentation(input));

    expect(rendered.result.current.sessionChrome.recovery).toMatchObject({
      kind: "transport-connecting",
      message: "Synchronizing the latest task progress…"
    });
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "blocked",
      reason: "target_connection"
    });

    act(() => observationGapSource.clear());

    expect(rendered.result.current.sessionChrome.recovery).toBeNull();
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "ready",
      reason: null
    });
  });
});
