import assert from "node:assert/strict";
import test from "node:test";
import {
  getWorkspaceModelPlanTemplateGroup,
  workspaceModelPlanUsesNativeLogin
} from "./workspaceModelPlanTemplates.ts";

test("legacy official subscriptions keep their native-login behavior", () => {
  const group = getWorkspaceModelPlanTemplateGroup("official_subscription");

  assert.ok(group);
  assert.equal(workspaceModelPlanUsesNativeLogin(group.kind), true);
});
