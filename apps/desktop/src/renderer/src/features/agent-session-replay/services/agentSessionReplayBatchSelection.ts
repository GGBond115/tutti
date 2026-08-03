import type { AgentSessionRecording } from "@tutti-os/client-tuttid-ts";

export type AgentSessionReplayBatchSelectionDisabledReason =
  | "ineligible"
  | "root-session-conflict"
  | null;

export interface AgentSessionReplayBatchSelectionState {
  disabledReason: AgentSessionReplayBatchSelectionDisabledReason;
  selected: boolean;
}

export function canSelectAgentSessionReplayBatch(
  recordings: readonly AgentSessionRecording[]
): boolean {
  return (
    new Set(
      recordings
        .filter(isAgentSessionReplayBatchEligible)
        .map((recording) => recording.rootAgentSessionId!.trim())
    ).size >= 2
  );
}

export function agentSessionReplayBatchSelectionState(
  recording: AgentSessionRecording,
  recordings: readonly AgentSessionRecording[],
  selectedRecordingIds: ReadonlySet<string>
): AgentSessionReplayBatchSelectionState {
  const selected = selectedRecordingIds.has(recording.id);
  if (!isAgentSessionReplayBatchEligible(recording)) {
    return { disabledReason: "ineligible", selected };
  }
  const selectedRootSessionIds = new Set(
    recordings
      .filter(
        (candidate) =>
          candidate.id !== recording.id &&
          selectedRecordingIds.has(candidate.id) &&
          isAgentSessionReplayBatchEligible(candidate)
      )
      .map((candidate) => candidate.rootAgentSessionId!.trim())
  );
  return {
    disabledReason: selectedRootSessionIds.has(
      recording.rootAgentSessionId!.trim()
    )
      ? "root-session-conflict"
      : null,
    selected
  };
}

export function selectedAgentSessionReplayCassetteIds(
  recordings: readonly AgentSessionRecording[],
  selectedRecordingIds: ReadonlySet<string>
): string[] {
  return recordings
    .filter(
      (recording) =>
        selectedRecordingIds.has(recording.id) &&
        isAgentSessionReplayBatchEligible(recording)
    )
    .map((recording) => recording.cassetteId!.trim());
}

function isAgentSessionReplayBatchEligible(
  recording: AgentSessionRecording
): boolean {
  return (
    recording.status === "complete" &&
    Boolean(recording.cassetteId?.trim()) &&
    Boolean(recording.rootAgentSessionId?.trim())
  );
}
