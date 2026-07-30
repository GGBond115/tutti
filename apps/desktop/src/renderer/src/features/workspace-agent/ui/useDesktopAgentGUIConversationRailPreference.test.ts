import assert from "node:assert/strict";
import test from "node:test";
import {
  handleDesktopAgentGUIConversationRailToggle,
  rememberDesktopAgentGUIConversationRailPreference
} from "./useDesktopAgentGUIConversationRailPreference.ts";

test("records a device preference for a supported Desktop provider", async () => {
  const calls: unknown[][] = [];

  await rememberDesktopAgentGUIConversationRailPreference({
    conversationRailCollapsed: true,
    desktopPreferencesService: {
      rememberAgentGuiConversationRailCollapsed: async (...args) => {
        calls.push(args);
      }
    },
    provider: "codex",
    workspaceId: "workspace-1"
  });

  assert.deepEqual(calls, [["codex", true]]);
});

test("does not create a Desktop preference for an unknown provider", () => {
  let called = false;

  const result = rememberDesktopAgentGUIConversationRailPreference({
    conversationRailCollapsed: true,
    desktopPreferencesService: {
      rememberAgentGuiConversationRailCollapsed: async () => {
        called = true;
      }
    },
    provider: "unknown",
    workspaceId: "workspace-1"
  });

  assert.equal(result, null);
  assert.equal(called, false);
});

test("embedded Workbench toggles remember preference without writing node state twice", () => {
  const remembered: unknown[][] = [];
  let updateCalled = false;

  handleDesktopAgentGUIConversationRailToggle({
    conversationRailCollapsed: true,
    currentNodeState: {
      conversationRailCollapsed: false,
      provider: "codex"
    } as never,
    rememberConversationRailPreference: (...args) => {
      remembered.push(args);
    },
    stateOwner: "workbench-node-source",
    updateNode: () => {
      updateCalled = true;
    }
  });

  assert.deepEqual(remembered, [["codex", true]]);
  assert.equal(updateCalled, false);
});

test("standalone toggles use the surface state writer", () => {
  let nextCollapsed: boolean | null = null;

  handleDesktopAgentGUIConversationRailToggle({
    conversationRailCollapsed: true,
    currentNodeState: {
      conversationRailCollapsed: false,
      provider: "codex"
    } as never,
    rememberConversationRailPreference: () => {
      assert.fail("standalone preference is recorded by the state writer");
    },
    stateOwner: "surface",
    updateNode: (updater) => {
      nextCollapsed =
        updater({
          conversationRailCollapsed: false,
          provider: "codex"
        } as never).conversationRailCollapsed === true;
    }
  });

  assert.equal(nextCollapsed, true);
});
