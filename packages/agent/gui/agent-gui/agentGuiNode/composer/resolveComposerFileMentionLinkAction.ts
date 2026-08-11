import {
  resolveWorkspaceLinkAction,
  type WorkspaceLinkAction,
  type WorkspaceLinkActionSource
} from "../../../actions/workspaceLinkActions";
import { parseMentionItemFromHref } from "../agentRichText/agentFileMentionExtension";
import { agentComposerDraftFiles } from "../model/agentComposerDraft";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile
} from "../model/agentGuiNodeTypes";

export type ComposerFileMentionLinkResolution =
  | { kind: "open"; action: WorkspaceLinkAction }
  | { kind: "blocked"; reason: "uploading" | "failed" | "unavailable" }
  | null;

/**
 * Composer-file chips only carry an attachment id + status in the href. Resolve
 * the live draft registry entry back to a real locator so host open/preview can
 * run the same path as ordinary file mentions.
 */
export function resolveComposerFileMentionLinkAction(input: {
  href: string;
  files: readonly AgentComposerDraftFile[];
  workspaceRoot?: string | null;
  source: WorkspaceLinkActionSource;
}): ComposerFileMentionLinkResolution {
  const item = parseMentionItemFromHref({ name: "", href: input.href });
  if (item?.kind !== "file" || !item.attachmentId?.trim()) {
    return null;
  }
  // Path-backed file mentions already carry a locator href; only the
  // composer-file form parks the locator in the draft registry.
  if (item.path.trim()) {
    return null;
  }

  const file = input.files.find(
    (candidate) => candidate.id === item.attachmentId
  );
  if (!file) {
    return { kind: "blocked", reason: "unavailable" };
  }
  if (file.uploading) {
    return { kind: "blocked", reason: "uploading" };
  }
  if (file.uploadError) {
    return { kind: "blocked", reason: "failed" };
  }

  const locator =
    file.path?.trim() || file.hostPath?.trim() || file.url?.trim() || "";
  if (!locator) {
    return { kind: "blocked", reason: "unavailable" };
  }

  const action = resolveWorkspaceLinkAction({
    href: locator,
    workspaceRoot: input.workspaceRoot,
    source: input.source
  });
  if (!action) {
    return { kind: "blocked", reason: "unavailable" };
  }
  return { kind: "open", action };
}

export function collectComposerDraftFiles(input: {
  activeFiles: readonly AgentComposerDraftFile[];
  draftsByScope?: Readonly<Record<string, AgentComposerDraft>>;
}): AgentComposerDraftFile[] {
  const byId = new Map<string, AgentComposerDraftFile>();
  for (const file of input.activeFiles) {
    byId.set(file.id, file);
  }
  for (const draft of Object.values(input.draftsByScope ?? {})) {
    for (const file of agentComposerDraftFiles(draft)) {
      if (!byId.has(file.id)) {
        byId.set(file.id, file);
      }
    }
  }
  return [...byId.values()];
}
