import type { ReactNode } from "react";

/**
 * Product-neutral context for the primary Composer capability placement.
 * AgentGUI owns exact target identity and draft mutation; an embedding host
 * decides which capability package, if any, renders the slot.
 */
export interface AgentGUIPrimaryCapabilitySlotContext {
  readonly disabled: boolean;
  readonly target: {
    readonly agentTargetId: string;
    readonly ownership: "self" | "shared";
  };
  readonly draft: {
    readonly selectedCapabilities: readonly AgentGUIPrimaryCapabilitySelection[];
    setSelected(
      capability: AgentGUIPrimaryCapabilitySelection,
      selected: boolean,
    ): void;
  };
}

export interface AgentGUIPrimaryCapabilitySelection {
  readonly id: string;
  readonly payload: Readonly<Record<string, unknown>>;
}

export type AgentGUIPrimaryCapabilityRenderer = (
  context: AgentGUIPrimaryCapabilitySlotContext,
) => ReactNode;
