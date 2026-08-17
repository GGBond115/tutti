import assert from "node:assert/strict";
import test from "node:test";
import type { PutDesktopPreferencesRequest } from "@tutti-os/client-tuttid-ts";
import { defaultDesktopWorkbenchShortcuts } from "../shared/preferences/index.ts";
import { createDesktopHostPreferencesState } from "./desktopHostPreferences.ts";
import type { DesktopLogger } from "./logging.ts";

const standaloneAgentModeFlag = "workspace.standaloneAgentMode";

test("desktop preferences keep the legacy OS fallback when the initial identity read fails", async () => {
  let putCalls = 0;
  const state = await createDesktopHostPreferencesState({
    fallbackLocale: "en",
    logger: createLogger(),
    tuttidClient: {
      async getDesktopPreferences() {
        throw new Error("read unavailable");
      },
      async putDesktopPreferences() {
        putCalls++;
        throw new Error("putDesktopPreferences should not be called");
      }
    }
  });

  assert.equal(state.getFeatureFlags()[standaloneAgentModeFlag], undefined);
  assert.equal(putCalls, 0);
});

test("desktop preferences keep a fresh Agent default when initialization fails before commit", async () => {
  let getCalls = 0;
  let capturedRequest: PutDesktopPreferencesRequest | undefined;
  const state = await createDesktopHostPreferencesState({
    fallbackLocale: "en",
    logger: createLogger(),
    tuttidClient: {
      async getDesktopPreferences() {
        getCalls++;
        return {
          initialized: false,
          preferences: createPreferences({
            [standaloneAgentModeFlag]: true
          })
        };
      },
      async putDesktopPreferences(request) {
        capturedRequest = request;
        throw new Error("write failed before commit");
      }
    }
  });

  assert.equal(capturedRequest?.writeMode, "initializeIfAbsent");
  assert.equal(state.getFeatureFlags()[standaloneAgentModeFlag], true);
  assert.equal(getCalls, 2);
});

test("desktop preferences reconcile a lost initialization response with the committed Agent preference", async () => {
  let getCalls = 0;
  const state = await createDesktopHostPreferencesState({
    fallbackLocale: "en",
    logger: createLogger(),
    tuttidClient: {
      async getDesktopPreferences() {
        getCalls++;
        return getCalls === 1
          ? {
              initialized: false,
              preferences: createPreferences({
                [standaloneAgentModeFlag]: true
              })
            }
          : {
              initialized: true,
              preferences: createPreferences({
                [standaloneAgentModeFlag]: true
              })
            };
      },
      async putDesktopPreferences() {
        throw new Error("response lost after commit");
      }
    }
  });

  assert.equal(state.getFeatureFlags()[standaloneAgentModeFlag], true);
  assert.equal(getCalls, 2);
});

test("desktop preferences preserve a concurrent existing OS preference after an ambiguous initialization", async () => {
  let getCalls = 0;
  const state = await createDesktopHostPreferencesState({
    fallbackLocale: "en",
    logger: createLogger(),
    tuttidClient: {
      async getDesktopPreferences() {
        getCalls++;
        return getCalls === 1
          ? {
              initialized: false,
              preferences: createPreferences({
                [standaloneAgentModeFlag]: true
              })
            }
          : {
              initialized: true,
              preferences: createPreferences({
                [standaloneAgentModeFlag]: false
              })
            };
      },
      async putDesktopPreferences() {
        throw new Error("initialization outcome unknown");
      }
    }
  });

  assert.equal(state.getFeatureFlags()[standaloneAgentModeFlag], false);
  assert.equal(getCalls, 2);
});

function createPreferences(
  featureFlags: Record<string, boolean>
): PutDesktopPreferencesRequest["preferences"] {
  return {
    agentCliUpdateCheckEnabled: true,
    agentComposerDefaultsByProvider: {},
    agentGuiConversationRailCollapsedByProvider: {},
    agentConversationDetailMode: "coding",
    agentDockLayout: "unified",
    appCatalogChannel: "production",
    browserUseConnectionMode: "isolated",
    defaultAgentProvider: "tutti-agent",
    deletedAgentConversationRetentionDays: 30,
    dockIconStyle: "default",
    dockPlacement: "bottom",
    featureFlags,
    fileDefaultOpenersByExtension: {},
    locale: "en",
    minimizeAnimation: "genie",
    showAppDeveloperSources: false,
    sleepPreventionMode: "never",
    themeSource: "dark",
    updateChannel: "stable",
    updatePolicy: "prompt",
    workbenchShortcuts: defaultDesktopWorkbenchShortcuts
  };
}

function createLogger(): DesktopLogger {
  return {
    debug() {},
    info() {},
    warn() {},
    error() {},
    async close() {}
  };
}
