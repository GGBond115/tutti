import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
  createSubmenuGraceCloseController,
  shouldShowDesktopAgentConfigSystemActions
} from "./desktopAgentConfigSystemActionsModel.ts";

const directory = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(
  resolve(directory, "DesktopAgentConfigSystemActions.tsx"),
  "utf8"
);
const workbenchSource = readFileSync(
  resolve(directory, "DesktopAgentGUIWorkbenchBody.tsx"),
  "utf8"
);

test("Agent config system actions stay hidden in OS mode", () => {
  assert.match(workbenchSource, /resolveDesktopWorkspaceUiMode\(/);
  assert.equal(shouldShowDesktopAgentConfigSystemActions("os"), false);
  assert.equal(shouldShowDesktopAgentConfigSystemActions("agent"), true);
  assert.match(
    workbenchSource,
    /shouldShowDesktopAgentConfigSystemActions\(\s*workspaceUiMode\s*\)/
  );
  assert.match(
    workbenchSource,
    /\? renderDesktopAgentConfigSystemActions\s+: undefined/
  );
});

test("Agent config log export uses a hover submenu", () => {
  assert.match(source, /<DropdownMenu[\s\S]*open=\{exportMenuOpen\}/);
  assert.match(source, /createSubmenuGraceCloseController\(\{/);
  assert.match(
    source,
    /onPointerLeave=\{\(\) => exportMenuGraceClose\.schedule\(\)\}/
  );
  assert.match(
    source,
    /onPointerEnter=\{\(\) => exportMenuGraceClose\.cancel\(\)\}/
  );
  assert.match(source, /modal=\{false\}/);
  assert.doesNotMatch(source, /DropdownMenuSub/);
});

test("Agent config log export grace close is canceled by submenu entry", () => {
  const timeoutSignals: AbortController[] = [];
  let closeCalls = 0;
  const controller = createSubmenuGraceCloseController({
    close: () => {
      closeCalls += 1;
    },
    createTimeoutSignal: () => {
      const timeout = new AbortController();
      timeoutSignals.push(timeout);
      return timeout.signal;
    }
  });

  controller.schedule();
  controller.cancel();
  timeoutSignals[0]?.abort();
  assert.equal(closeCalls, 0);

  controller.schedule();
  timeoutSignals[1]?.abort();
  assert.equal(closeCalls, 1);
});
