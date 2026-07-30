import { useCallback, useEffect, useMemo, useState } from "react";
import {
  useAgentSideConversationSnapshot,
  useOptionalAgentSideConversationRuntime
} from "../../../agentSideConversationRuntime";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerProps } from "../AgentComposer";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { emptyAgentComposerDraft } from "../model/agentComposerDraft";
import { useTranslation } from "../../../i18n/index";
import { projectAgentSideConversationViewState } from "../../../agentSideConversationViewProjection";

interface UseAgentGUIDetailSideConversationInput {
  workspaceId: string;
  sourceAgentSessionId: string | null;
  provider: string;
  cwd: string | null;
  availableCommands: AgentComposerProps["availableCommands"];
  submitPrompt: NonNullable<AgentComposerProps["onSubmit"]>;
}

export function useAgentGUIDetailSideConversation({
  workspaceId,
  sourceAgentSessionId,
  provider,
  cwd,
  availableCommands,
  submitPrompt
}: UseAgentGUIDetailSideConversationInput) {
  const { t } = useTranslation();
  const runtime = useOptionalAgentSideConversationRuntime();
  const runtimeActive = useAgentSideConversationSnapshot(workspaceId).active;
  const active = useMemo(
    () => projectAgentSideConversationViewState(runtimeActive),
    [runtimeActive]
  );
  const activeSideAgentSessionId = active?.sideAgentSessionId ?? null;
  const [focusedSideAgentSessionId, setFocusedSideAgentSessionId] = useState<
    string | null
  >(null);
  const focused = activeSideAgentSessionId === focusedSideAgentSessionId;
  const emptyDraft = useMemo(emptyAgentComposerDraft, [
    activeSideAgentSessionId
  ]);
  const [draftState, setDraftState] = useState<{
    sideAgentSessionId: string | null;
    content: AgentComposerDraft;
  }>(() => ({
    sideAgentSessionId: null,
    content: emptyAgentComposerDraft()
  }));
  const draftContent =
    draftState.sideAgentSessionId === activeSideAgentSessionId
      ? draftState.content
      : emptyDraft;
  const setDraftContent = useCallback(
    (content: AgentComposerDraft) =>
      setDraftState({ sideAgentSessionId: activeSideAgentSessionId, content }),
    [activeSideAgentSessionId]
  );
  const setFocused = useCallback(
    (nextFocused: boolean) =>
      setFocusedSideAgentSessionId(
        nextFocused ? activeSideAgentSessionId : null
      ),
    [activeSideAgentSessionId]
  );

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

  const open = useCallback(
    async (initialPrompt?: string | null) => {
      if (!runtime || !sourceAgentSessionId) return null;
      const existing = runtime.getSnapshot(workspaceId).active;
      if (existing?.sourceAgentSessionId === sourceAgentSessionId) {
        if (initialPrompt?.trim() && existing.status === "idle") {
          await runtime.send({
            workspaceId,
            sideAgentSessionId: existing.sideAgentSessionId,
            content: [{ type: "text", text: initialPrompt.trim() }],
            displayPrompt: initialPrompt.trim()
          });
        }
        return existing;
      }
      const capabilities = await runtime.resolveCapabilities({
        workspaceId,
        sourceAgentSessionId,
        provider,
        cwd
      });
      if (
        !capabilities.supported ||
        !capabilities.ephemeral ||
        !capabilities.hideInheritedTurns ||
        !capabilities.modelBoundaryInjected ||
        !capabilities.activeSourceTurn
      ) {
        throw new Error("side_conversation_unsupported");
      }
      const opened = await runtime.open({
        workspaceId,
        sourceAgentSessionId,
        provider,
        cwd
      });
      if (initialPrompt?.trim()) {
        await runtime.send({
          workspaceId,
          sideAgentSessionId: opened.sideAgentSessionId,
          content: [{ type: "text", text: initialPrompt.trim() }],
          displayPrompt: initialPrompt.trim()
        });
      }
      return opened;
    },
    [cwd, provider, runtime, sourceAgentSessionId, workspaceId]
  );

  const submitMain = useCallback<NonNullable<AgentComposerProps["onSubmit"]>>(
    (content, displayPrompt, options) => {
      const text = content
        .filter((block) => block.type === "text")
        .map((block) => block.text ?? "")
        .join("");
      const invocation = text.trim().match(/^\/side(?:\s+([\s\S]*))?$/);
      if (!invocation) {
        submitPrompt(content, displayPrompt, options);
        return;
      }
      void open(invocation[1]?.trim() ?? null).catch(() => {});
    },
    [open, submitPrompt]
  );

  const submitSide = useCallback<NonNullable<AgentComposerProps["onSubmit"]>>(
    (content, displayPrompt) => {
      if (!runtime || !active || active.status !== "idle") return;
      void runtime
        .send({
          workspaceId,
          sideAgentSessionId: active.sideAgentSessionId,
          content,
          displayPrompt
        })
        .then(() => setDraftContent(emptyAgentComposerDraft()))
        .catch(() => {});
    },
    [active, runtime, workspaceId]
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
    if (!runtime || !active?.activeTurnId) return;
    void runtime
      .cancel({
        workspaceId,
        sideAgentSessionId: active.sideAgentSessionId,
        turnId: active.activeTurnId
      })
      .catch(() => {});
  }, [active, runtime, workspaceId]);

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
    canOpen: Boolean(runtime && sourceAgentSessionId),
    close,
    commands,
    draftContent,
    focused,
    interactionSubmitting,
    interactivePrompt,
    interrupt,
    open,
    setFocused,
    setDraftContent,
    sourceAgentSessionId,
    submitMain,
    submitSide,
    submitInteraction
  };
}
