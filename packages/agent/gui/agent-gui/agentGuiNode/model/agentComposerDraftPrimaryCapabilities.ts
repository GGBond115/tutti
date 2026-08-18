import type {
  AgentComposerDraft,
  AgentComposerDraftPrimaryCapability,
} from "./agentGuiNodeTypes";
import { updateAgentComposerDraft } from "./agentComposerDraft";

export function agentComposerDraftPrimaryCapabilities(
  draft: AgentComposerDraft,
): AgentComposerDraftPrimaryCapability[] {
  return draft.flatMap((block) => {
    if (block.type === "primary-capability") {
      return [{ id: block.id, payload: block.payload }];
    }
    if (
      block.type === "text" ||
      block.type === "image" ||
      block.type === "file"
    ) {
      return [];
    }
    const payload = { ...(block as unknown as Record<string, unknown>) };
    return typeof payload.type === "string"
      ? [{ id: opaquePrimaryCapabilityId(payload), payload }]
      : [];
  });
}

export function updateAgentComposerDraftPrimaryCapabilities(
  draft: AgentComposerDraft,
  capabilities: readonly AgentComposerDraftPrimaryCapability[],
): AgentComposerDraft {
  return updateAgentComposerDraft(draft, {
    primaryCapabilities: capabilities,
  });
}

export function opaquePrimaryCapabilityId(
  payload: Readonly<Record<string, unknown>>,
): string {
  return JSON.stringify(
    Object.entries(payload).sort(([left], [right]) =>
      left.localeCompare(right),
    ),
  );
}
