import { useCallback } from "react";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import {
  agentComposerDraftPrimaryCapabilities,
  updateAgentComposerDraftPrimaryCapabilities,
} from "../model/agentComposerDraftPrimaryCapabilities";
import type { AgentGUIPrimaryCapabilitySelection } from "../view/AgentGUIPrimaryCapabilitySlot.types";

type UpdateScopedDraft = (
  sourceScopeKey: string,
  update: (current: AgentComposerDraft) => AgentComposerDraft,
) => AgentComposerDraft | null;

export function useComposerPrimaryCapabilitySelection(
  draftScopeKey: string,
  updateScopedDraft: UpdateScopedDraft,
) {
  const setPrimaryCapabilitySelected = useCallback(
    (
      capability: AgentGUIPrimaryCapabilitySelection,
      selected: boolean,
    ): void => {
      const normalizedId = capability.id.trim();
      if (!normalizedId || typeof capability.payload.type !== "string") {
        return;
      }
      updateScopedDraft(draftScopeKey, (currentDraft) => {
        const capabilities =
          agentComposerDraftPrimaryCapabilities(currentDraft);
        const alreadySelected = capabilities.some(
          (candidate) => candidate.id === normalizedId,
        );
        if (alreadySelected === selected) {
          return currentDraft;
        }
        return updateAgentComposerDraftPrimaryCapabilities(
          currentDraft,
          selected
            ? [
                ...capabilities,
                { id: normalizedId, payload: capability.payload },
              ]
            : capabilities.filter((candidate) => candidate.id !== normalizedId),
        );
      });
    },
    [draftScopeKey, updateScopedDraft],
  );

  return { setPrimaryCapabilitySelected };
}
