import assert from "node:assert/strict";
import test from "node:test";
import {
  isDesktopFeatureSupported,
  normalizeDesktopFeatureKeys,
  parseDesktopFeatureAvailability
} from "./core.ts";

test("normalizes a unique deterministic feature key list", () => {
  assert.deepEqual(
    normalizeDesktopFeatureKeys(["workspace.z", "agent.preview"]),
    ["agent.preview", "workspace.z"]
  );
  assert.throws(() =>
    normalizeDesktopFeatureKeys(["workspace.z", "workspace.z"])
  );
  assert.throws(() => normalizeDesktopFeatureKeys([" invalid"]));
});

test("distinguishes a missing envelope from an authoritative empty list", () => {
  assert.equal(parseDesktopFeatureAvailability({}), null);
  assert.deepEqual(
    parseDesktopFeatureAvailability({ featureAvailability: { keys: [] } }),
    { keys: [] }
  );
  assert.throws(() =>
    parseDesktopFeatureAvailability({
      featureAvailability: { keys: [], unexpected: true }
    })
  );
});

test("queries feature support by exact case-sensitive key membership", () => {
  const snapshot = { keys: ["workspace.example"] };
  assert.equal(isDesktopFeatureSupported(snapshot, "workspace.example"), true);
  assert.equal(isDesktopFeatureSupported(snapshot, "Workspace.example"), false);
  assert.equal(isDesktopFeatureSupported(snapshot, "invalid key"), false);
});
