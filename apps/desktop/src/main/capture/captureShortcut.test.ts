import assert from "node:assert/strict";
import test from "node:test";
import {
  desktopCaptureAccelerator,
  desktopCaptureShortcutKeys
} from "./captureShortcut.ts";

test("desktop capture uses a discoverable shortcut with at most three keys", () => {
  assert.equal(desktopCaptureAccelerator, "CommandOrControl+Shift+S");
  assert.ok(desktopCaptureShortcutKeys.length <= 3);
});
