import type { SubmitRequestedIntent } from "./pendingIntents.types.ts";
import {
  clonePromptCapabilityReferences,
  clonePromptRequiredSettingsPatch
} from "./promptQueue.prompt.ts";
import { createPromptQueueId } from "./promptQueue.identity.ts";
import type { EngineQueuedPrompt } from "./promptQueue.types.ts";

export function queuedPromptFromSubmitIntent(
  intent: SubmitRequestedIntent,
  visibleInQueue: boolean
): EngineQueuedPrompt {
  return {
    ...clonePromptCapabilityReferences(intent.capabilityRefs),
    clientSubmitId: intent.clientSubmitId,
    content: intent.content,
    createdAtUnixMs: intent.requestedAtUnixMs,
    ...(intent.displayPrompt ? { displayPrompt: intent.displayPrompt } : {}),
    id: intent.promptId?.trim() || createPromptQueueId(),
    ...clonePromptRequiredSettingsPatch(intent.requiredSettingsPatch),
    submitDiagnostics: {
      ...(intent.submitDiagnostics ?? {}),
      blockCount: intent.submitDiagnostics?.blockCount ?? intent.content.length,
      queued: visibleInQueue,
      submittedAtUnixMs:
        intent.submitDiagnostics?.submittedAtUnixMs ?? intent.requestedAtUnixMs
    },
    ...(intent.runtimeContent ? { runtimeContent: intent.runtimeContent } : {}),
    ...(intent.targetTurnId?.trim()
      ? { targetTurnId: intent.targetTurnId.trim() }
      : {}),
    visibleInQueue
  };
}
