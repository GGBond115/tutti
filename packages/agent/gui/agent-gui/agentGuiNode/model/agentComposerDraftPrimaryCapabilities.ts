import type {
  AgentComposerDraft,
  AgentComposerDraftPrimaryCapability
} from "./agentGuiNodeTypes";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import { updateAgentComposerDraft } from "./agentComposerDraft";

const STANDARD_PROMPT_BLOCK_TYPES = new Set([
  "text",
  "image",
  "file",
  "skill",
  "mention"
]);

export function agentComposerDraftPrimaryCapabilities(
  draft: AgentComposerDraft
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
  capabilities: readonly AgentComposerDraftPrimaryCapability[]
): AgentComposerDraft {
  return updateAgentComposerDraft(draft, {
    primaryCapabilities: capabilities
  });
}

export function opaquePrimaryCapabilityId(
  payload: Readonly<Record<string, unknown>>
): string {
  return JSON.stringify(
    Object.entries(payload).sort(([left], [right]) => left.localeCompare(right))
  );
}

export function agentPromptPrimaryCapabilities(
  content: readonly AgentPromptContentBlock[]
): AgentComposerDraftPrimaryCapability[] {
  return content.flatMap((block) =>
    STANDARD_PROMPT_BLOCK_TYPES.has(block.type)
      ? []
      : [
          {
            id: opaquePrimaryCapabilityId(
              block as unknown as Readonly<Record<string, unknown>>
            ),
            payload: { ...block }
          }
        ]
  );
}
