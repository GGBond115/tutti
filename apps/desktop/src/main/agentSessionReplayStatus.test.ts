import assert from "node:assert/strict";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  createAgentSessionReplayControlWriter,
  createAgentSessionReplayCassetteStatusWriter,
  readAgentSessionReplayStatus
} from "./agentSessionReplayStatus.ts";

test("hides absent or invalid replay status", async () => {
  assert.deepEqual(await readAgentSessionReplayStatus({ cassetteId: "" }), {
    active: false
  });
  assert.deepEqual(
    await readAgentSessionReplayStatus(
      { cassetteId: "cassette-1" },
      "/missing/cassette-status.json"
    ),
    { active: false }
  );
});

test("reads only the requested Replay Cassette status", async () => {
  const directory = await mkdtemp(join(tmpdir(), "replay-cassette-status-"));
  const path = join(directory, "cassette-status.json");
  const writer = createAgentSessionReplayCassetteStatusWriter(path);
  await Promise.all([
    writer.write("cassette-1", {
      active: true,
      currentCheckpoint: 2,
      phase: "replaying",
      totalCheckpoints: 4
    }),
    writer.write("cassette-2", {
      active: true,
      errorMessage: "cassette two failed",
      phase: "failed"
    })
  ]);

  assert.deepEqual(
    await readAgentSessionReplayStatus({ cassetteId: "cassette-1" }, path),
    {
      active: true,
      currentCheckpoint: 2,
      phase: "replaying",
      totalCheckpoints: 4
    }
  );
  assert.deepEqual(
    await readAgentSessionReplayStatus({ cassetteId: "cassette-2" }, path),
    {
      active: true,
      errorMessage: "cassette two failed",
      phase: "failed"
    }
  );
  assert.deepEqual(
    await readAgentSessionReplayStatus({ cassetteId: "cassette-3" }, path),
    { active: false }
  );
});

test("writes replay controls atomically with increasing revisions", async () => {
  const directory = await mkdtemp(join(tmpdir(), "replay-control-"));
  const path = join(directory, "replay-control.json");
  const writeControl = createAgentSessionReplayControlWriter(path);

  await Promise.all([
    writeControl({ command: "next-checkpoint", cassetteId: "cassette-1" }),
    writeControl({ command: "pause", cassetteId: "cassette-2" }),
    writeControl({ command: "resume", cassetteId: "cassette-1" })
  ]);

  assert.deepEqual(JSON.parse(await readFile(path, "utf8")), {
    schemaVersion: 2,
    cassettes: {
      "cassette-1": {
        command: "resume",
        revision: 2
      },
      "cassette-2": {
        command: "pause",
        revision: 1
      }
    }
  });
});
