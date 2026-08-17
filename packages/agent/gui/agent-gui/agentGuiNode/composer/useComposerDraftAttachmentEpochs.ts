import { useCallback, useRef, type RefObject } from "react";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";

export function useStableEventCallback<Args extends unknown[], Result>(
  callback: (...args: Args) => Result
): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}

interface UseComposerDraftAttachmentEpochsInput {
  draftScopeKey: string;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
}

export function useComposerDraftAttachmentEpochs({
  draftScopeKey,
  draftByScopeKeyRef
}: UseComposerDraftAttachmentEpochsInput) {
  const activeDraftScopeKeyRef = useRef(draftScopeKey);
  activeDraftScopeKeyRef.current = draftScopeKey;
  const attachmentEpochByScopeKeyRef = useRef<
    Record<string, Record<string, symbol>>
  >({});
  const registerAttachmentEpoch = useStableEventCallback(
    (sourceScopeKey: string, attachmentKey: string): symbol => {
      const scopeEpochs =
        attachmentEpochByScopeKeyRef.current[sourceScopeKey] ?? {};
      const epoch = Symbol(attachmentKey);
      scopeEpochs[attachmentKey] = epoch;
      attachmentEpochByScopeKeyRef.current[sourceScopeKey] = scopeEpochs;
      return epoch;
    }
  );
  const invalidateAttachmentEpoch = useStableEventCallback(
    (sourceScopeKey: string, attachmentKey: string): void => {
      delete attachmentEpochByScopeKeyRef.current[sourceScopeKey]?.[
        attachmentKey
      ];
    }
  );
  const isCurrentAttachmentEpoch = useStableEventCallback(
    (sourceScopeKey: string, attachmentKey: string, epoch: symbol): boolean => {
      if (activeDraftScopeKeyRef.current !== sourceScopeKey) return false;
      const currentDraft = draftByScopeKeyRef.current[sourceScopeKey];
      return Boolean(
        currentDraft?.some(
          (block) =>
            (block.type === "image" || block.type === "file") &&
            block.id === attachmentKey
        ) &&
        attachmentEpochByScopeKeyRef.current[sourceScopeKey]?.[
          attachmentKey
        ] === epoch
      );
    }
  );
  const isActiveDraftScope = useStableEventCallback(
    (sourceScopeKey: string): boolean =>
      activeDraftScopeKeyRef.current === sourceScopeKey
  );
  const invalidateRemovedAttachmentEpochs = useStableEventCallback(
    (
      sourceScopeKey: string,
      currentDraft: AgentComposerDraft | undefined,
      nextDraft: AgentComposerDraft
    ): void => {
      if (!currentDraft) return;
      const nextAttachmentKeys = new Set(
        nextDraft
          .filter((block) => block.type === "image" || block.type === "file")
          .map((block) => block.id)
      );
      for (const block of currentDraft) {
        if (
          (block.type === "image" || block.type === "file") &&
          !nextAttachmentKeys.has(block.id)
        ) {
          invalidateAttachmentEpoch(sourceScopeKey, block.id);
        }
      }
    }
  );
  return {
    invalidateAttachmentEpoch,
    invalidateRemovedAttachmentEpochs,
    isActiveDraftScope,
    isCurrentAttachmentEpoch,
    registerAttachmentEpoch
  };
}
