import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type {
  DesktopAgentSessionReplayPlayback,
  DesktopGetAgentSessionReplayPlaybackInput,
  DesktopSetAgentSessionReplayPlaybackInput
} from "../shared/contracts/ipc";

type AgentSessionReplayPlaybackClient = Pick<
  TuttidClient,
  | "getAgentSessionReplayTransportPlayback"
  | "updateAgentSessionReplayTransportPlayback"
>;

export interface AgentSessionReplayPlaybackAccess {
  get(
    input: DesktopGetAgentSessionReplayPlaybackInput
  ): Promise<DesktopAgentSessionReplayPlayback>;
  update(
    input: DesktopSetAgentSessionReplayPlaybackInput
  ): Promise<DesktopAgentSessionReplayPlayback>;
}

export function createAgentSessionReplayPlaybackAccess(
  client: AgentSessionReplayPlaybackClient
): AgentSessionReplayPlaybackAccess {
  return {
    async get(input) {
      const cassetteID = input.cassetteId.trim();
      if (!cassetteID) {
        return inactiveReplayPlayback();
      }
      const playback =
        await client.getAgentSessionReplayTransportPlayback(cassetteID);
      return playback
        ? activeReplayPlayback(playback)
        : inactiveReplayPlayback();
    },
    async update(input) {
      const cassetteID = input.cassetteId.trim();
      if (!cassetteID) {
        throw new Error("Replay Cassette id is unavailable");
      }
      const playback = await client.updateAgentSessionReplayTransportPlayback(
        cassetteID,
        replayPlaybackUpdateRequest(input)
      );
      return activeReplayPlayback(playback);
    }
  };
}

function inactiveReplayPlayback(): DesktopAgentSessionReplayPlayback {
  return {
    active: false,
    paused: false,
    playbackElapsedMs: 0,
    speed: 1,
    timingMode: "realtime"
  };
}

function activeReplayPlayback(
  playback: Awaited<
    ReturnType<
      AgentSessionReplayPlaybackClient["updateAgentSessionReplayTransportPlayback"]
    >
  >
): DesktopAgentSessionReplayPlayback {
  return {
    active: true,
    paused: playback.paused,
    playbackElapsedMs: playback.playbackElapsedMs,
    speed: playback.speed,
    timingMode: playback.timingMode
  };
}

function replayPlaybackUpdateRequest(
  input: DesktopSetAgentSessionReplayPlaybackInput
):
  | { command: "pause" | "resume" }
  | {
      command: "set-speed";
      speed: DesktopAgentSessionReplayPlayback["speed"];
    }
  | {
      command: "set-timing-mode";
      timingMode: DesktopAgentSessionReplayPlayback["timingMode"];
    } {
  switch (input.command) {
    case "pause":
    case "resume":
      return { command: input.command };
    case "set-speed":
      return { command: input.command, speed: input.speed };
    case "set-timing-mode":
      return { command: input.command, timingMode: input.timingMode };
  }
}
