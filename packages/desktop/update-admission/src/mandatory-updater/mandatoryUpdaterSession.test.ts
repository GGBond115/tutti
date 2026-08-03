import assert from "node:assert/strict";
import test from "node:test";
import type { DesktopUpdateState } from "../contracts/index.ts";
import {
  createMandatoryUpdaterLeaseManager,
  MandatoryUpdateTargetError
} from "./mandatoryUpdaterSession.ts";

function updateState(
  status: DesktopUpdateState["status"],
  latestVersion: string | null
): DesktopUpdateState {
  return {
    channel: "stable",
    checkedAt: null,
    currentVersion: "1.0.0",
    downloadedBytes: null,
    downloadPercent: null,
    latestVersion,
    message: null,
    policy: "prompt",
    releaseDate: null,
    releaseName: null,
    releaseNotesUrl: null,
    status,
    totalBytes: null
  };
}

test("owns updater access and restores the captured configuration", async () => {
  let state = updateState("available", "2.0.0");
  let restored: string | null = null;
  const manager = createMandatoryUpdaterLeaseManager({
    captureNormalConfiguration: () => "normal",
    downloadUpdate: async () => {
      state = updateState("downloaded", "2.0.0");
      return state;
    },
    getState: () => state,
    installUpdate: async () => undefined,
    prepareMandatoryUpdate: async () => state,
    restoreNormalConfiguration: async (configuration) => {
      restored = configuration;
    },
    suspendNormalUpdates: async () => undefined
  });

  const session = await manager.acquire({
    channel: "stable",
    minimumVersion: "2.0.0",
    policyRevision: "revision-1"
  });
  assert.throws(() => manager.assertAccess());
  assert.equal((await session.prepare()).status, "available");
  assert.equal((await session.downloadUpdate()).status, "downloaded");
  await session.installUpdate();
  await session.release();
  assert.equal(restored, "normal");
  manager.assertAccess();
});

test("retargets an active lease without losing its captured normal configuration", async () => {
  let state = updateState("available", "2.1.0");
  const preparedChannels: string[] = [];
  let restored: string | null = null;
  const manager = createMandatoryUpdaterLeaseManager({
    captureNormalConfiguration: () => "normal",
    downloadUpdate: async () => state,
    getState: () => state,
    installUpdate: async () => undefined,
    prepareMandatoryUpdate: async (configuration) => {
      preparedChannels.push(configuration.channel);
      return state;
    },
    restoreNormalConfiguration: async (configuration) => {
      restored = configuration;
    },
    suspendNormalUpdates: async () => undefined
  });
  const session = await manager.acquire({
    channel: "stable",
    minimumVersion: "2.0.0",
    policyRevision: "revision-1"
  });

  session.retarget({
    channel: "rc",
    minimumVersion: "2.1.0-rc.1",
    policyRevision: "revision-2"
  });
  await session.prepare();
  await session.release();

  assert.deepEqual(preparedChannels, ["rc"]);
  assert.equal(restored, "normal");
});

test("retains updater ownership until normal configuration restoration finishes", async () => {
  const state = updateState("available", "2.0.0");
  let finishRestore!: () => void;
  const restoreStarted = new Promise<void>((resolve) => {
    finishRestore = resolve;
  });
  const manager = createMandatoryUpdaterLeaseManager({
    captureNormalConfiguration: () => "normal",
    downloadUpdate: async () => state,
    getState: () => state,
    installUpdate: async () => undefined,
    prepareMandatoryUpdate: async () => state,
    restoreNormalConfiguration: async () => restoreStarted,
    suspendNormalUpdates: async () => undefined
  });
  const session = await manager.acquire({
    channel: "stable",
    minimumVersion: "2.0.0",
    policyRevision: "revision-1"
  });

  const release = session.release();
  assert.throws(() => manager.assertAccess());
  await assert.rejects(
    manager.acquire({
      channel: "stable",
      minimumVersion: "2.0.0",
      policyRevision: "revision-2"
    })
  );
  finishRestore();
  await release;
  manager.assertAccess();
});

test("rejects an updater target before installation", async () => {
  const state = updateState("available", "1.9.9");
  const manager = createMandatoryUpdaterLeaseManager({
    captureNormalConfiguration: () => null,
    downloadUpdate: async () => state,
    getState: () => state,
    installUpdate: async () => assert.fail("install must not run"),
    prepareMandatoryUpdate: async () => state,
    restoreNormalConfiguration: async () => undefined,
    suspendNormalUpdates: async () => undefined
  });
  const session = await manager.acquire({
    channel: "stable",
    minimumVersion: "2.0.0",
    policyRevision: "revision-1"
  });
  await assert.rejects(session.prepare(), MandatoryUpdateTargetError);
});

test("releases ownership when suspension fails", async () => {
  const state = updateState("idle", null);
  let fail = true;
  const manager = createMandatoryUpdaterLeaseManager({
    captureNormalConfiguration: () => null,
    downloadUpdate: async () => state,
    getState: () => state,
    installUpdate: async () => undefined,
    prepareMandatoryUpdate: async () => state,
    restoreNormalConfiguration: async () => undefined,
    suspendNormalUpdates: async () => {
      if (fail) {
        fail = false;
        throw new Error("suspend failed");
      }
    }
  });
  await assert.rejects(
    manager.acquire({
      channel: "stable",
      minimumVersion: "2.0.0",
      policyRevision: "revision-1"
    })
  );
  await manager.acquire({
    channel: "stable",
    minimumVersion: "2.0.0",
    policyRevision: "revision-2"
  });
});
