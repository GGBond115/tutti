import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
  createSubmenuGraceCloseController,
  shouldKeepOpenSubmenuOnTriggerKeyDown,
  shouldKeepOpenSubmenuOnTriggerPointerDown,
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
  assert.match(source, /onClick=\{\(\) => setExportMenuOpen\(true\)\}/);
  assert.match(
    source,
    /onPointerLeave=\{\(\) => exportMenuGraceClose\.schedule\(\)\}/
  );
  assert.match(
    source,
    /onPointerEnter=\{\(\) => exportMenuGraceClose\.cancel\(\)\}/
  );
  assert.match(
    source,
    /<DropdownMenuContent[\s\S]*data-agent-gui-config-owned-layer=""/
  );
  assert.equal(
    source.match(/<DropdownMenuItem[\s\S]*?onClick=/g)?.length,
    4
  );
  assert.doesNotMatch(source, /<DropdownMenuItem[\s\S]*?onSelect=/);
  assert.match(source, /modal=\{false\}/);
  assert.match(source, /<ArrowRightIcon/);
  assert.match(source, /event\.key === "ArrowRight"/);
  assert.match(source, /event\.key === "ArrowLeft"/);
  assert.match(source, /exportMenuTriggerRef\.current\?\.focus\(\)/);
  assert.match(source, /shouldKeepOpenSubmenuOnTriggerPointerDown\(\{/);
  assert.doesNotMatch(source, /DropdownMenuSub/);
});

test("an already open export menu does not toggle closed on click or activation", () => {
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerPointerDown({
      button: 0,
      ctrlKey: false,
      open: true
    }),
    true
  );
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerPointerDown({
      button: 0,
      ctrlKey: false,
      open: false
    }),
    false
  );
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerPointerDown({
      button: 2,
      ctrlKey: false,
      open: true
    }),
    false
  );
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerKeyDown({ key: "Enter", open: true }),
    true
  );
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerKeyDown({ key: " ", open: true }),
    true
  );
  assert.equal(
    shouldKeepOpenSubmenuOnTriggerKeyDown({ key: "Enter", open: false }),
    false
  );
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
