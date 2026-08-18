import assert from "node:assert/strict";
import test from "node:test";

import {
  normalizeConnectorItems,
  type ConnectorComposerItem
} from "./ConnectorComposerMenu.tsx";

test("prioritizes selected then authorized connectors while preserving first identity", () => {
  const items: ConnectorComposerItem[] = [
    item(" github "),
    item("figma", "connected"),
    item("notion", "connected", true),
    item("github"),
    item("lark"),
    item("   ")
  ];

  assert.deepEqual(
    normalizeConnectorItems(items).map((entry) => entry.connectorKey),
    ["notion", "figma", "github", "lark"]
  );
});

function item(
  connectorKey: string,
  status: ConnectorComposerItem["status"] = "setup_required",
  selected = false
): ConnectorComposerItem {
  return {
    allowedActions:
      status === "connected"
        ? ["details", "select", "remove_selection"]
        : ["details", "remove_selection"],
    connectorKey,
    name: connectorKey,
    selected,
    status
  };
}
