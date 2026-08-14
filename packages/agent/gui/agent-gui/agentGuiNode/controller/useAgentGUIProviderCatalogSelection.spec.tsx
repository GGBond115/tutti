import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  createLocalAgentGUIAgentTarget,
  resolveAgentGUISessionLaunchTarget
} from "../../../agentTargets";
import type { AgentGUIAgentTarget } from "../../../types";
import { useAgentGUIProviderCatalogSelection } from "./useAgentGUIProviderCatalogSelection";

describe("useAgentGUIProviderCatalogSelection handoff catalog", () => {
  it("uses an independent ready target catalog for handoff", () => {
    const local = target("local-codex", "codex");
    const shared = target("shared-agent:claude", "claude-code");
    const unavailable = {
      ...target("shared-agent:offline", "codex"),
      disabled: true
    };
    const { result } = renderHook(() =>
      useAgentGUIProviderCatalogSelection({
        agentTargets: [local],
        agentTargetsLoading: false,
        comingSoonProviders: undefined,
        data: {
          agentTargetId: local.agentTargetId,
          lastActiveAgentSessionId: null,
          provider: local.provider
        },
        defaultAgentTargetId: local.agentTargetId,
        handoffAgentTargets: [local, shared, unavailable],
        handoffAgentTargetsLoading: false,
        providerRailMode: "exact",
        providerReadinessGates: null
      })
    );

    expect(result.current.normalizedProviderTargets).toEqual([local]);
    expect(result.current.handoffAgentTargets).toEqual([local, shared]);
  });

  it("uses the runtime catalog when no independent handoff catalog is supplied", () => {
    const local = target("local-codex", "codex");
    const { result } = renderHook(() =>
      useAgentGUIProviderCatalogSelection({
        agentTargets: [local],
        agentTargetsLoading: false,
        comingSoonProviders: undefined,
        data: {
          agentTargetId: local.agentTargetId,
          lastActiveAgentSessionId: null,
          provider: local.provider
        },
        defaultAgentTargetId: local.agentTargetId,
        handoffAgentTargets: undefined,
        handoffAgentTargetsLoading: undefined,
        providerRailMode: "exact",
        providerReadinessGates: null
      })
    );

    expect(result.current.handoffAgentTargets).toEqual([local]);
  });
});

describe("useAgentGUIProviderCatalogSelection explicit target", () => {
  it("accepts a selected cloud target declared by an explicit local target", () => {
    const local = {
      ...createLocalAgentGUIAgentTarget("codex"),
      sessionLaunchMode: "local" as const,
      sessionLaunchTargets: [
        {
          mode: "cloud" as const,
          agentTargetId: "personal-agent:codex",
          availability: { status: "ready" as const },
          setupKind: null
        }
      ]
    };
    const cloud = resolveAgentGUISessionLaunchTarget({
      mode: "cloud",
      target: local
    });
    expect(cloud).not.toBeNull();

    const { result } = renderHook(() =>
      useAgentGUIProviderCatalogSelection({
        agentTargets: [local],
        agentTargetsLoading: false,
        comingSoonProviders: undefined,
        data: {
          agentTargetId: cloud!.agentTargetId,
          lastActiveAgentSessionId: null,
          provider: cloud!.provider
        },
        defaultAgentTargetId: local.agentTargetId,
        handoffAgentTargets: undefined,
        handoffAgentTargetsLoading: undefined,
        providerRailMode: "exact",
        providerReadinessGates: null
      })
    );

    expect(result.current.selectedAgentTarget).toEqual(cloud);
    expect(result.current.selectedAgentTargetIsExplicit).toBe(true);
    expect(result.current.selectedComposerTargetData.agentTargetId).toBe(
      "personal-agent:codex"
    );
  });
});

function target(agentTargetId: string, provider: string): AgentGUIAgentTarget {
  return {
    agentTargetId,
    label: agentTargetId,
    provider,
    ref: { agentTargetId, kind: "agent-directory", provider },
    targetId: agentTargetId
  };
}
