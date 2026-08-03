import assert from "node:assert/strict";
import test from "node:test";
import type { AgentSessionRecording } from "@tutti-os/client-tuttid-ts";
import {
  agentSessionReplayBatchSelectionState,
  canSelectAgentSessionReplayBatch,
  selectedAgentSessionReplayCassetteIds
} from "./agentSessionReplayBatchSelection.ts";

test("batch selection requires two distinct eligible root Sessions", () => {
  assert.equal(
    canSelectAgentSessionReplayBatch([
      recording("recording-1", "cassette-1", "session-1"),
      recording("recording-2", "cassette-2", "session-1")
    ]),
    false
  );
  assert.equal(
    canSelectAgentSessionReplayBatch([
      recording("recording-1", "cassette-1", "session-1"),
      recording("recording-2", "cassette-2", "session-2")
    ]),
    true
  );
});

test("selected root Session disables only conflicting recordings", () => {
  const recordings = [
    recording("recording-1", "cassette-1", "session-1"),
    recording("recording-2", "cassette-2", "session-1"),
    recording("recording-3", "cassette-3", "session-2")
  ];
  const selected = new Set(["recording-1"]);

  assert.deepEqual(
    agentSessionReplayBatchSelectionState(recordings[0]!, recordings, selected),
    { disabledReason: null, selected: true }
  );
  assert.deepEqual(
    agentSessionReplayBatchSelectionState(recordings[1]!, recordings, selected),
    { disabledReason: "root-session-conflict", selected: false }
  );
  assert.deepEqual(
    agentSessionReplayBatchSelectionState(recordings[2]!, recordings, selected),
    { disabledReason: null, selected: false }
  );
});

test("batch Cassette order follows the recording list and excludes ineligible rows", () => {
  const recordings = [
    recording("recording-3", "cassette-3", "session-3"),
    {
      ...recording("recording-2", "cassette-2", "session-2"),
      status: "failed"
    },
    recording("recording-1", "cassette-1", "session-1")
  ] satisfies AgentSessionRecording[];

  assert.deepEqual(
    selectedAgentSessionReplayCassetteIds(
      recordings,
      new Set(["recording-1", "recording-2", "recording-3"])
    ),
    ["cassette-3", "cassette-1"]
  );
});

function recording(
  id: string,
  cassetteId: string,
  rootAgentSessionId: string
): AgentSessionRecording {
  return {
    agentTargetId: "local:codex",
    cassetteId,
    createdAtUnixMs: 1,
    directory: `/cassettes/${cassetteId}`,
    id,
    mode: "continue-session",
    name: id,
    rootAgentSessionId,
    status: "complete",
    updatedAtUnixMs: 1,
    workspaceId: "workspace-1"
  };
}
