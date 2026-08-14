import { act, renderHook } from "@testing-library/react";
import {
  createAgentSessionEngine,
  normalizeAgentActivitySession,
  selectEngineSessionSettingsUpdate
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import { createTestEngineCommandPort } from "../../../shared/testing/createTestAgentSessionEngine";
import { useAgentGUISessionEngineState } from "./useAgentGUISessionEngineState";

describe("useAgentGUISessionEngineState", () => {
  it.each(["requested", "uncertain"] as const)(
    "does not present a reconcile miss while a new activation is %s",
    (activationStatus) => {
      const sessionEngine = createAgentSessionEngine({
        clock: { nowUnixMs: () => 1 },
        commandPort: createTestEngineCommandPort({
          execute: vi.fn(() => new Promise(() => undefined))
        }),
        identity: { origin: "test", workspaceId: "workspace-1" },
        scheduler: { schedule: () => ({ cancel() {} }) }
      });
      sessionEngine.dispatch({
        agentSessionId: "session-new",
        agentTargetId: "personal-agent:codex",
        clientSubmitId: "submit-1",
        content: [{ type: "text", text: "hello" }],
        cwd: "/workspace",
        expiresAtUnixMs: 45_001,
        mode: "new",
        requestedAtUnixMs: 1,
        requestId: "activation-1",
        type: "activation/requested",
        workspaceId: "workspace-1"
      });
      if (activationStatus === "uncertain") {
        sessionEngine.dispatch({
          commandId: "activate:activation-1",
          commandType: "session/activate",
          correlationId: "activation-1",
          outcome: "timedOut",
          type: "engine/commandResult"
        });
      }
      sessionEngine.dispatch({
        agentSessionId: "session-new",
        needsMessages: true,
        needsState: true,
        type: "session/reconcileRequested",
        workspaceId: "workspace-1"
      });
      sessionEngine.dispatch({
        commandId: "session:reconcile:session-new:1",
        commandType: "session/reconcile",
        errorCode: "session.not_found",
        errorMessage: "session not found",
        outcome: "failed",
        type: "engine/commandResult"
      });

      const rendered = renderHook(() =>
        useAgentGUISessionEngineState({
          activeConversationId: "session-new",
          sessionEngine
        })
      );

      expect(rendered.result.current.isCreatingConversation).toBe(true);
      expect(
        rendered.result.current.activeSessionReconcileErrorCode
      ).toBeNull();
      expect(rendered.result.current.activeSessionReconcileError).toBeNull();
    }
  );

  it("still presents a reconcile miss for an ordinary existing session", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: vi.fn(() => new Promise(() => undefined))
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    sessionEngine.dispatch({
      agentSessionId: "session-existing",
      needsMessages: true,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: "workspace-1"
    });
    sessionEngine.dispatch({
      commandId: "session:reconcile:session-existing:1",
      commandType: "session/reconcile",
      errorCode: "session.not_found",
      errorMessage: "session not found",
      outcome: "failed",
      type: "engine/commandResult"
    });

    const rendered = renderHook(() =>
      useAgentGUISessionEngineState({
        activeConversationId: "session-existing",
        sessionEngine
      })
    );

    expect(rendered.result.current.activeSessionReconcileErrorCode).toBe(
      "session.not_found"
    );
    expect(rendered.result.current.activeSessionReconcileError).toBe(
      "session not found"
    );
  });

  it("shows an optimistic session selection then silently restores canonical settings on failure", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: vi.fn(() => new Promise(() => undefined))
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    sessionEngine.dispatch({
      type: "session/snapshotReceived",
      sessions: [
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentSessionId: "session-1",
          agentTargetId: "local:opencode",
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "opencode",
          settings: { permissionModeId: "ask" },
          title: "Session 1",
          workspaceId: "workspace-1"
        })
      ]
    });
    const rendered = renderHook(() =>
      useAgentGUISessionEngineState({
        activeConversationId: "session-1",
        sessionEngine
      })
    );

    act(() => {
      sessionEngine.dispatch({
        agentSessionId: "session-1",
        commandId: "settings-1",
        settings: { permissionModeId: "full-access" },
        type: "session/settingsUpdateRequested",
        workspaceId: "workspace-1"
      });
    });
    expect(
      rendered.result.current.activeCanonicalComposerSettings.permissionModeId
    ).toBe("full-access");

    act(() => {
      sessionEngine.dispatch({
        commandId: "settings-1",
        commandType: "session/updateSettings",
        correlationId: "session-1",
        errorCode: "settings_require_new_session",
        errorMessage: "requires a new session",
        outcome: "failed",
        type: "engine/commandResult"
      });
    });
    expect(
      rendered.result.current.activeCanonicalComposerSettings.permissionModeId
    ).toBe("ask");
    expect(rendered.result.current.activeEngineError).toBeNull();
  });

  it("keeps the latest queued settings visible while the session runtime reconnects", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: vi.fn(() => new Promise(() => undefined))
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    const session = (settings: { model: string; planMode: boolean }) =>
      normalizeAgentActivitySession({
        activeTurnId: null,
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        settings,
        title: "Session 1",
        workspaceId: "workspace-1"
      });
    sessionEngine.dispatch({
      type: "session/snapshotReceived",
      sessions: [session({ model: "model-canonical", planMode: false })]
    });
    const rendered = renderHook(() =>
      useAgentGUISessionEngineState({
        activeConversationId: "session-1",
        sessionEngine
      })
    );

    act(() => {
      sessionEngine.dispatch({
        agentSessionId: "session-1",
        commandId: "settings-1",
        settings: { model: "model-first" },
        type: "session/settingsUpdateRequested",
        workspaceId: "workspace-1"
      });
      sessionEngine.dispatch({
        agentSessionId: "session-1",
        commandId: "settings-2",
        settings: { model: "model-latest", planMode: true },
        type: "session/settingsUpdateRequested",
        workspaceId: "workspace-1"
      });
      sessionEngine.dispatch({
        type: "session/runtimeAvailabilityChanged",
        agentSessionId: "session-1",
        availability: {
          state: "blocked",
          reason: "transport_reconnecting"
        }
      });
      sessionEngine.dispatch({
        commandId: "settings-1",
        commandType: "session/updateSettings",
        correlationId: "session-1",
        outcome: "succeeded",
        type: "engine/commandResult",
        value: {
          agentSessionId: "session-1",
          session: session({ model: "model-first", planMode: false })
        }
      });
    });

    expect(
      selectEngineSessionSettingsUpdate(
        sessionEngine.getSnapshot(),
        "session-1"
      )?.status
    ).toBe("waitingForRuntime");
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });

    act(() => {
      sessionEngine.dispatch({
        type: "session/runtimeAvailabilityChanged",
        agentSessionId: "session-1",
        availability: { state: "available" }
      });
    });
    expect(
      selectEngineSessionSettingsUpdate(
        sessionEngine.getSnapshot(),
        "session-1"
      )?.status
    ).toBe("inFlight");
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });

    act(() => {
      sessionEngine.dispatch({
        commandId: "settings-2",
        commandType: "session/updateSettings",
        correlationId: "session-1",
        outcome: "succeeded",
        type: "engine/commandResult",
        value: {
          agentSessionId: "session-1",
          session: session({ model: "model-latest", planMode: true })
        }
      });
    });
    expect(
      selectEngineSessionSettingsUpdate(
        sessionEngine.getSnapshot(),
        "session-1"
      )?.status
    ).toBe("idle");
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });
  });

  it("observes runtime availability for the selected session only", () => {
    const sessionEngine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: vi.fn(() => new Promise(() => undefined))
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    sessionEngine.dispatch({
      type: "session/snapshotReceived",
      sessions: ["session-1", "session-2"].map((agentSessionId) =>
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentSessionId,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Session 1",
          workspaceId: "workspace-1"
        })
      )
    });
    const rendered = renderHook(
      ({ activeConversationId }) =>
        useAgentGUISessionEngineState({
          activeConversationId,
          sessionEngine
        }),
      { initialProps: { activeConversationId: "session-1" } }
    );

    act(() => {
      sessionEngine.dispatch({
        type: "session/runtimeAvailabilityChanged",
        agentSessionId: "session-1",
        availability: {
          state: "blocked",
          reason: "transport_reconnecting"
        }
      });
    });
    expect(rendered.result.current.activeEngineRuntimeAvailability).toEqual({
      state: "blocked",
      reason: "transport_reconnecting"
    });

    rendered.rerender({ activeConversationId: "session-2" });
    expect(rendered.result.current.activeEngineRuntimeAvailability).toEqual({
      state: "available"
    });
  });
});
