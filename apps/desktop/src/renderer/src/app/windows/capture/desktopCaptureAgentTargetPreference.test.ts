import assert from "node:assert/strict";
import test from "node:test";
import { createDesktopCaptureAgentTargetPreference } from "./desktopCaptureAgentTargetPreference.ts";

test("desktop capture Agent Target preference is versioned and workspace scoped", () => {
  const values = new Map<string, string>();
  const preference = createDesktopCaptureAgentTargetPreference({
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value)
  });

  assert.equal(preference.read("workspace-1"), null);
  preference.write("workspace-1", "agent-codex");
  preference.write("workspace-2", "agent-tutti");

  assert.equal(preference.read("workspace-1"), "agent-codex");
  assert.equal(preference.read("workspace-2"), "agent-tutti");
});
