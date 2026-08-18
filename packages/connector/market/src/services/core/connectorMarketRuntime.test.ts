import assert from "node:assert/strict";
import test from "node:test";
import { InstantiationService } from "@tutti-os/infra/di";

import type {
  Connector,
  ConnectorMarketBackend,
  ConnectorMarketEventSource,
  ConnectorMarketSnapshot
} from "../../contracts/index.ts";
import { ConnectorMarketModule } from "./connectorMarketModule.ts";
import { getConnectorRendererModel } from "../../composition/index.ts";
import type { ConnectorRendererAgentPolicyPort } from "../../renderer/index.ts";

test("module activation runs all service startup jobs before ready", async () => {
  let snapshotLoads = 0;
  let subscriptions = 0;
  let unsubscriptions = 0;
  const module = new ConnectorMarketModule({
    market: {
      backend: backendWith({
        getSnapshot: async () => {
          snapshotLoads += 1;
          return snapshot(1, [connector("github")]);
        },
        listCategories: async () => [
          {
            categoryId: "development",
            kind: "category",
            sortOrder: 20,
            itemCount: 1
          }
        ],
        listCatalogPage: async () => ({
          sectionId: "development",
          items: [
            {
              categoryId: "development",
              featured: false,
              connector: connector("github")
            }
          ],
          revision: 1
        })
      }),
      events: eventSource({
        onSubscribe: () => {
          subscriptions += 1;
        },
        onUnsubscribe: () => {
          unsubscriptions += 1;
        }
      })
    },
    scope: {}
  });

  await module.activate(new InstantiationService());

  assert.equal(module.lifecycle.phase, "ready");
  assert.equal(snapshotLoads, 1);
  assert.equal(subscriptions, 1);
  assert.equal(module.rendererPorts.uiState.dataStore.started, true);
  assert.equal(module.rendererPorts.view.dataStore.status, "ready");
  assert.deepEqual(
    module.rendererPorts.view.dataStore.sections[0]?.connectorKeys,
    ["github"]
  );
  assert.strictEqual(module.rendererPorts, module.rendererPorts);
  assert.equal("start" in module.rendererPorts.market, false);
  assert.equal("ensureLoaded" in module.rendererPorts.market, false);
  assert.equal("dispose" in module.rendererPorts.market, false);
  assert.equal("start" in module.rendererPorts.uiState, false);
  assert.equal("dispose" in module.rendererPorts.uiState, false);
  assert.equal("start" in module.rendererPorts.view, false);
  assert.equal("dispose" in module.rendererPorts.view, false);
  const defaultModel = getConnectorRendererModel(module.rendererPorts);
  assert.strictEqual(
    defaultModel,
    getConnectorRendererModel(module.rendererPorts)
  );
  const sharedTarget = {
    agentTargetId: "shared-agent-1",
    ownership: "shared" as const
  };
  assert.deepEqual(defaultModel.getAgentPolicy(sharedTarget), {
    status: "unavailable",
    presentationsByConnectorKey: {}
  });
  let agentPolicySubscriptions = 0;
  let agentPolicyUnsubscriptions = 0;
  const sharedPolicy = Object.freeze({
    status: "ready" as const,
    presentationsByConnectorKey: Object.freeze({
      github: {
        state: "disabled" as const,
        reasonCode: "shared_agent_connector_disabled",
        allowedActions: ["details", "remove_selection"] as (
          | "details"
          | "remove_selection"
        )[]
      }
    })
  });
  const agentPolicy: ConnectorRendererAgentPolicyPort = {
    getSnapshot: () => sharedPolicy,
    subscribe: () => {
      agentPolicySubscriptions += 1;
      return () => {
        agentPolicyUnsubscriptions += 1;
      };
    }
  };
  const policyModel = getConnectorRendererModel(module.rendererPorts, {
    agentPolicy
  });
  assert.strictEqual(policyModel, defaultModel);
  assert.strictEqual(
    policyModel,
    getConnectorRendererModel(module.rendererPorts, { agentPolicy })
  );
  assert.deepEqual(policyModel.getAgentPolicy(sharedTarget), sharedPolicy);
  assert.throws(
    () =>
      getConnectorRendererModel(module.rendererPorts, {
        agentPolicy: { getSnapshot: () => sharedPolicy }
      }),
    /different Agent policy port/
  );
  const unsubscribePolicy = policyModel.subscribeAgentPolicy(
    sharedTarget,
    () => undefined
  );
  assert.equal(agentPolicySubscriptions, 1);

  module.dispose();
  assert.equal(module.lifecycle.phase, "disposed");
  assert.equal(unsubscriptions, 1);
  assert.equal(agentPolicyUnsubscriptions, 1);
  unsubscribePolicy();
  assert.equal(agentPolicyUnsubscriptions, 1);
});

test("module activation skips market requests until the host admits them", async () => {
  let requestAllowed = false;
  let snapshotLoads = 0;
  let categoryLoads = 0;
  const module = new ConnectorMarketModule({
    market: {
      backend: backendWith({
        getSnapshot: async () => {
          snapshotLoads += 1;
          return snapshot(1, []);
        },
        listCategories: async () => {
          categoryLoads += 1;
          return [];
        }
      }),
      canRequest: () => requestAllowed,
      events: eventSource({})
    },
    scope: {}
  });

  await module.activate(new InstantiationService());

  assert.equal(module.lifecycle.phase, "ready");
  assert.equal(snapshotLoads, 0);
  assert.equal(categoryLoads, 0);

  requestAllowed = true;
  await module.rendererPorts.market.reload();

  assert.equal(snapshotLoads, 1);
  assert.equal(categoryLoads, 1);
  assert.equal(module.rendererPorts.view.dataStore.status, "empty");
  module.dispose();
});

test("module activation remains ready when optional catalog synchronization fails", async () => {
  let unsubscriptions = 0;
  const failure = new Error("catalog unavailable");
  const module = new ConnectorMarketModule({
    market: {
      backend: backendWith({
        getSnapshot: async () => Promise.reject(failure)
      }),
      events: eventSource({
        onUnsubscribe: () => {
          unsubscriptions += 1;
        }
      })
    },
    scope: {}
  });

  await module.activate(new InstantiationService());

  assert.equal(module.lifecycle.phase, "ready");
  assert.equal(module.rendererPorts.market.dataStore.loadState, "error");
  assert.equal(unsubscriptions, 0);
  module.dispose();
  assert.equal(module.lifecycle.phase, "disposed");
  assert.equal(unsubscriptions, 1);
});

test("one dialog host projects authorization without creating a connected management dialog", async () => {
  const module = new ConnectorMarketModule({
    market: {
      backend: backendWith({
        getSnapshot: async () =>
          snapshot(1, [
            connector("github", {
              authorization: { state: "disconnected" },
              installation: {
                installedReleaseDigest:
                  "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
                installedReleaseId: "github@1.0.0",
                installedVersion: "1.0.0",
                state: "installed"
              },
              presentation: {
                state: "authorization_required",
                reasonCode: "connector_authorization_required",
                allowedActions: [
                  "details",
                  "remove_selection",
                  "authorize",
                  "uninstall"
                ]
              }
            }),
            connector("notion", {
              authorization: { state: "connected" },
              installation: {
                installedReleaseDigest:
                  "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
                installedReleaseId: "notion@1.0.0",
                installedVersion: "1.0.0",
                state: "installed"
              },
              presentation: {
                state: "connected",
                allowedActions: [
                  "details",
                  "select",
                  "remove_selection",
                  "disconnect",
                  "uninstall"
                ]
              }
            })
          ])
      }),
      events: eventSource({})
    },
    scope: {}
  });

  await module.activate(new InstantiationService());
  const dialogKind = () => module.rendererPorts.view.dataStore.dialog?.kind;
  module.rendererPorts.uiState.openConnector("github");
  assert.equal(dialogKind(), "authorization");

  module.rendererPorts.uiState.openConnector("notion");
  assert.equal(dialogKind(), undefined);

  module.rendererPorts.uiState.requestUninstall("github");
  assert.equal(dialogKind(), "uninstall_confirmation");

  module.rendererPorts.uiState.closeDialog();
  assert.equal(module.rendererPorts.view.dataStore.dialog, null);
  module.dispose();
});

test("application presentation remains authoritative when raw install state is failed", async () => {
  const module = new ConnectorMarketModule({
    market: {
      backend: backendWith({
        getSnapshot: async () =>
          snapshot(1, [
            connector("github", {
              installation: {
                failureCode: "artifact_download_failed",
                state: "failed"
              }
            })
          ])
      }),
      events: eventSource({})
    },
    scope: {}
  });

  await module.activate(new InstantiationService());
  assert.equal(module.rendererPorts.view.dataStore.availableCount, 1);
  assert.equal(module.rendererPorts.view.dataStore.installedCount, 0);
  assert.equal(
    module.rendererPorts.view.dataStore.cardsByKey.github?.action,
    "install"
  );
  module.rendererPorts.uiState.openConnector("github");
  assert.equal(
    module.rendererPorts.view.dataStore.dialog?.kind,
    "installation"
  );
  module.dispose();
});

function connector(key: string, overrides: Partial<Connector> = {}): Connector {
  const value: Connector = {
    authorization: { state: "not_required" },
    compatibility: { state: "supported" },
    installation: { state: "not_installed" },
    key,
    presentation: {
      state: "setup_required",
      reasonCode: "connector_not_installed",
      allowedActions: ["details", "remove_selection", "install"]
    },
    release: {
      artifact: {
        mediaType: "application/vnd.tutti.connector+tar+gzip",
        sha256:
          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        sizeBytes: 1
      },
      connectorKey: key,
      manifest: {
        authorizationKind: "none",
        displayName: "GitHub",
        iconUrl: "data:image/png;base64,iVBORw0KGgo=",
        implementation: { kind: "builtin" },
        permissions: [],
        schemaVersion: "1"
      },
      manifestDigest:
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      publishedAt: "2026-08-04T00:00:00Z",
      releaseDigest:
        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
      releaseId: `${key}@1.0.0`,
      schemaVersion: "1",
      status: "available",
      version: "1.0.0"
    },
    revision: 1
  };
  return { ...value, ...overrides };
}

function snapshot(
  revision: number,
  connectors: Connector[]
): ConnectorMarketSnapshot {
  return {
    catalogFreshness: { state: "fresh", snapshotId: `snapshot-${revision}` },
    connectors,
    eventCursor: revision,
    operations: [],
    revision
  };
}

function backendWith(
  overrides: Partial<ConnectorMarketBackend>
): ConnectorMarketBackend {
  const unsupported = async (): Promise<never> => {
    throw new Error("not implemented in test");
  };
  return {
    beginAuthorization: unsupported,
    cancelAuthorization: unsupported,
    disconnectAuthorization: unsupported,
    getConnector: unsupported,
    getOperation: unsupported,
    getSnapshot: async () => snapshot(0, []),
    listCategories: async () => [],
    listCatalogPage: unsupported,
    installConnector: unsupported,
    refreshCatalog: unsupported,
    uninstallConnector: unsupported,
    restartRuntime: unsupported,
    ...overrides
  };
}

function eventSource(callbacks: {
  onSubscribe?: () => void;
  onUnsubscribe?: () => void;
}): ConnectorMarketEventSource {
  return {
    subscribe() {
      callbacks.onSubscribe?.();
      return () => callbacks.onUnsubscribe?.();
    }
  };
}
