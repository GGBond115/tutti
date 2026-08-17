import { flushSync } from "react-dom";
import { useCallback, type RefObject } from "react";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import type { AgentContextMentionItem } from "../agentRichText/agentFileMentionExtension";

export interface WorkspaceReferencePickResult {
  files: readonly import("@tutti-os/workspace-file-reference/contracts").WorkspaceFileReference[];
  mentionItems: readonly AgentContextMentionItem[];
}

interface UseComposerDraftReferencePickerInput {
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  clearActiveFileMentionTrigger: () => void;
  onRequestWorkspaceReferences?:
    | ((
        entity?: AgentContextMentionItem | null
      ) => Promise<WorkspaceReferencePickResult>)
    | null;
}

export function useComposerDraftReferencePicker({
  editorHandleRef,
  clearActiveFileMentionTrigger,
  onRequestWorkspaceReferences
}: UseComposerDraftReferencePickerInput) {
  const applyReferencePickResult = useCallback(
    async (result: WorkspaceReferencePickResult) => {
      if (result.files.length > 0) {
        editorHandleRef.current?.insertWorkspaceReferences(result.files);
      }
      if (result.mentionItems.length > 0) {
        editorHandleRef.current?.insertMentionItems(result.mentionItems);
      }
    },
    [editorHandleRef]
  );

  const handleWorkspaceReferencePicker = useCallback(async () => {
    if (!onRequestWorkspaceReferences) return;
    await applyReferencePickResult(await onRequestWorkspaceReferences());
  }, [applyReferencePickResult, onRequestWorkspaceReferences]);

  const handleOpenReferencesForEntity = useCallback(
    (entity: AgentContextMentionItem): void => {
      if (!onRequestWorkspaceReferences) return;
      void onRequestWorkspaceReferences(entity).then((result) => {
        if (result.files.length > 0 || result.mentionItems.length > 0) {
          flushSync(clearActiveFileMentionTrigger);
        }
        return applyReferencePickResult(result);
      });
    },
    [
      clearActiveFileMentionTrigger,
      applyReferencePickResult,
      onRequestWorkspaceReferences
    ]
  );

  return {
    applyReferencePickResult,
    handleOpenReferencesForEntity,
    handleWorkspaceReferencePicker
  };
}
