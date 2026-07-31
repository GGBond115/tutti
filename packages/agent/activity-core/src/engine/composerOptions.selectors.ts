import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus
} from "../types.ts";
import type { AgentSessionEngineStateBase } from "./types.ts";

export function selectComposerOptions(
  state: AgentSessionEngineStateBase,
  targetKey: string | null | undefined
): AgentActivityComposerOptions | null {
  const key = targetKey?.trim() ?? "";
  if (!key) return null;
  return state.composerOptions.optionsByTargetKey[key] ?? null;
}

export function selectComposerOptionsLoadStatus(
  state: AgentSessionEngineStateBase,
  targetKey: string | null | undefined
): AgentActivityComposerOptionsLoadStatus | undefined {
  const key = targetKey?.trim() ?? "";
  if (!key) return undefined;
  return state.composerOptions.entriesByTargetKey[key]?.status;
}
