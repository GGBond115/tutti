import assert from "node:assert/strict";
import test from "node:test";

import { shouldShowReleaseNotesAction } from "./appUpdateStatusPresentation.ts";

test("release notes are available for download and install actions when enabled", () => {
  assert.equal(shouldShowReleaseNotesAction("download", true), true);
  assert.equal(shouldShowReleaseNotesAction("install", true), true);
  assert.equal(shouldShowReleaseNotesAction("retry", true), false);
});

test("release builds hide release notes for every update action", () => {
  assert.equal(shouldShowReleaseNotesAction("download", false), false);
  assert.equal(shouldShowReleaseNotesAction("install", false), false);
});
