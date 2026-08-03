import assert from "node:assert/strict";
import test from "node:test";
import { createAgentSessionReplayFolderAccess } from "./agentSessionReplayFolderAccess.ts";

test("reveals the prepared Replay Cassette directory", async () => {
  const requests: Array<{ cassetteIds: string[]; workspaceId: string }> = [];
  const revealed: string[] = [];
  const access = createAgentSessionReplayFolderAccess(
    {
      async prepareAgentSessionReplayWorkspace(workspaceId, request) {
        requests.push({ cassetteIds: request.cassetteIds, workspaceId });
        return {
          launches: [
            {
              cassetteDirectory: "/cassettes/cassette-1",
              cassetteId: "cassette-1",
              name: "Recording",
              rootAgentSessionId: "session-1"
            }
          ]
        };
      }
    },
    (path) => revealed.push(path)
  );

  await access.reveal({
    cassetteId: " cassette-1 ",
    workspaceId: " workspace-1 "
  });

  assert.deepEqual(requests, [
    { cassetteIds: ["cassette-1"], workspaceId: "workspace-1" }
  ]);
  assert.deepEqual(revealed, ["/cassettes/cassette-1"]);
});

test("rejects an unavailable Replay Cassette directory", async () => {
  const access = createAgentSessionReplayFolderAccess(
    {
      async prepareAgentSessionReplayWorkspace() {
        return { launches: [] };
      }
    },
    () => assert.fail("must not reveal an unavailable directory")
  );

  await assert.rejects(
    access.reveal({ cassetteId: "cassette-1", workspaceId: "workspace-1" }),
    /Replay Cassette directory is unavailable/u
  );
});
