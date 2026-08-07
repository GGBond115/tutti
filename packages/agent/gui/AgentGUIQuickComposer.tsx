import { useMemo, type JSX } from "react";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import { TooltipProvider } from "@tutti-os/ui-system";
import type { RichTextMentionService } from "@tutti-os/ui-rich-text/service";
import { createWorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import { AgentComposer } from "./agent-gui/agentGuiNode/AgentComposer";
import { AgentGUIMentionServiceBoundary } from "./agent-gui/agentGuiNode/AgentGUIMentionServiceBoundary";
import { useAgentGUIViewLabels } from "./agent-gui/agentGuiNode/AgentGUINode.labels";
import type { AgentComposerProps } from "./agent-gui/agentGuiNode/composer/AgentComposer.types";
import type {
  AgentGUIComposerGate,
  AgentGUIComposerSettingsVM
} from "./agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import {
  agentComposerDraftPrompt,
  agentComposerDraftToPromptContent,
  agentPromptContentToComposerDraft
} from "./agent-gui/agentGuiNode/model/agentComposerDraft";
import {
  AgentGuiI18nProvider,
  type AgentGuiI18nLocale,
  useTranslation
} from "./i18n/index";
import type { AgentGUIAgentTarget } from "./types";

const readyGate: AgentGUIComposerGate = {
  conversationBusy: false,
  editor: { reason: null, status: "editable" },
  runtime: {
    reason: null,
    sessionRuntimeReason: null,
    status: "ready"
  },
  submission: { reason: null, status: "ready" }
};

const quickComposerSettings: AgentGUIComposerSettingsVM = {
  availableModels: [],
  availablePermissionModes: [],
  availableReasoningEfforts: [],
  availableSpeeds: [],
  draftSettings: {
    browserUse: true,
    computerUse: true,
    model: null,
    permissionModeId: null,
    planMode: false,
    reasoningEffort: null,
    speed: null
  },
  isSettingsLoading: false,
  modelUnavailable: false,
  permissionModeUnavailable: false,
  reasoningUnavailable: false,
  selectedProjectPath: null,
  selectedProjectSectionKey: "conversations",
  sessionSettings: null,
  speedUnavailable: false,
  supportsModel: false,
  supportsPermissionMode: false,
  supportsPlanMode: false,
  supportsReasoningEffort: false,
  supportsSpeed: false
};

export interface AgentGUIQuickComposerProps {
  agentTargets: readonly AgentGUIAgentTarget[];
  composerActionAccessory?: AgentComposerProps["composerActionAccessory"];
  composerActionPlacement?: AgentComposerProps["composerActionPlacement"];
  content: readonly AgentPromptContentBlock[];
  disabled?: boolean;
  /** Fill a height explicitly assigned by the embedding host. */
  fillAvailableHeight?: boolean;
  locale?: AgentGuiI18nLocale;
  /** Mention providers supplied by the embedding host. */
  mentionService?: RichTextMentionService;
  /** Reserved host chrome above portaled composer menus. */
  menuViewportTopInset?: number;
  onAgentTargetChange(agentTargetId: string): void;
  onContentChange(content: AgentPromptContentBlock[]): void;
  onRequestWorkspaceReferences?: AgentComposerProps["onRequestWorkspaceReferences"];
  onSubmit(content: AgentPromptContentBlock[], displayPrompt?: string): void;
  placeholder?: string;
  selectedAgentTargetId: string;
  workspaceId: string;
}

/**
 * A lifecycle-free AgentGUI entry surface for launchers that already have a
 * workspace Engine owner. It owns only draft presentation; the Host receives
 * the submitted prompt and remains responsible for Session activation.
 */
export function AgentGUIQuickComposer(
  props: AgentGUIQuickComposerProps
): JSX.Element {
  return (
    <AgentGuiI18nProvider locale={props.locale}>
      <TooltipProvider delayDuration={120} skipDelayDuration={0}>
        <AgentGUIMentionServiceBoundary service={props.mentionService}>
          <AgentGUIQuickComposerInner {...props} />
        </AgentGUIMentionServiceBoundary>
      </TooltipProvider>
    </AgentGuiI18nProvider>
  );
}

function AgentGUIQuickComposerInner({
  agentTargets,
  composerActionAccessory,
  composerActionPlacement,
  content,
  disabled = false,
  fillAvailableHeight = false,
  menuViewportTopInset,
  onAgentTargetChange,
  onContentChange,
  onRequestWorkspaceReferences,
  onSubmit,
  placeholder,
  selectedAgentTargetId,
  workspaceId
}: AgentGUIQuickComposerProps): JSX.Element {
  const { i18n, locale, t } = useTranslation();
  const selectedAgentTarget =
    agentTargets.find(
      (target) =>
        target.targetId === selectedAgentTargetId ||
        target.agentTargetId === selectedAgentTargetId
    ) ??
    agentTargets[0] ??
    null;
  const provider = selectedAgentTarget?.provider ?? "codex";
  const labels = useAgentGUIViewLabels({
    displayProviderLabel: selectedAgentTarget?.label ?? provider,
    fallbackAgentTitle: selectedAgentTarget?.label ?? provider,
    t,
    workspaceAppIcons: [],
    workspaceId
  });
  const workspaceUserProjectI18n = useMemo(
    () => createWorkspaceUserProjectI18nRuntime(i18n),
    [i18n]
  );
  const draftContent = useMemo(
    () => agentPromptContentToComposerDraft(content, "quick-composer"),
    [content]
  );

  return (
    <div className="agent-gui-node__shell">
      <AgentComposer
        activePrompt={null}
        agentTargets={agentTargets}
        availableCommands={[]}
        canGoalControl={false}
        canUploadAttachment={true}
        composerActionAccessory={composerActionAccessory}
        composerActionPlacement={composerActionPlacement}
        composerSettings={quickComposerSettings}
        drainingQueuedPromptId={null}
        draftContent={draftContent}
        gate={readyGate}
        fillAvailableHeight={fillAvailableHeight}
        isInterrupting={false}
        isSendingTurn={false}
        isSubmittingPrompt={disabled}
        layoutMode="embedded"
        menuViewportTopInset={menuViewportTopInset}
        labels={{
          ...labels,
          approvalLead: labels.approvalRequired,
          fileChangeApprovalLead: labels.fileChangeApprovalRequired
        }}
        placeholder={placeholder ?? labels.initialPlaceholder}
        presentationEditorDisabled={disabled}
        presentationSubmitDisabled={disabled || selectedAgentTarget === null}
        projectMissingProbeEnabled={false}
        promptImagesSupported={true}
        provider={provider}
        providerSelectLabel={labels.providerSwitchLabel}
        queuedPrompts={[]}
        selectedAgentTarget={selectedAgentTarget}
        showStopButton={false}
        stopDisabled={true}
        uiLanguage={locale}
        workspaceId={workspaceId}
        workspaceUserProjectI18n={workspaceUserProjectI18n}
        onDraftContentChange={(nextDraft) => {
          onContentChange(
            agentComposerDraftToPromptContent({
              draft: nextDraft,
              skills: []
            })
          );
        }}
        onEditQueuedPrompt={() => {}}
        onInterruptCurrentTurn={() => {}}
        onProviderSelect={({ agentTargetId }) => {
          if (agentTargetId) {
            onAgentTargetChange(agentTargetId);
          }
        }}
        onRemoveQueuedPrompt={() => {}}
        onRequestWorkspaceReferences={onRequestWorkspaceReferences}
        onSendQueuedPromptNext={() => {}}
        onSettingsChange={() => {}}
        onSubmit={(nextContent, displayPrompt) => {
          onSubmit(
            nextContent,
            displayPrompt ?? agentComposerDraftPrompt(draftContent)
          );
        }}
        onSubmitInteractivePrompt={() => false}
      />
    </div>
  );
}
