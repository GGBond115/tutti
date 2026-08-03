import assert from "node:assert/strict";
import test from "node:test";
import { createAgentSessionReplayPlaybackAccess } from "./agentSessionReplayPlaybackAccess.ts";

const playback = {
  drained: false,
  paused: true,
  playbackElapsedMs: 42,
  providerConnections: [],
  speed: 2 as const,
  timingMode: "fast-forward" as const
};

test("replay playback access projects unavailable and active transport state", async () => {
  const requestedCassetteIDs: string[] = [];
  const access = createAgentSessionReplayPlaybackAccess({
    async getAgentSessionReplayTransportPlayback(cassetteID) {
      requestedCassetteIDs.push(cassetteID);
      return cassetteID === "cassette-active" ? playback : null;
    },
    async updateAgentSessionReplayTransportPlayback() {
      return playback;
    }
  });

  assert.deepEqual(await access.get({ cassetteId: " " }), {
    active: false,
    paused: false,
    playbackElapsedMs: 0,
    speed: 1,
    timingMode: "realtime"
  });
  assert.deepEqual(await access.get({ cassetteId: " cassette-active " }), {
    active: true,
    paused: true,
    playbackElapsedMs: 42,
    speed: 2,
    timingMode: "fast-forward"
  });
  assert.deepEqual(requestedCassetteIDs, ["cassette-active"]);
});

test("replay playback access sends a narrow cassette-scoped update", async () => {
  const updates: unknown[] = [];
  const access = createAgentSessionReplayPlaybackAccess({
    async getAgentSessionReplayTransportPlayback() {
      return null;
    },
    async updateAgentSessionReplayTransportPlayback(cassetteID, request) {
      updates.push({ cassetteID, request });
      return playback;
    }
  });

  assert.deepEqual(
    await access.update({
      command: "set-speed",
      cassetteId: " cassette-a ",
      speed: 2
    }),
    {
      active: true,
      paused: true,
      playbackElapsedMs: 42,
      speed: 2,
      timingMode: "fast-forward"
    }
  );
  assert.deepEqual(updates, [
    {
      request: { command: "set-speed", speed: 2 },
      cassetteID: "cassette-a"
    }
  ]);
  await assert.rejects(
    access.update({ command: "pause", cassetteId: " " }),
    /Replay Cassette id is unavailable/u
  );
});
