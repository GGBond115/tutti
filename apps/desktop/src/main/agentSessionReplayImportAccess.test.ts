import assert from "node:assert/strict";
import test from "node:test";
import { createAgentSessionReplayImportAccess } from "./agentSessionReplayImportAccess.ts";

test("imports selected cassette files and directories as one batch", async () => {
  const requests: Array<{
    sourceDirectories: string[];
    workspaceId: string;
  }> = [];
  const access = createAgentSessionReplayImportAccess(
    {
      async importAgentSessionCassettes(workspaceId, request) {
        requests.push({
          sourceDirectories: request.sourceDirectories,
          workspaceId
        });
        return {
          failures: [{ code: "invalid", sourceDirectory: "/tmp/bad" }],
          recordings: [{ id: "recording-1" }, { id: "recording-2" }]
        } as never;
      }
    },
    {
      async selectUploadFiles() {
        return ["/tmp/cassette-a", "/tmp/cassette-b/cassette.json"];
      }
    },
    async (path) => !path.endsWith(".json")
  );

  const result = await access.importCassettes({ workspaceId: " workspace-1 " });

  assert.deepEqual(result, {
    canceled: false,
    failedCount: 1,
    importedCount: 2
  });
  assert.deepEqual(requests, [
    {
      sourceDirectories: ["/tmp/cassette-a", "/tmp/cassette-b"],
      workspaceId: "workspace-1"
    }
  ]);
});

test("does not call the daemon when cassette selection is canceled", async () => {
  const access = createAgentSessionReplayImportAccess(
    {
      async importAgentSessionCassettes() {
        assert.fail("daemon import must not run");
      }
    },
    {
      async selectUploadFiles() {
        return [];
      }
    }
  );

  assert.deepEqual(
    await access.importCassettes({ workspaceId: "workspace-1" }),
    {
      canceled: true,
      failedCount: 0,
      importedCount: 0
    }
  );
});
