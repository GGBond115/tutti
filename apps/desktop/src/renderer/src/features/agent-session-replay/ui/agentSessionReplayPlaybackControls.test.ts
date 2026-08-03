import assert from "node:assert/strict";
import test from "node:test";
import { resolveAgentSessionReplayControlAvailability } from "./agentSessionReplayPlaybackControls.ts";

test("enables transport controls only while replaying", () => {
  assert.deepEqual(
    resolveAgentSessionReplayControlAvailability({
      currentCheckpoint: 3,
      lastCheckpoint: 7,
      phase: "replaying",
      playbackActive: true,
      updating: false
    }),
    {
      canNext: true,
      canPause: true,
      canSetSpeed: true
    }
  );
});

test("disables every control until replay phase is known", () => {
  assert.equal(
    Object.values(
      resolveAgentSessionReplayControlAvailability({
        currentCheckpoint: 3,
        lastCheckpoint: 7,
        phase: undefined,
        playbackActive: false,
        updating: false
      })
    ).some(Boolean),
    false
  );
});

test("keeps transport controls disabled until playback becomes active", () => {
  const availability = resolveAgentSessionReplayControlAvailability({
    currentCheckpoint: 0,
    lastCheckpoint: 3,
    phase: "replaying",
    playbackActive: false,
    updating: false
  });

  assert.equal(availability.canNext, false);
  assert.equal(availability.canPause, false);
  assert.equal(availability.canSetSpeed, false);
});
