import assert from "node:assert/strict";
import test from "node:test";
import { createAgentSessionReplayLauncher } from "./agentSessionReplayLauncher.ts";

test("batch prepares and launches all Cassettes once without Renderer status writes", async () => {
  const events: string[] = [];
  const launcher = createAgentSessionReplayLauncher({
    createLaunchId: () => "launch-1",
    createReplayWorkspaceId: () => "replay-workspace-1",
    runtimeApi: {
      async launchAgentSessionReplay(input) {
        events.push(
          `launch:${input.playbackMode}:${input.cassettes.map((cassette) => cassette.cassetteId).join(",")}`
        );
        return {
          launchId: input.launchId,
          cassetteIds: input.cassettes.map((cassette) => cassette.cassetteId),
          workspaceId: input.workspaceId
        };
      },
      async waitForAgentSessionReplay(input) {
        events.push(`wait:${input.cassetteId}`);
        return input;
      }
    },
    service: {
      async prepareReplayWorkspace(cassetteIds) {
        events.push(`prepare:${cassetteIds.join(",")}`);
        return {
          launches: cassetteIds.map((cassetteId, index) => ({
            cassetteDirectory: `/cassettes/${cassetteId}`,
            cassetteId,
            rootAgentSessionId: `session-${index + 1}`
          }))
        };
      }
    }
  });

  const launched = await launcher.launch(
    ["cassette-1", "cassette-2"],
    "manual"
  );
  await launched.completion;
  assert.deepEqual(events, [
    "prepare:cassette-1,cassette-2",
    "launch:manual:cassette-1,cassette-2",
    "wait:cassette-1",
    "wait:cassette-2"
  ]);
});

test("default launch id keeps the Crypto method receiver", async () => {
  const originalCrypto = Object.getOwnPropertyDescriptor(globalThis, "crypto");
  const fakeCrypto = {
    randomUUID() {
      assert.equal(this, fakeCrypto);
      return "launch-bound";
    }
  };
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: fakeCrypto
  });
  let launchId = "";
  try {
    const launcher = createAgentSessionReplayLauncher({
      createReplayWorkspaceId: () => "replay-workspace-1",
      runtimeApi: {
        async launchAgentSessionReplay(input) {
          launchId = input.launchId;
          return {
            launchId: input.launchId,
            cassetteIds: input.cassettes.map((cassette) => cassette.cassetteId),
            workspaceId: input.workspaceId
          };
        },
        async waitForAgentSessionReplay(input) {
          return input;
        }
      },
      service: {
        async prepareReplayWorkspace() {
          return {
            launches: [
              {
                cassetteDirectory: "/cassettes/1",
                cassetteId: "cassette-1",
                rootAgentSessionId: "session-1"
              }
            ]
          };
        }
      }
    });

    const launched = await launcher.launch(["cassette-1"], "automatic");
    await launched.completion;
  } finally {
    if (originalCrypto) {
      Object.defineProperty(globalThis, "crypto", originalCrypto);
    } else {
      delete (globalThis as { crypto?: Crypto }).crypto;
    }
  }
  assert.equal(launchId, "launch-bound");
});

test("does not wait when the Main launch fails", async () => {
  let waited = false;
  const launcher = createAgentSessionReplayLauncher({
    createLaunchId: () => "launch-1",
    createReplayWorkspaceId: () => "replay-workspace-1",
    runtimeApi: {
      async launchAgentSessionReplay() {
        throw new Error("launch failed");
      },
      async waitForAgentSessionReplay(input) {
        waited = true;
        return input;
      }
    },
    service: {
      async prepareReplayWorkspace() {
        return {
          launches: [
            {
              cassetteDirectory: "/cassettes/1",
              cassetteId: "cassette-1",
              rootAgentSessionId: "session-1"
            }
          ]
        };
      }
    }
  });

  await assert.rejects(
    launcher.launch(["cassette-1"], "automatic"),
    /launch failed/u
  );
  assert.equal(waited, false);
});

test("rejects empty and duplicate Cassette batches before preparation", async () => {
  let prepared = false;
  const launcher = createAgentSessionReplayLauncher({
    runtimeApi: {
      async launchAgentSessionReplay() {
        throw new Error("unexpected launch");
      },
      async waitForAgentSessionReplay(input) {
        return input;
      }
    },
    service: {
      async prepareReplayWorkspace() {
        prepared = true;
        throw new Error("unexpected preparation");
      }
    }
  });

  await assert.rejects(
    launcher.launch([], "automatic"),
    /requires at least one Cassette/u
  );
  await assert.rejects(
    launcher.launch(["cassette-1", "cassette-1"], "automatic"),
    /identities must be unique/u
  );
  assert.equal(prepared, false);
});
