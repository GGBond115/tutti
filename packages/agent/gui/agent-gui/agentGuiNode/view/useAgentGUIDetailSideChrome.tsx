import { useMemo, type ReactNode } from "react";
import { PanelRightOpen } from "lucide-react";
import type { AgentSideConversationViewState } from "../../../agentSideConversationViewProjection";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerProps } from "../AgentComposer";
import {
  projectAgentSideComposerGate,
  projectAgentSideComposerSettings
} from "../model/agentGuiSideComposerPolicy";
import { useTranslation } from "../../../i18n/index";
import styles from "../AgentGUINode.styles";
import {
  AgentGUISideConversationPane,
  type AgentGUISideConversationPaneProps
} from "./AgentGUISideConversationPane";

const EMPTY_WORKSPACE_APP_ICONS: NonNullable<
  AgentComposerProps["workspaceAppIcons"]
> = [];

interface UseAgentGUIDetailSideChromeInput {
  active: AgentSideConversationViewState | null;
  availableSkills: AgentGUISideConversationPaneProps["availableSkills"];
  baseComposerProps: AgentComposerProps;
  canOpen: boolean;
  conversationFlowLabels: AgentGUISideConversationPaneProps["conversationFlowLabels"];
  draftContent: AgentComposerProps["draftContent"];
  focused: boolean;
  interactionSubmitting: boolean;
  interactivePrompt: AgentConversationPromptVM | null;
  isVisible: boolean;
  loadingLabel: string;
  sourceAgentSessionId: string | null;
  hostFooterAccessory: ReactNode;
  onClose(): void;
  onDraftContentChange: AgentComposerProps["onDraftContentChange"];
  onFocusChange(sideAgentSessionId: string | null): void;
  onInterrupt: AgentComposerProps["onInterruptCurrentTurn"];
  onOpen(): Promise<unknown>;
  onSubmit: AgentComposerProps["onSubmit"];
  onSubmitInteraction: AgentComposerProps["onSubmitInteractivePrompt"];
}

export function useAgentGUIDetailSideChrome({
  active,
  availableSkills,
  baseComposerProps,
  canOpen,
  conversationFlowLabels,
  draftContent,
  focused,
  interactionSubmitting,
  interactivePrompt,
  isVisible,
  loadingLabel,
  sourceAgentSessionId,
  hostFooterAccessory,
  onClose,
  onDraftContentChange,
  onFocusChange,
  onInterrupt,
  onOpen,
  onSubmit,
  onSubmitInteraction
}: UseAgentGUIDetailSideChromeInput): {
  bottomDockComposerProps: AgentComposerProps;
  sidePane: ReactNode;
} {
  const { t } = useTranslation();
  const footerAccessory = (
    <>
      {canOpen && !active && baseComposerProps.showStopButton ? (
        <button
          type="button"
          className={`${styles.composerMenuTrigger} w-auto`}
          aria-label={t("agentHost.agentGui.sideOpen")}
          title={t("agentHost.agentGui.sideOpen")}
          data-testid="agent-gui-open-side"
          onClick={() => void onOpen().catch(() => {})}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <PanelRightOpen aria-hidden="true" className="size-3.5" />
            <span>{t("agentHost.agentGui.sideOpen")}</span>
          </span>
        </button>
      ) : null}
      {hostFooterAccessory}
    </>
  );
  const bottomDockComposerProps = useMemo<AgentComposerProps>(
    () => ({ ...baseComposerProps, footerAccessory }),
    [baseComposerProps, footerAccessory]
  );
  const sideComposerProps = useMemo<AgentComposerProps | null>(() => {
    if (!active) return null;
    return {
      workspaceId: baseComposerProps.workspaceId,
      agentSessionId: active.sideAgentSessionId,
      workspacePath: baseComposerProps.workspacePath,
      currentUserId: baseComposerProps.currentUserId,
      provider: baseComposerProps.provider,
      draftContent,
      draftScopeKey: `side:${active.sideAgentSessionId}`,
      inputHistory: [],
      availableCommands: [],
      hasCompactableContext: false,
      compactSupported: false,
      availableSkills: [],
      gate: projectAgentSideComposerGate(active),
      presentationEditorDisabled: false,
      presentationSubmitDisabled: false,
      placeholder: t("agentHost.agentGui.sideInputPlaceholder"),
      composerSettings: projectAgentSideComposerSettings(
        baseComposerProps.composerSettings
      ),
      queuedPrompts: [],
      drainingQueuedPromptId: null,
      workspaceAppIcons: baseComposerProps.workspaceAppIcons,
      selectedAgentTarget: baseComposerProps.selectedAgentTarget,
      agentTargets: [],
      handoffAgentTargets: [],
      providerSelectReadonly: true,
      showStopButton: Boolean(active.activeTurnId),
      stopDisabled: false,
      activePrompt: interactivePrompt,
      activePromptKeyboardShortcutsEnabled:
        baseComposerProps.isActive && focused,
      promptTips: [],
      isInterrupting: false,
      isSendingTurn: Boolean(active.activeTurnId),
      isSubmittingPrompt: interactionSubmitting,
      projectMissingProbeEnabled: false,
      uiLanguage: baseComposerProps.uiLanguage,
      isActive: baseComposerProps.isActive && focused,
      workspaceReferencePickerOpen: false,
      promptImagesSupported: false,
      canGoalControl: false,
      canUploadAttachment: false,
      labels: baseComposerProps.labels,
      workspaceUserProjectI18n: baseComposerProps.workspaceUserProjectI18n,
      capabilityControlsReadOnly: true,
      onDraftContentChange,
      onSettingsChange: () => {},
      onSubmit,
      onSendQueuedPromptNext: () => {},
      onRemoveQueuedPrompt: () => {},
      onEditQueuedPrompt: () => {},
      onInterruptCurrentTurn: onInterrupt,
      onSubmitInteractivePrompt: onSubmitInteraction,
      onLinkAction: baseComposerProps.onLinkAction
    };
  }, [
    active,
    baseComposerProps,
    draftContent,
    focused,
    interactionSubmitting,
    interactivePrompt,
    onDraftContentChange,
    onInterrupt,
    onSubmit,
    onSubmitInteraction,
    t
  ]);
  const sidePane =
    active &&
    sideComposerProps &&
    active.sourceAgentSessionId === sourceAgentSessionId ? (
      <AgentGUISideConversationPane
        active={active}
        availableSkills={availableSkills}
        composerProps={sideComposerProps}
        conversationFlowLabels={conversationFlowLabels}
        isVisible={isVisible}
        loadingLabel={loadingLabel}
        workspaceAppIcons={
          baseComposerProps.workspaceAppIcons ?? EMPTY_WORKSPACE_APP_ICONS
        }
        onClose={onClose}
        onFocusChange={(nextFocused) =>
          onFocusChange(nextFocused ? active.sideAgentSessionId : null)
        }
        onLinkAction={baseComposerProps.onLinkAction}
      />
    ) : null;
  return { bottomDockComposerProps, sidePane };
}
