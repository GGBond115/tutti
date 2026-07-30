import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  supportsAgentSideConversation,
  useAgentSideConversationSnapshot,
  useOptionalAgentSideConversationRuntime
} from "../../../agentSideConversationRuntime";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerProps } from "../AgentComposer";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { emptyAgentComposerDraft } from "../model/agentComposerDraft";
import { useTranslation } from "../../../i18n/index";
import { projectAgentSideConversationViewState } from "../../../agentSideConversationViewProjection";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto/agentSession";

export function parseAgentSideInvocation(
  content: readonly AgentPromptContentBlock[]
): { prompt: string | null; contentSupported: boolean } | null {
  const text = content
    .filter((block) => block.type === "text")
    .map((block) => block.text ?? "")
    .join("");
  const invocation = text.trim().match(/^\/side(?:\s+([\s\S]*))?$/);
  if (!invocation) return null;
  return {
    prompt: invocation[1]?.trim() || null,
    contentSupported: content.every((block) => block.type === "text")
  };
}

export function appendAgentSidePromptToDraft(
  draft: AgentComposerDraft,
  prompt: string
): AgentComposerDraft {
  const normalizedPrompt = prompt.trim();
  if (!normalizedPrompt) return draft;
  const [textBlock, ...attachmentBlocks] = draft;
  const currentText = textBlock.text.trim();
  return [
    {
      ...textBlock,
      text: currentText
        ? `${textBlock.text}\n${normalizedPrompt}`
        : normalizedPrompt
    },
    ...attachmentBlocks
  ];
}

interface UseAgentGUIDetailSideConversationInput {
  workspaceId: string;
  sourceAgentSessionId: string | null;
  sourceTurnActive: boolean;
  provider: string;
  cwd: string | null;
  availableCommands: AgentComposerProps["availableCommands"];
  submitPrompt: NonNullable<AgentComposerProps["onSubmit"]>;
}

export function useAgentGUIDetailSideConversation({
  workspaceId,
  sourceAgentSessionId,
  sourceTurnActive,
  provider,
  cwd,
  availableCommands,
  submitPrompt
}: UseAgentGUIDetailSideConversationInput) {
  const { t } = useTranslation();
  const runtime = useOptionalAgentSideConversationRuntime();
  const [capabilityState, setCapabilityState] = useState<{
    identity: string;
    runtime: NonNullable<typeof runtime>;
    supported: boolean;
  } | null>(null);
  const [entryErrorState, setEntryErrorState] = useState<{
    identity: string;
    runtime: typeof runtime;
    code: "content_unsupported" | "operation_failed";
  } | null>(null);
  const capabilityIdentity = `${workspaceId}:${sourceAgentSessionId ?? ""}:${provider}:${cwd ?? ""}:${sourceTurnActive ? "active" : "idle"}`;
  const lifecycleTokenRef = useRef<{
    runtime: typeof runtime;
    sourceAgentSessionId: string | null;
    workspaceId: string;
  } | null>(null);
  const entryError =
    entryErrorState?.identity === capabilityIdentity &&
    entryErrorState.runtime === runtime
      ? entryErrorState.code
      : null;
  useEffect(() => {
    let canceled = false;
    const lifecycleToken = { runtime, sourceAgentSessionId, workspaceId };
    lifecycleTokenRef.current = lifecycleToken;
    if (runtime && sourceAgentSessionId && sourceTurnActive) {
      void runtime
        .resolveCapabilities({
          workspaceId,
          sourceAgentSessionId,
          provider,
          cwd
        })
        .then((capabilities) => {
          if (canceled) return;
          setCapabilityState({
            identity: capabilityIdentity,
            runtime,
            supported: supportsAgentSideConversation(capabilities)
          });
        })
        .catch(() => {
          if (!canceled) {
            setCapabilityState({
              identity: capabilityIdentity,
              runtime,
              supported: false
            });
          }
        });
    }
    return () => {
      canceled = true;
      queueMicrotask(() => {
        const next = lifecycleTokenRef.current;
        const ownershipEnded =
          next === lifecycleToken ||
          next?.runtime !== runtime ||
          next?.sourceAgentSessionId !== sourceAgentSessionId ||
          next?.workspaceId !== workspaceId;
        if (!ownershipEnded || !runtime || !sourceAgentSessionId) return;
        const ownedSide = runtime.getSnapshot(workspaceId).active;
        if (ownedSide?.sourceAgentSessionId !== sourceAgentSessionId) return;
        void runtime
          .close({
            workspaceId,
            sideAgentSessionId: ownedSide.sideAgentSessionId
          })
          .catch(() => {});
      });
    };
  }, [
    capabilityIdentity,
    cwd,
    provider,
    runtime,
    sourceAgentSessionId,
    sourceTurnActive,
    workspaceId
  ]);
  const sideSupported =
    capabilityState?.identity === capabilityIdentity &&
    capabilityState.runtime === runtime &&
    capabilityState.supported;
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

  const open = useCallback(
    async (initialPrompt?: string | null) => {
      if (!runtime || !sourceAgentSessionId) return null;
      setEntryErrorState(null);
      try {
        const existing = runtime.getSnapshot(workspaceId).active;
        if (existing?.sourceAgentSessionId === sourceAgentSessionId) {
          if (
            existing.status === "error" &&
            existing.error === "side_close_failed"
          ) {
            await runtime.close({
              workspaceId,
              sideAgentSessionId: existing.sideAgentSessionId
            });
          } else {
            const prompt = initialPrompt?.trim();
            if (prompt) {
              if (existing.status === "idle") {
                await runtime.send({
                  workspaceId,
                  sideAgentSessionId: existing.sideAgentSessionId,
                  content: [{ type: "text", text: prompt }],
                  displayPrompt: prompt
                });
              } else {
                setDraftState((current) => ({
                  sideAgentSessionId: existing.sideAgentSessionId,
                  content: appendAgentSidePromptToDraft(
                    current.sideAgentSessionId === existing.sideAgentSessionId
                      ? current.content
                      : emptyAgentComposerDraft(),
                    prompt
                  )
                }));
                setFocusedSideAgentSessionId(existing.sideAgentSessionId);
              }
            }
            return existing;
          }
        }
        const capabilities = await runtime.resolveCapabilities({
          workspaceId,
          sourceAgentSessionId,
          provider,
          cwd
        });
        if (!supportsAgentSideConversation(capabilities)) {
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
      } catch (error) {
        setEntryErrorState({
          identity: capabilityIdentity,
          runtime,
          code: "operation_failed"
        });
        throw error;
      }
    },
    [
      capabilityIdentity,
      cwd,
      provider,
      runtime,
      sourceAgentSessionId,
      workspaceId
    ]
  );

  const submitMain = useCallback<NonNullable<AgentComposerProps["onSubmit"]>>(
    (content, displayPrompt, options) => {
      const invocation = parseAgentSideInvocation(content);
      if (!invocation) {
        submitPrompt(content, displayPrompt, options);
        return;
      }
      if (!invocation.contentSupported) {
        setEntryErrorState({
          identity: capabilityIdentity,
          runtime,
          code: "content_unsupported"
        });
        return;
      }
      void open(invocation.prompt).catch(() => {});
    },
    [capabilityIdentity, open, runtime, submitPrompt]
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
      runtime && sourceAgentSessionId && sideSupported
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
    [availableCommands, runtime, sideSupported, sourceAgentSessionId, t]
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
    async (input: {
      requestId: string;
      action?: string;
      optionId?: string;
      payload?: Record<string, unknown>;
    }) => {
      const interaction = active?.pendingInteraction;
      if (!runtime || !active || !interaction) return;
      setInteractionSubmitting(true);
      try {
        await runtime.respond({
          workspaceId,
          sideAgentSessionId: active.sideAgentSessionId,
          turnId: interaction.turnId,
          ...input
        });
      } finally {
        setInteractionSubmitting(false);
      }
    },
    [active, runtime, workspaceId]
  );

  return {
    active,
    canOpen: Boolean(runtime && sourceAgentSessionId && sideSupported),
    close,
    commands,
    draftContent,
    entryError,
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
