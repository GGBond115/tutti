import assert from "node:assert/strict";
import test from "node:test";
import { tuttiAgentSessionComposerSettingsFromActivity } from "./composerSettings.ts";

test("projects only settings supported by the tuttid composer contract", () => {
  assert.deepEqual(
    tuttiAgentSessionComposerSettingsFromActivity({
      browserUse: false,
      computerUse: false,
      model: null,
      permissionModeId: "auto",
      planMode: true,
      reasoningEffort: "high",
      speed: "fast"
    }),
    {
      browserUse: false,
      model: null,
      permissionModeId: "auto",
      planMode: true,
      reasoningEffort: "high",
      speed: "fast"
    }
  );
});

test("does not invent a tuttid request field for computer use", () => {
  assert.deepEqual(
    tuttiAgentSessionComposerSettingsFromActivity({ computerUse: true }),
    {}
  );
});
