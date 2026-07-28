import { useCallback, useEffect, useMemo, useState } from "react";
import {
  useAgentSideConversationSnapshot,
  useOptionalAgentSideConversationRuntime
} from "../../../agentSideConversationRuntime";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerProps } from "../AgentComposer";
import { useTranslation } from "../../../i18n/index";

interface UseAgentGUIDetailSideConversationInput {
  workspaceId: string;
  sourceAgentSessionId: string | null;
  availableCommands: AgentComposerProps["availableCommands"];
  submitPrompt: NonNullable<AgentComposerProps["onSubmit"]>;
  interruptCurrentTurn(): void;
}

export function useAgentGUIDetailSideConversation({
  workspaceId,
  sourceAgentSessionId,
  availableCommands,
  submitPrompt,
  interruptCurrentTurn
}: UseAgentGUIDetailSideConversationInput) {
  const { t } = useTranslation();
  const runtime = useOptionalAgentSideConversationRuntime();
  const active = useAgentSideConversationSnapshot(workspaceId).active;

  // The surface owns the runtime. This single lifecycle binding closes a Side
  // when its source changes or the surface unmounts; runtime.dispose covers
  // host-owned teardown outside React.
  useEffect(
    () => () => {
      const ownedSide = runtime?.getSnapshot(workspaceId).active;
      if (
        !runtime ||
        !ownedSide ||
        ownedSide.sourceAgentSessionId !== sourceAgentSessionId
      ) {
        return;
      }
      void runtime
        .close({
          workspaceId,
          sideAgentSessionId: ownedSide.sideAgentSessionId
        })
        .catch(() => {});
    },
    [runtime, sourceAgentSessionId, workspaceId]
  );

  const submit = useCallback<NonNullable<AgentComposerProps["onSubmit"]>>(
    (content, displayPrompt, options) => {
      const text = content
        .filter((block) => block.type === "text")
        .map((block) => block.text ?? "")
        .join("");
      const invocation = text.trim().match(/^\/side(?:\s+([\s\S]*))?$/);
      if (!runtime || !sourceAgentSessionId) {
        if (invocation) return;
        submitPrompt(content, displayPrompt, options);
        return;
      }
      if (!active && invocation) {
        void runtime
          .resolveCapabilities({ workspaceId, sourceAgentSessionId })
          .then((capabilities) => {
            if (
              !capabilities.supported ||
              !capabilities.ephemeral ||
              !capabilities.hideInheritedTurns ||
              !capabilities.modelBoundaryInjected ||
              !capabilities.activeSourceTurn
            ) {
              throw new Error("side_conversation_unsupported");
            }
            return runtime.open({ workspaceId, sourceAgentSessionId });
          })
          .then((opened) => {
            const initialPrompt = invocation[1]?.trim();
            if (!initialPrompt) return;
            return runtime.send({
              workspaceId,
              sideAgentSessionId: opened.sideAgentSessionId,
              content: [{ type: "text", text: initialPrompt }],
              displayPrompt: initialPrompt
            });
          })
          .catch(() => {});
        return;
      }
      if (active?.sourceAgentSessionId === sourceAgentSessionId) {
        void runtime
          .send({
            workspaceId,
            sideAgentSessionId: active.sideAgentSessionId,
            content,
            displayPrompt
          })
          .catch(() => {});
        return;
      }
      submitPrompt(content, displayPrompt, options);
    },
    [active, runtime, sourceAgentSessionId, submitPrompt, workspaceId]
  );

  const commands = useMemo(
    () =>
      runtime && sourceAgentSessionId
        ? [
            ...availableCommands.filter(
              (command) => command.name.trim().toLowerCase() !== "side"
            ),
            {
              name: "side",
              description: t("agentHost.agentGui.sideCommandDescription")
            }
          ]
        : availableCommands,
    [availableCommands, runtime, sourceAgentSessionId, t]
  );

  const interrupt = useCallback(() => {
    if (runtime && active?.activeTurnId) {
      void runtime
        .cancel({
          workspaceId,
          sideAgentSessionId: active.sideAgentSessionId,
          turnId: active.activeTurnId
        })
        .catch(() => {});
      return;
    }
    interruptCurrentTurn();
  }, [active, interruptCurrentTurn, runtime, workspaceId]);

  const close = useCallback(() => {
    if (!runtime || !active) return;
    void runtime
      .close({
        workspaceId,
        sideAgentSessionId: active.sideAgentSessionId
      })
      .catch(() => {});
  }, [active, runtime, workspaceId]);

  const [interactionSubmitting, setInteractionSubmitting] = useState(false);
  const interactivePrompt = useMemo<AgentConversationPromptVM | null>(() => {
    const interaction = active?.pendingInteraction;
    if (!interaction) return null;
    if (interaction.kind === "question") {
      const rawQuestions = Array.isArray(interaction.input.questions)
        ? interaction.input.questions
        : [];
      return {
        kind: "ask-user",
        requestId: interaction.requestId,
        title:
          interaction.toolName ?? t("agentHost.agentGui.sideInteractionTitle"),
        questions: rawQuestions.flatMap((rawQuestion, index) => {
          if (!rawQuestion || typeof rawQuestion !== "object") return [];
          const question = rawQuestion as Record<string, unknown>;
          const rawOptions = Array.isArray(question.options)
            ? question.options
            : [];
          return [
            {
              id:
                typeof question.id === "string"
                  ? question.id
                  : `question-${index + 1}`,
              header:
                typeof question.header === "string"
                  ? question.header
                  : t("agentHost.agentGui.sideInteractionTitle"),
              question:
                typeof question.question === "string" ? question.question : "",
              options: rawOptions.flatMap((rawOption) => {
                if (!rawOption || typeof rawOption !== "object") return [];
                const option = rawOption as Record<string, unknown>;
                const label =
                  typeof option.label === "string"
                    ? option.label
                    : typeof option.name === "string"
                      ? option.name
                      : "";
                return label
                  ? [
                      {
                        label,
                        description:
                          typeof option.description === "string"
                            ? option.description
                            : ""
                      }
                    ]
                  : [];
              }),
              multiSelect: question.multiSelect === true
            }
          ];
        })
      };
    }
    return {
      kind: "approval",
      id: interaction.requestId,
      requestId: interaction.requestId,
      turnId: interaction.turnId,
      callId: interaction.requestId,
      title:
        interaction.toolName ?? t("agentHost.agentGui.sideInteractionTitle"),
      toolName: interaction.toolName,
      status: "pending",
      input: interaction.input,
      options: interaction.actions.map((action) => ({
        id: action.id,
        label: action.label,
        kind: action.semantic
      })),
      occurredAtUnixMs: null
    };
  }, [active?.pendingInteraction, t]);

  const submitInteraction = useCallback(
    (input: {
      requestId: string;
      action?: string;
      optionId?: string;
      payload?: Record<string, unknown>;
    }) => {
      const interaction = active?.pendingInteraction;
      if (!runtime || !active || !interaction) return;
      setInteractionSubmitting(true);
      void runtime
        .respond({
          workspaceId,
          sideAgentSessionId: active.sideAgentSessionId,
          turnId: interaction.turnId,
          ...input
        })
        .catch(() => {})
        .finally(() => setInteractionSubmitting(false));
    },
    [active, runtime, workspaceId]
  );

  return {
    active,
    close,
    commands,
    interactionSubmitting,
    interactivePrompt,
    interrupt,
    submit,
    submitInteraction
  };
}
