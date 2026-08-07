import assert from "node:assert/strict";
import test from "node:test";
import { createDesktopCaptureProjectPreference } from "./desktopCaptureProjectPreference.ts";

test("desktop capture project preference is versioned and workspace scoped", () => {
  const values = new Map<string, string>();
  const preference = createDesktopCaptureProjectPreference({
    getItem: (key) => values.get(key) ?? null,
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, value)
  });

  assert.equal(preference.read("workspace-1"), null);
  preference.write("workspace-1", "/workspace/alpha");
  preference.write("workspace-2", "/workspace/beta");

  assert.equal(preference.read("workspace-1"), "/workspace/alpha");
  assert.equal(preference.read("workspace-2"), "/workspace/beta");

  preference.write("workspace-1", null);
  assert.equal(preference.read("workspace-1"), null);
  assert.equal(preference.read("workspace-2"), "/workspace/beta");
});
