import { useEffect, useMemo } from "react";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import type { AgentGUIAgentTarget } from "@tutti-os/agent-gui";
import { AgentGUIQuickComposer } from "@tutti-os/agent-gui/quick-composer";
import { Switch } from "@tutti-os/ui-system";
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
  taskActionLabel,
  taskActionHint,
  taskInstruction,
  trackWithTask,
  workspaceId
}: {
  agentTargets: readonly AgentGUIAgentTarget[];
  content: readonly AgentPromptContentBlock[];
  controller: DesktopCaptureWindowController;
  disabled: boolean;
  locale: DesktopLocale;
  placeholder: string;
  selectedAgentTargetId: string;
  taskActionHint: string;
  taskActionLabel: string;
  taskInstruction: string;
  trackWithTask: boolean;
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
      composerActionAccessory={
        <div
          className="inline-flex shrink-0 items-center gap-2"
          title={taskActionHint}
        >
          <span
            className="text-[12px] leading-4 text-[var(--text-secondary)]"
            id="capture-track-with-task-label"
          >
            {taskActionLabel}
          </span>
          <Switch
            aria-labelledby="capture-track-with-task-label"
            checked={trackWithTask}
            disabled={disabled}
            size="sm"
            onCheckedChange={(checked) => controller.setTrackWithTask(checked)}
          />
        </div>
      }
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
      onRequestWorkspaceReferences={async () => ({
        files: await controller.selectFiles(),
        mentionItems: []
      })}
      onSubmit={(nextContent, displayPrompt) =>
        void controller.submit(nextContent, displayPrompt, taskInstruction)
      }
    />
  );
}
