import assert from "node:assert/strict";
import test from "node:test";

import {
  createDesktopConnectorSelection,
  decodeDesktopConnectorSelections
} from "./desktopConnectorPrimaryCapabilityAdapter.ts";

test("Desktop alone maps the existing Connector prompt wire", () => {
  const restored = {
    id: "legacy-opaque-id",
    payload: { type: "connector", connectorKey: "calendar" }
  } as const;
  assert.deepEqual(decodeDesktopConnectorSelections([restored]), [
    { key: "calendar", selection: restored }
  ]);
  assert.deepEqual(createDesktopConnectorSelection("calendar"), {
    id: "calendar",
    payload: { type: "connector", connectorKey: "calendar" }
  });
});

test("Desktop bridge ignores opaque capabilities owned by another product", () => {
  assert.deepEqual(
    decodeDesktopConnectorSelections([
      { id: "other", payload: { type: "host-extension", key: "other" } }
    ]),
    []
  );
});
