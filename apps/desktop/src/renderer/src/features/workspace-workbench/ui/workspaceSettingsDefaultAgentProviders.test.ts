import assert from "node:assert/strict";
import test from "node:test";
import { migratedAgentGUIProviderIdentityCatalog } from "@tutti-os/agent-gui/provider-catalog";
import {
  normalizeWorkspaceSettingsDefaultAgentProvider,
  workspaceSettingsDefaultAgentProviders
} from "./workspaceSettingsDefaultAgentProviders.ts";

test("normalizes an ineligible provider to the highest-priority default", () => {
  const ineligibleProvider = migratedAgentGUIProviderIdentityCatalog.find(
    (entry) => !entry.desktop.defaultProviderEligible
  );
  assert.ok(ineligibleProvider, "registry must include an ineligible provider");
  assert.equal(
    normalizeWorkspaceSettingsDefaultAgentProvider(
      ineligibleProvider.providerId as Parameters<
        typeof normalizeWorkspaceSettingsDefaultAgentProvider
      >[0]
    ),
    workspaceSettingsDefaultAgentProviders[0]
  );
});
