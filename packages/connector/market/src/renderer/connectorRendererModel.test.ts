import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import { createElement } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";

import type { Connector } from "../contracts/index.ts";
import { createConnectorMarketStoreState } from "../services/connectorMarketState.ts";
import {
  ConnectorComposerEntry,
  projectConnectorComposerItems,
  useConnectorRendererAgentPolicy
} from "./ConnectorComposerEntry.tsx";
import type { ConnectorMarketI18nRuntime } from "../i18n/connectorMarketI18n.ts";
import {
  normalizeConnectorRendererStatus,
  projectConnectorRendererSnapshot,
  projectConnectorStatus,
  type ConnectorRendererItem,
  type ConnectorRendererModel
} from "./connectorRendererModel.ts";

type JsdomModule = {
  JSDOM: new (html: string) => {
    window: Window & typeof globalThis;
  };
};

const require = createRequire(import.meta.url);
const { JSDOM } = require("jsdom") as JsdomModule;

const connectedItem: ConnectorRendererItem = {
  connectorKey: "github",
  name: "GitHub",
  revision: 1,
  status: "connected"
};

test("unknown renderer states fail closed as unsupported", () => {
  assert.equal(normalizeConnectorRendererStatus("future_state"), "unsupported");
});

test("projects the application-owned connector lifecycle", () => {
  const base = {
    authorization: "connected",
    compatibility: "supported",
    installation: "installed",
    pendingAuthorization: false,
    pendingInstallation: false
  };

  assert.equal(projectConnectorStatus(base), "connected");
  assert.equal(
    projectConnectorStatus({ ...base, installation: "not_installed" }),
    "setup_required"
  );
  assert.equal(
    projectConnectorStatus({ ...base, authorization: "expired" }),
    "authorization_required"
  );
  assert.equal(
    projectConnectorStatus({ ...base, pendingAuthorization: true }),
    "connecting"
  );
  assert.equal(
    projectConnectorStatus({ ...base, authorization: "failed" }),
    "failed"
  );
  assert.equal(
    projectConnectorStatus({
      ...base,
      compatibility: "unsupported_platform"
    }),
    "unsupported"
  );
});

test("shared Agent without policy fails closed", () => {
  assert.deepEqual(
    projectConnectorComposerItems({
      items: [connectedItem],
      policy: { status: "unavailable", supportedConnectorKeys: [] },
      selectedConnectorKeys: []
    }),
    [
      {
        connectorKey: "github",
        iconUrl: undefined,
        name: "GitHub",
        selected: false,
        status: "unsupported"
      }
    ]
  );
});

test("shared Agent empty allowlist disables every catalog item", () => {
  assert.equal(
    projectConnectorComposerItems({
      items: [connectedItem],
      policy: { status: "ready", supportedConnectorKeys: [] },
      selectedConnectorKeys: []
    })[0]?.status,
    "disabled"
  );
});

test("neutral draft keys outside the Connector catalog are ignored", () => {
  const projected = projectConnectorComposerItems({
    items: [connectedItem],
    policy: { status: "ready", supportedConnectorKeys: null },
    selectedConnectorKeys: ["browser", "github"]
  });

  assert.deepEqual(
    projected.map((item) => [item.connectorKey, item.selected]),
    [["github", true]]
  );
});

test("stale catalog metadata does not reinterpret physical readiness", () => {
  const market = createConnectorMarketStoreState();
  const connector = connectorFixture();
  market.loadState = "error";
  market.connectorKeys = [connector.key];
  market.connectorsByKey[connector.key] = connector;

  const snapshot = projectConnectorRendererSnapshot(market);

  assert.equal(snapshot.phase, "failed");
  assert.equal(snapshot.stale, true);
  assert.equal(snapshot.entryAvailable, true);
  assert.equal(snapshot.items[0]?.status, "connected");
});

test("authoritative catalog freshness marks a ready transport stale", () => {
  const market = createConnectorMarketStoreState();
  const connector = connectorFixture();
  market.loadState = "ready";
  market.catalogState = "refreshing";
  market.catalogFreshness = {
    state: "refreshing",
    snapshotId: "last-good",
    staleSince: "2026-08-18T01:00:00Z"
  };
  market.catalogMutationState = "blocked";
  market.connectorKeys = [connector.key];
  market.connectorsByKey[connector.key] = connector;

  const snapshot = projectConnectorRendererSnapshot(market);

  assert.equal(snapshot.phase, "ready");
  assert.equal(snapshot.stale, true);
  assert.equal(snapshot.entryAvailable, true);
  assert.equal(snapshot.items[0]?.status, "connected");
});

test("daemon unavailable without a last-good catalog hides the entry", () => {
  const market = createConnectorMarketStoreState();
  market.loadState = "error";
  const snapshot = projectConnectorRendererSnapshot(market);
  assert.equal(snapshot.entryAvailable, false);
  assert.equal(snapshot.stale, true);
});

test("composer entry hides unavailable daemon and keeps stale last-good visible", () => {
  const render = (
    entryAvailable: boolean,
    items: readonly ConnectorRendererItem[]
  ) =>
    renderToStaticMarkup(
      createElement(ConnectorComposerEntry, {
        agent: {
          target: { agentTargetId: "local", ownership: "local" },
          draft: { selectedConnectorKeys: [], setSelected: () => undefined }
        },
        i18n: {
          has: () => true,
          t: (key: string) => key,
          tFirst: (keys: readonly string[]) => keys[0] ?? ""
        } as ConnectorMarketI18nRuntime,
        model: {
          commands: { refresh: async () => undefined },
          getAgentPolicy: () => ({
            status: "ready",
            supportedConnectorKeys: null
          }),
          subscribeAgentPolicy: () => () => undefined,
          getSnapshot: () => ({
            entryAvailable,
            items,
            phase: "failed",
            revision: 1,
            stale: true
          }),
          subscribe: () => () => undefined
        } as unknown as ConnectorRendererModel,
        onEvent: () => undefined
      })
    );

  assert.equal(render(false, []), "");
  assert.match(
    render(true, [connectedItem]),
    /connector-market-composer-trigger/u
  );
});

test("stable target identity does not resubscribe during parent rerenders", async () => {
  const dom = new JSDOM('<!doctype html><div id="root"></div>');
  const previousWindow = globalThis.window;
  const previousDocument = globalThis.document;
  const previousActEnvironment = (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT;
  let reactRoot: Root | null = null;
  let subscriptions = 0;
  let unsubscriptions = 0;
  const policy = Object.freeze({
    status: "ready" as const,
    supportedConnectorKeys: Object.freeze(["github"])
  });
  const model: ConnectorRendererModel = {
    commands: {
      refresh: async () => undefined
    } as ConnectorRendererModel["commands"],
    getAgentPolicy: () => policy,
    getSnapshot: () => ({
      entryAvailable: true,
      items: [],
      phase: "ready",
      revision: 0,
      stale: false
    }),
    getSurfaceSnapshot: () =>
      ({ market: {}, ui: {}, view: {} }) as ReturnType<
        ConnectorRendererModel["getSurfaceSnapshot"]
      >,
    subscribeSurface: () => () => undefined,
    subscribe: () => () => undefined,
    subscribeAgentPolicy: () => {
      subscriptions += 1;
      return () => {
        unsubscriptions += 1;
      };
    }
  };

  function PolicyProbe(props: { targetId: string }): null {
    useConnectorRendererAgentPolicy(model, {
      agentTargetId: props.targetId,
      ownership: "shared"
    });
    return null;
  }

  try {
    globalThis.window = dom.window;
    globalThis.document = dom.window.document;
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true;
    const container = dom.window.document.getElementById("root");
    assert.ok(container);
    reactRoot = createRoot(container);

    await act(async () => {
      reactRoot?.render(createElement(PolicyProbe, { targetId: "shared-1" }));
    });
    await act(async () => {
      reactRoot?.render(createElement(PolicyProbe, { targetId: "shared-1" }));
    });

    assert.equal(subscriptions, 1);
    assert.equal(unsubscriptions, 0);
  } finally {
    if (reactRoot) {
      await act(async () => reactRoot?.unmount());
    }
    globalThis.window = previousWindow;
    globalThis.document = previousDocument;
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = previousActEnvironment;
    dom.window.close();
  }
  assert.equal(unsubscriptions, 1);
});

function connectorFixture(): Connector {
  return {
    authorization: { state: "connected" },
    compatibility: { state: "supported" },
    installation: { state: "installed" },
    key: "github",
    release: {
      artifact: {
        key: "connectors/github.tgz",
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
        implementation: {
          builtin: { cli: true, mcp: true, providerId: "github" },
          kind: "builtin"
        },
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
