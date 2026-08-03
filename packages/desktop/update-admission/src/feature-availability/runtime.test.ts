import assert from "node:assert/strict";
import test from "node:test";
import type { DesktopUpdateAdmissionSnapshot } from "../contracts/index.ts";
import { createDesktopFeatureAvailabilityRuntime } from "./runtime.ts";

const identity = {
  architecture: "arm64",
  currentVersion: "1.0.0",
  platform: "macos",
  product: "tutti-desktop"
} as const;

function daemonSnapshot(
  featureAvailability: DesktopUpdateAdmissionSnapshot<"tutti-desktop">["featureAvailability"]
): DesktopUpdateAdmissionSnapshot<"tutti-desktop"> {
  return {
    featureAvailability,
    identity,
    lastAttemptAt: "2026-08-02T09:00:00Z",
    nextForegroundCheckAt: "2026-08-02T09:30:00Z",
    policy: {
      response: {
        channel: "stable",
        decision: "allowed",
        minimumVersion: "1.0.0",
        policyRevision: "v1",
        reason: "meetsMinimum"
      },
      status: "resolved"
    }
  };
}

test("feature runtime projects daemon-owned cache snapshot without disk access", () => {
  const runtime = createDesktopFeatureAvailabilityRuntime({
    identity,
    logger: { error() {}, info() {} }
  });
  const observed: Array<readonly string[]> = [];
  runtime.subscribe((snapshot) => observed.push(snapshot.keys));

  runtime.acceptDaemonSnapshot(
    daemonSnapshot({
      fetchedAt: "2026-08-02T08:30:00Z",
      keys: ["workspace.example"],
      policyRevision: "v1",
      source: "cache"
    })
  );

  assert.deepEqual(runtime.getSnapshot(), {
    ...identity,
    fetchedAt: "2026-08-02T08:30:00Z",
    keys: ["workspace.example"],
    policyRevision: "v1",
    source: "cache"
  });
  assert.equal(runtime.isSupported("workspace.example"), true);
  assert.deepEqual(observed, [["workspace.example"]]);
});

test("feature runtime rejects daemon snapshots for another identity", () => {
  const runtime = createDesktopFeatureAvailabilityRuntime({
    identity,
    logger: { error() {}, info() {} }
  });
  const snapshot = daemonSnapshot({
    fetchedAt: null,
    keys: [],
    policyRevision: null,
    source: "empty"
  });

  assert.throws(
    () =>
      runtime.acceptDaemonSnapshot({
        ...snapshot,
        identity: { ...identity, currentVersion: "2.0.0" }
      }),
    /daemon identity mismatch/
  );
});

test("feature runtime stops notifying after disposal", () => {
  const runtime = createDesktopFeatureAvailabilityRuntime({
    identity,
    logger: { error() {}, info() {} }
  });
  let notifications = 0;
  runtime.subscribe(() => {
    notifications += 1;
  });
  runtime.dispose();
  runtime.acceptDaemonSnapshot(
    daemonSnapshot({
      fetchedAt: null,
      keys: ["workspace.example"],
      policyRevision: null,
      source: "empty"
    })
  );
  assert.equal(notifications, 0);
  assert.deepEqual(runtime.getSnapshot().keys, []);
});
