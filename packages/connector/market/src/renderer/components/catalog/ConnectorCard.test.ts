import assert from "node:assert/strict";
import test from "node:test";

import {
  connectorCardActionStartsInstallation,
  connectorCardShowsInstallationProgress
} from "./connectorCardAction.ts";

test("routes connector install and update actions directly to installation", () => {
  assert.equal(connectorCardActionStartsInstallation("install"), true);
  assert.equal(connectorCardActionStartsInstallation("update"), true);
  assert.equal(connectorCardActionStartsInstallation("authorize"), false);
  assert.equal(connectorCardActionStartsInstallation("disconnect"), false);
});

test("shows installation progress for every converging install stage", () => {
  assert.equal(
    connectorCardShowsInstallationProgress("installation_converging"),
    true
  );
  assert.equal(
    connectorCardShowsInstallationProgress("runtime_converging"),
    false
  );
  assert.equal(connectorCardShowsInstallationProgress(undefined), false);
});
