import type { ComponentProps } from "react";
import type { AgentSideConversationState } from "../../../agentSideConversationRuntime";
import { AgentInteractivePromptSurface } from "../../../shared/agentConversation/components/AgentInteractivePromptSurface";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import { useTranslation } from "../../../i18n/index";

interface AgentGUISideConversationPaneProps {
  active: AgentSideConversationState;
  interactivePrompt: AgentConversationPromptVM | null;
  interactionSubmitting: boolean;
  interactivePromptLabels: ComponentProps<
    typeof AgentInteractivePromptSurface
  >["labels"];
  onClose(): void;
  onSubmitInteraction: ComponentProps<
    typeof AgentInteractivePromptSurface
  >["onSubmit"];
}

export function AgentGUISideConversationPane({
  active,
  interactivePrompt,
  interactionSubmitting,
  interactivePromptLabels,
  onClose,
  onSubmitInteraction
}: AgentGUISideConversationPaneProps): React.JSX.Element {
  const { t } = useTranslation();
  return (
    <section
      className="mx-3 mb-2 max-h-52 overflow-auto rounded-xl border border-border/70 bg-background/95 p-3 shadow-sm"
      aria-label={t("agentHost.agentGui.sidePanelTitle")}
      data-testid="agent-gui-side-panel"
    >
      <header className="mb-2 flex items-center justify-between gap-2">
        <div>
          <div className="text-sm font-medium">
            {t("agentHost.agentGui.sidePanelTitle")}
          </div>
          <div className="text-xs text-muted-foreground">
            {active.status === "opening"
              ? t("agentHost.agentGui.sideOpening")
              : t("agentHost.agentGui.sideEphemeralHint")}
          </div>
        </div>
        <button
          type="button"
          className="rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted"
          onClick={onClose}
        >
          {t("agentHost.agentGui.sideClose")}
        </button>
      </header>
      <div className="space-y-2">
        {active.messages.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t("agentHost.agentGui.sideEmpty")}
          </p>
        ) : (
          active.messages.map((message) => (
            <div
              key={message.id}
              className="rounded-lg bg-muted/60 px-3 py-2 text-sm whitespace-pre-wrap"
              data-role={message.role}
            >
              {message.text}
            </div>
          ))
        )}
        {active.error ? (
          <p className="text-sm text-destructive">
            {active.error === "content_unsupported"
              ? t("agentHost.agentGui.sideContentUnsupported")
              : t("agentHost.agentGui.sideOperationFailed")}
          </p>
        ) : null}
        {interactivePrompt ? (
          <AgentInteractivePromptSurface
            embedded
            prompt={interactivePrompt}
            isSubmitting={interactionSubmitting}
            onSubmit={onSubmitInteraction}
            labels={interactivePromptLabels}
          />
        ) : null}
      </div>
    </section>
  );
}
