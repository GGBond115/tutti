import { normalizeAgentActivityCapabilityReferences } from "../capabilityReferences.ts";
import type { EngineQueuedPrompt } from "./promptQueue.types.ts";

export function clonePromptCapabilityReferences(
  references: EngineQueuedPrompt["capabilityRefs"]
): Pick<EngineQueuedPrompt, "capabilityRefs"> {
  const normalized = normalizeAgentActivityCapabilityReferences(references);
  return normalized.length > 0 ? { capabilityRefs: normalized } : {};
}

export function clonePromptRequiredSettingsPatch(
  patch: EngineQueuedPrompt["requiredSettingsPatch"]
): Pick<EngineQueuedPrompt, "requiredSettingsPatch"> {
  return patch ? { requiredSettingsPatch: { ...patch } } : {};
}

export function normalizeQueuedPrompt(
  prompt: EngineQueuedPrompt
): EngineQueuedPrompt | null {
  const id = prompt.id.trim();
  if (!id || prompt.content.length === 0) return null;
  // This is the raw queue-record boundary. Older persisted records predate
  // clientSubmitId, so the compatibility mapping is deliberately kept here.
  const clientSubmitId = prompt.clientSubmitId?.trim() || id;
  return {
    clientSubmitId,
    ...clonePromptCapabilityReferences(prompt.capabilityRefs),
    content: prompt.content.map((block) => ({ ...block })),
    createdAtUnixMs: prompt.createdAtUnixMs,
    ...(prompt.displayPrompt?.trim()
      ? { displayPrompt: prompt.displayPrompt.trim() }
      : {}),
    ...(prompt.guidance === true ? { guidance: true } : {}),
    id,
    ...clonePromptRequiredSettingsPatch(prompt.requiredSettingsPatch),
    ...(prompt.submitDiagnostics
      ? { submitDiagnostics: { ...prompt.submitDiagnostics } }
      : {}),
    ...(prompt.targetTurnId?.trim()
      ? { targetTurnId: prompt.targetTurnId.trim() }
      : {}),
    ...(prompt.runtimeContent
      ? { runtimeContent: prompt.runtimeContent.map((block) => ({ ...block })) }
      : {}),
    ...(prompt.visibleInQueue === false ? { visibleInQueue: false } : {})
  };
}
