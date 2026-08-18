import {
  useComposerDraftAttachments,
  type UseComposerDraftAttachmentsInput
} from "./useComposerDraftAttachments";
import { useComposerPrimaryCapabilitySelection } from "./useComposerPrimaryCapabilitySelection";

export function useComposerDraftAttachmentsWithPrimaryCapability(
  input: UseComposerDraftAttachmentsInput
) {
  const attachments = useComposerDraftAttachments(input);
  const { _updateScopedDraft, ...publicAttachments } = attachments;
  const primaryCapabilitySelection = useComposerPrimaryCapabilitySelection(
    input.draftScopeKey,
    _updateScopedDraft
  );
  return { ...publicAttachments, ...primaryCapabilitySelection };
}
