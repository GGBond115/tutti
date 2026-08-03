import type { DesktopAgentSessionReplayPhase } from "@shared/contracts/ipc";

export function resolveAgentSessionReplayControlAvailability(input: {
  currentCheckpoint: number;
  lastCheckpoint: number;
  phase: DesktopAgentSessionReplayPhase | undefined;
  playbackActive: boolean;
  updating: boolean;
}): {
  canNext: boolean;
  canPause: boolean;
  canSetSpeed: boolean;
} {
  const replaying = input.phase === "replaying";
  const transportReady = !input.updating && replaying && input.playbackActive;
  return {
    canNext: transportReady && input.currentCheckpoint < input.lastCheckpoint,
    canPause: transportReady,
    canSetSpeed: transportReady
  };
}
