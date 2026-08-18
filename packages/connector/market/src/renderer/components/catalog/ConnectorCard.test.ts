import assert from "node:assert/strict";
import test from "node:test";

import { connectorCardActionStartsInstallation } from "./connectorCardAction.ts";

test("routes connector install and update actions directly to installation", () => {
  assert.equal(connectorCardActionStartsInstallation("install"), true);
  assert.equal(connectorCardActionStartsInstallation("update"), true);
  assert.equal(connectorCardActionStartsInstallation("authorize"), false);
  assert.equal(connectorCardActionStartsInstallation("manage"), false);
});
