import assert from "node:assert/strict";
import test from "node:test";

import type {
  Connector,
  ConnectorPresentation,
  ConnectorPresentationState
} from "../../contracts/index.ts";
import { createConnectorMarketStoreState } from "../connectorMarketState.ts";
import type { ConnectorMarketUiState } from "../ui-state/connectorMarketUiStateService.interface.ts";
import { buildConnectorMarketView } from "./connectorMarketViewBuilder.ts";

const uiState: ConnectorMarketUiState = {
  dialog: null,
  query: "",
  scope: {},
  segment: "available",
  started: true
};

const states: readonly ConnectorPresentationState[] = [
  "unavailable",
  "loading",
  "setup_required",
  "authorization_required",
  "connecting",
  "connected",
  "degraded",
  "disabled",
  "unsupported",
  "failed"
];

test("maps all ten application-owned states without reading raw lifecycle facts", () => {
  for (const state of states) {
    const presentation = presentationFor(state);
    const market = marketWith(connectorFixture(presentation));
    const card = buildConnectorMarketView(market, uiState).cardsByKey.github;
    assert.equal(card?.status, state);
    assert.deepEqual(card?.allowedActions, presentation.allowedActions);
  }
});

test("card primary action follows application action priority only", () => {
  const cases = [
    { actions: ["details", "install"] as const, want: "install" },
    { actions: ["details", "update"] as const, want: "update" },
    { actions: ["details", "authorize"] as const, want: "authorize" },
    { actions: ["details", "cancel"] as const, want: "cancel" },
    { actions: ["details", "disconnect"] as const, want: "disconnect" }
  ];
  for (const { actions, want } of cases) {
    const connector = connectorFixture({
      state: want === "install" ? "setup_required" : "failed",
      reasonCode: "test",
      allowedActions: [...actions]
    });
    assert.equal(
      buildConnectorMarketView(marketWith(connector), uiState).cardsByKey.github
        ?.action,
      want
    );
  }
});

test("raw installation authorization and compatibility cannot change state or action", () => {
  const presentation: ConnectorPresentation = {
    state: "disabled",
    reasonCode: "agent_policy_disabled",
    allowedActions: ["details", "remove_selection"]
  };
  const connector = connectorFixture(presentation);
  connector.installation = {
    state: "installed",
    installedReleaseDigest: connector.release.releaseDigest
  };
  connector.authorization = { state: "connected" };
  connector.compatibility = { state: "supported" };
  const before = buildConnectorMarketView(marketWith(connector), uiState)
    .cardsByKey.github;

  connector.installation = { state: "failed", failureCode: "broken" };
  connector.authorization = { state: "failed", failureCode: "expired" };
  connector.compatibility = { state: "unsupported_platform" };
  const after = buildConnectorMarketView(marketWith(connector), uiState)
    .cardsByKey.github;

  assert.deepEqual(
    [before?.status, before?.action],
    ["disabled", "unavailable"]
  );
  assert.deepEqual([after?.status, after?.action], ["disabled", "unavailable"]);
});

test("dialogs are selected and gated by presentation state and actions", () => {
  const cases = [
    {
      presentation: {
        state: "setup_required",
        reasonCode: "setup",
        allowedActions: ["details", "install", "remove_selection"]
      } satisfies ConnectorPresentation,
      kind: "installation"
    },
    {
      presentation: {
        state: "authorization_required",
        reasonCode: "auth",
        allowedActions: ["details", "authorize", "remove_selection"]
      } satisfies ConnectorPresentation,
      kind: "authorization"
    },
    {
      presentation: {
        state: "connected",
        allowedActions: [
          "details",
          "select",
          "remove_selection",
          "disconnect",
          "uninstall"
        ]
      } satisfies ConnectorPresentation,
      kind: undefined
    },
    {
      presentation: {
        state: "unsupported",
        reasonCode: "unsupported",
        allowedActions: ["details", "remove_selection"]
      } satisfies ConnectorPresentation,
      kind: "blocked"
    }
  ];
  for (const { presentation, kind } of cases) {
    const market = marketWith(connectorFixture(presentation));
    const view = buildConnectorMarketView(market, {
      ...uiState,
      dialog: { connectorKey: "github", kind: "connector" }
    });
    assert.equal(view.dialog?.kind, kind);
  }
});

test("runtime failure exposes the application-owned restart action", () => {
  const connector = connectorFixture({
    state: "failed",
    reasonCode: "runtime_start_failed",
    allowedActions: ["details", "remove_selection", "restart_runtime"]
  });
  const card = buildConnectorMarketView(marketWith(connector), uiState)
    .cardsByKey.github;
  assert.equal(card?.status, "failed");
  assert.equal(card?.action, "restart_runtime");
});

test("stale exact-connected remains connected without mutation actions", () => {
  const connector = connectorFixture({
    state: "connected",
    allowedActions: [
      "details",
      "select",
      "remove_selection",
      "disconnect",
      "uninstall"
    ]
  });
  const market = marketWith(connector);
  market.catalogFreshness = {
    state: "stale",
    snapshotId: "last-good",
    staleSince: "2026-08-18T01:00:00Z"
  };
  const view = buildConnectorMarketView(market, {
    ...uiState,
    dialog: { connectorKey: "github", kind: "connector" },
    segment: "installed"
  });

  assert.equal(view.cardsByKey.github?.status, "connected");
  assert.equal(view.cardsByKey.github?.action, "disconnect");
  assert.equal(
    view.cardsByKey.github?.allowedActions.includes("update"),
    false
  );
  assert.equal(
    view.cardsByKey.github?.allowedActions.includes("install"),
    false
  );
  assert.equal(view.dialog, null);
});

test("catalog freshness remains canonical while local load state controls shell phase", () => {
  const market = marketWith(
    connectorFixture(presentationFor("setup_required"))
  );
  market.loadState = "error";
  market.catalogFreshness = { state: "fresh", snapshotId: "snapshot-1" };
  market.lastError = {
    code: "connector_market_upstream_unavailable",
    message: "network unavailable",
    retryable: true
  };
  const view = buildConnectorMarketView(market, uiState);
  assert.equal(view.status, "error");
  assert.equal(view.catalogError?.kind, "unavailable");
  assert.equal(market.catalogFreshness.state, "fresh");
  assert.equal("catalogState" in market, false);
});

test("projects server-owned sections and operation stage as presentation detail", () => {
  const connector = connectorFixture(presentationFor("connecting"));
  const market = marketWith(connector);
  market.catalogSections = [
    {
      categoryId: "business",
      kind: "category",
      sortOrder: 10,
      itemCount: 1,
      displayNameZh: "商业",
      displayNameEn: "Business",
      connectorKeys: [connector.key],
      loadState: "ready"
    }
  ];
  market.operationsByConnectorKey[connector.key] = {
    operationId: "operation-1",
    clientRequestId: "request-1",
    connectorKey: connector.key,
    kind: "install",
    state: "running",
    stage: "runtime_pending",
    attempt: 1,
    createdAt: "2026-08-18T00:00:00Z",
    updatedAt: "2026-08-18T00:00:01Z"
  };
  const view = buildConnectorMarketView(market, uiState);
  assert.equal(view.sections[0]?.displayNameEn, "Business");
  assert.equal(view.cardsByKey.github?.operationStage, "runtime_pending");
  assert.equal(view.cardsByKey.github?.status, "connecting");
});

test("keeps a recovered install dialog in installing state without a management action", () => {
  const connector = connectorFixture({
    state: "connecting",
    reasonCode: "installation_converging",
    allowedActions: ["details", "remove_selection"]
  });
  const market = marketWith(connector);
  market.operationsByConnectorKey[connector.key] = {
    operationId: "operation-install",
    clientRequestId: "request-install",
    connectorKey: connector.key,
    kind: "install",
    state: "running",
    stage: "installing",
    attempt: 2,
    createdAt: "2026-08-18T00:00:00Z",
    updatedAt: "2026-08-18T00:00:01Z"
  };

  const view = buildConnectorMarketView(market, {
    ...uiState,
    dialog: { connectorKey: connector.key, kind: "connector" }
  });

  assert.equal(view.cardsByKey.github?.action, "unavailable");
  assert.deepEqual(view.dialog, {
    connectorKey: "github",
    description: "Manage repositories and pull requests",
    displayName: "GitHub",
    iconUrl: "data:image/png;base64,iVBORw0KGgo=",
    permissions: [{ id: "repositories", name: "repositories" }],
    installing: true,
    kind: "installation",
    updating: false
  });
});

function marketWith(connector: Connector) {
  const market = createConnectorMarketStoreState();
  market.loadState = "ready";
  market.catalogFreshness = { state: "fresh", snapshotId: "snapshot-1" };
  market.connectorKeys = [connector.key];
  market.connectorsByKey[connector.key] = connector;
  return market;
}

function presentationFor(
  state: ConnectorPresentationState
): ConnectorPresentation {
  return state === "connected"
    ? {
        state,
        allowedActions: [
          "details",
          "select",
          "remove_selection",
          "disconnect",
          "uninstall"
        ]
      }
    : {
        state,
        reasonCode: `reason_${state}`,
        allowedActions: ["details", "remove_selection"]
      };
}

function connectorFixture(presentation: ConnectorPresentation): Connector {
  return {
    authorization: { state: "disconnected" },
    compatibility: { state: "supported" },
    installation: { state: "not_installed" },
    key: "github",
    presentation,
    release: {
      artifact: {
        mediaType: "application/vnd.tutti.connector+tar+gzip",
        sha256:
          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        sizeBytes: 1
      },
      connectorKey: "github",
      manifest: {
        authorizationKind: "oauth2",
        description: "Manage repositories and pull requests",
        displayName: "GitHub",
        iconUrl: "data:image/png;base64,iVBORw0KGgo=",
        implementation: { kind: "builtin" },
        permissions: ["repositories"],
        schemaVersion: "1"
      },
      manifestDigest:
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      publishedAt: "2026-08-04T00:00:00Z",
      releaseDigest:
        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
      releaseId: "github@1.0.0",
      schemaVersion: "1",
      status: "available",
      version: "1.0.0"
    },
    revision: 1
  };
}
