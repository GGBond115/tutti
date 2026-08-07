import { useEffect, useMemo } from "react";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import type { AgentGUIAgentTarget } from "@tutti-os/agent-gui";
import {
  AgentGUIQuickComposer,
  type AgentGUIQuickComposerProps
} from "@tutti-os/agent-gui/quick-composer";
import { createRichTextMentionHref } from "@tutti-os/ui-rich-text/core";
import type { TuttiExternalReferenceSelection } from "@tutti-os/workspace-external-core/contracts";
import { createTuttiExternalRichTextMentionService } from "@tutti-os/workspace-external-core/rich-text";
import type { DesktopLocale } from "../../../../../shared/i18n/core/locale.ts";
import type { DesktopCaptureWindowController } from "./desktopCaptureWindowController.ts";

export function DesktopCaptureComposer({
  agentTargets,
  content,
  controller,
  disabled,
  locale,
  placeholder,
  selectedAgentTargetId,
  taskInstruction,
  workspaceId
}: {
  agentTargets: readonly AgentGUIAgentTarget[];
  content: readonly AgentPromptContentBlock[];
  controller: DesktopCaptureWindowController;
  disabled: boolean;
  locale: DesktopLocale;
  placeholder: string;
  selectedAgentTargetId: string;
  taskInstruction: string;
  workspaceId: string;
}) {
  const mentionService = useMemo(
    () =>
      createTuttiExternalRichTextMentionService({
        getBridge: () => controller.mentionBridge
      }),
    [controller]
  );

  useEffect(() => () => mentionService.dispose(), [mentionService]);

  return (
    <AgentGUIQuickComposer
      agentTargets={agentTargets}
      content={content}
      disabled={disabled}
      locale={locale}
      mentionService={mentionService}
      menuViewportTopInset={48}
      placeholder={placeholder}
      selectedAgentTargetId={selectedAgentTargetId}
      workspaceId={workspaceId}
      onAgentTargetChange={(agentTargetId) =>
        controller.setAgentTargetId(agentTargetId)
      }
      onContentChange={(nextContent) => controller.setContent(nextContent)}
      onRequestWorkspaceReferences={async () =>
        toCaptureReferencePickResult(await controller.selectReferences())
      }
      onSubmit={(nextContent, displayPrompt) =>
        void controller.submit(nextContent, displayPrompt, taskInstruction)
      }
    />
  );
}

type CaptureReferencePickResult = Awaited<
  ReturnType<
    NonNullable<AgentGUIQuickComposerProps["onRequestWorkspaceReferences"]>
  >
>;

function toCaptureReferencePickResult(
  selections: readonly TuttiExternalReferenceSelection[]
): CaptureReferencePickResult {
  const files = selections.flatMap((selection) =>
    selection.selectionKind === "path" ? [selection.reference] : []
  );
  const mentionItems = selections.flatMap((selection) =>
    selection.selectionKind === "workspace-reference"
      ? [toCaptureWorkspaceReferenceMention(selection)]
      : []
  );
  return { files, mentionItems };
}

function toCaptureWorkspaceReferenceMention(
  selection: Extract<
    TuttiExternalReferenceSelection,
    { selectionKind: "workspace-reference" }
  >
) {
  return {
    kind: "workspace-reference" as const,
    href: createRichTextMentionHref({
      providerId: "workspace-reference",
      entityId: selection.id,
      label: selection.displayName,
      scope: {
        workspaceId: selection.workspaceId,
        source: selection.source,
        ...(selection.groupId ? { groupId: selection.groupId } : {}),
        ...(selection.fileCount && selection.fileCount > 0
          ? { count: String(selection.fileCount) }
          : {})
      }
    }),
    workspaceId: selection.workspaceId,
    targetId: selection.id,
    source: selection.source,
    ...(selection.groupId ? { groupId: selection.groupId } : {}),
    name: selection.displayName,
    fileCount: selection.fileCount ?? 0
  };
}
