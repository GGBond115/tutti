import type { PromptQueueState } from "./promptQueue.types.ts";

export function promptQueuePromptIdForClientSubmit(
  state: PromptQueueState,
  agentSessionId: string,
  clientSubmitId: string
): string | null {
  return (
    state.recordsBySessionId[agentSessionId.trim()]?.prompts.find(
      (prompt) => prompt.clientSubmitId === clientSubmitId.trim()
    )?.id ?? null
  );
}

export function promptQueueHasClientSubmitInFlight(
  state: PromptQueueState,
  agentSessionId: string,
  clientSubmitId: string
): boolean {
  const sessionId = agentSessionId.trim();
  const submitId = clientSubmitId.trim();
  const record = state.recordsBySessionId[sessionId];
  if (!record || !submitId) return false;
  const promptId = promptQueuePromptIdForClientSubmit(
    state,
    sessionId,
    submitId
  );
  return Boolean(
    record.inFlight &&
    (record.inFlight.clientSubmitId === submitId ||
      record.inFlight.promptId === promptId)
  );
}

export function promptQueueHasClientSubmitUncertainDelivery(
  state: PromptQueueState,
  agentSessionId: string,
  clientSubmitId: string
): boolean {
  const sessionId = agentSessionId.trim();
  const submitId = clientSubmitId.trim();
  const record = state.recordsBySessionId[sessionId];
  if (!record || !submitId) return false;
  const promptId = promptQueuePromptIdForClientSubmit(
    state,
    sessionId,
    submitId
  );
  return Boolean(
    record.uncertainDelivery &&
    (record.uncertainDelivery.clientSubmitId === submitId ||
      record.uncertainDelivery.promptId === promptId)
  );
}

export function canCancelQueuedSubmit(
  state: PromptQueueState,
  agentSessionId: string,
  clientSubmitId: string
): boolean {
  const record = state.recordsBySessionId[agentSessionId.trim()];
  const promptId = promptQueuePromptIdForClientSubmit(
    state,
    agentSessionId,
    clientSubmitId
  );
  return Boolean(
    record &&
    promptId &&
    record.inFlight?.promptId !== promptId &&
    record.uncertainDelivery?.promptId !== promptId
  );
}

export function isQueuedSubmitDeliveryPending(
  state: PromptQueueState,
  agentSessionId: string,
  clientSubmitId: string
): boolean {
  const record = state.recordsBySessionId[agentSessionId.trim()];
  const promptId = promptQueuePromptIdForClientSubmit(
    state,
    agentSessionId,
    clientSubmitId
  );
  return Boolean(
    record &&
    promptId &&
    record.failedPromptId !== promptId &&
    record.uncertainDelivery?.promptId !== promptId
  );
}
