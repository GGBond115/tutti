import assert from "node:assert/strict";
import test from "node:test";
import { resolveWorkspaceWindowChromeOptions } from "./workspaceWindowChrome.ts";

test("Windows workspace and agent modes use native window chrome", () => {
  assert.deepEqual(
    resolveWorkspaceWindowChromeOptions("win32", "workspace"),
    { autoHideMenuBar: false }
  );
  assert.deepEqual(
    resolveWorkspaceWindowChromeOptions("win32", "agent"),
    { autoHideMenuBar: false }
  );
});

test("frameless agent windows keep their existing non-Windows chrome", () => {
  assert.deepEqual(resolveWorkspaceWindowChromeOptions("darwin", "agent"), {
    frame: false,
    maximizable: false
  });
  assert.deepEqual(resolveWorkspaceWindowChromeOptions("linux", "agent"), {
    frame: false,
    maximizable: false
  });
  assert.deepEqual(
    resolveWorkspaceWindowChromeOptions("darwin", "workspace"),
    {}
  );
});
