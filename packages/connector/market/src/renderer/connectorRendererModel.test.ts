import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";

import type {
  Connector,
  ConnectorPresentation,
  ConnectorPresentationState
} from "../contracts/index.ts";
import type { ConnectorMarketI18nRuntime } from "../i18n/connectorMarketI18n.ts";
import { createConnectorMarketStoreState } from "../services/connectorMarketState.ts";
import {
  ConnectorComposerEntry,
  connectorComposerSelectionAllowed,
  projectConnectorComposerItems,
  useConnectorRendererAgentPolicy
} from "./ConnectorComposerEntry.tsx";
import {
  projectConnectorRendererSnapshot,
  type ConnectorRendererItem,
  type ConnectorRendererModel
} from "./connectorRendererModel.ts";

type JsdomModule = {
  JSDOM: new (html: string) => { window: Window & typeof globalThis };
};
const require = createRequire(import.meta.url);
const { JSDOM } = require("jsdom") as JsdomModule;

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

test("copies every canonical application presentation without lifecycle derivation", () => {
  for (const state of states) {
    const market = createConnectorMarketStoreState();
    const presentation = presentationFor(state);
    const connector = connectorFixture(presentation);
    market.loadState = "ready";
    market.catalogFreshness = {
      state: "fresh",
      snapshotId: "snapshot-1"
    };
    market.connectorKeys = [connector.key];
    market.connectorsByKey[connector.key] = connector;

    const projected = projectConnectorRendererSnapshot(market);
    assert.deepEqual(projected.items[0]?.presentation, presentation);
  }
});

test("defensively normalizes unknown application state and actions to unsupported", () => {
  const market = createConnectorMarketStoreState();
  const connector = connectorFixture({
    state: "future_state" as ConnectorPresentationState,
    reasonCode: "future",
    allowedActions: ["future_action" as "details"]
  });
  market.loadState = "ready";
  market.connectorKeys = [connector.key];
  market.connectorsByKey[connector.key] = connector;

  assert.deepEqual(
    projectConnectorRendererSnapshot(market).items[0]?.presentation,
    {
      state: "unsupported",
      reasonCode: "unsupported_connector_presentation",
      allowedActions: ["details", "remove_selection"]
    }
  );
});

test("canonical freshness is the only catalog fact in the internal snapshot", () => {
  const market = createConnectorMarketStoreState();
  market.loadState = "ready";
  market.catalogFreshness = {
    state: "stale",
    snapshotId: "last-good",
    sourceRevision: "catalog-8",
    staleSince: "2026-08-18T01:00:00Z"
  };
  const connector = connectorFixture(presentationFor("connected"));
  market.connectorKeys = [connector.key];
  market.connectorsByKey[connector.key] = connector;

  const projected = projectConnectorRendererSnapshot(market);
  assert.equal("catalogState" in market, false);
  assert.equal("catalogMutationState" in market, false);
  assert.equal("sourceRevision" in market, false);
  assert.equal(projected.stale, true);
  assert.equal(projected.items[0]?.presentation.state, "connected");
  assert.equal(
    projected.items[0]?.presentation.allowedActions.includes("update"),
    false
  );
});

test("shared policy is per-connector and missing policy fails closed", () => {
  const item = rendererItem(presentationFor("connected"));
  const ready = projectConnectorComposerItems({
    items: [item],
    policy: {
      status: "ready",
      presentationsByConnectorKey: {
        github: presentationFor("disabled")
      }
    },
    selectedConnectorKeys: []
  });
  assert.equal(ready[0]?.status, "disabled");

  const missing = projectConnectorComposerItems({
    items: [item],
    policy: { status: "ready", presentationsByConnectorKey: {} },
    selectedConnectorKeys: ["github"]
  });
  assert.deepEqual(missing[0]?.allowedActions, ["details", "remove_selection"]);
  assert.equal(missing[0]?.status, "unsupported");
});

test("composer selection requires exact select or remove_selection action", () => {
  assert.equal(
    connectorComposerSelectionAllowed(
      {
        allowedActions: ["details", "select", "remove_selection"]
      },
      false
    ),
    true
  );
  assert.equal(
    connectorComposerSelectionAllowed(
      { allowedActions: ["details", "remove_selection"] },
      true
    ),
    true
  );
  assert.equal(
    connectorComposerSelectionAllowed(
      { allowedActions: ["details", "remove_selection"] },
      false
    ),
    false
  );
  assert.equal(
    connectorComposerSelectionAllowed(
      { allowedActions: ["details", "select"] },
      true
    ),
    false
  );
});

test("composer entry hides unavailable daemon and keeps last-good visible", () => {
  const render = (entryAvailable: boolean) =>
    renderToStaticMarkup(
      createElement(ConnectorComposerEntry, {
        agent: {
          target: { agentTargetId: "local", ownership: "local" },
          draft: { selectedConnectorKeys: [], setSelected: () => undefined }
        },
        i18n: i18nFixture(),
        model: modelFixture(entryAvailable),
        onEvent: () => undefined
      })
    );
  assert.equal(render(false), "");
  assert.match(render(true), /connector-market-composer-trigger/u);
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
  const model = modelFixture(true, {
    subscribeAgentPolicy: () => {
      subscriptions += 1;
      return () => {
        unsubscriptions += 1;
      };
    }
  });

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
    if (reactRoot) await act(async () => reactRoot?.unmount());
    globalThis.window = previousWindow;
    globalThis.document = previousDocument;
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = previousActEnvironment;
    dom.window.close();
  }
  assert.equal(unsubscriptions, 1);
});

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
          "manage",
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

function rendererItem(
  presentation: ConnectorPresentation
): ConnectorRendererItem {
  return {
    connectorKey: "github",
    name: "GitHub",
    presentation,
    revision: 1
  };
}

function modelFixture(
  entryAvailable: boolean,
  overrides: Partial<ConnectorRendererModel> = {}
): ConnectorRendererModel {
  const presentation = presentationFor("connected");
  const policy = Object.freeze({
    status: "ready" as const,
    presentationsByConnectorKey: Object.freeze({ github: presentation })
  });
  return {
    commands: {
      refresh: async () => undefined
    } as ConnectorRendererModel["commands"],
    getAgentPolicy: () => policy,
    subscribeAgentPolicy: () => () => undefined,
    getSnapshot: () => ({
      entryAvailable,
      catalogFreshness: { state: "stale", snapshotId: "last-good" },
      items: [rendererItem(presentation)],
      phase: "failed",
      revision: 1,
      stale: true
    }),
    subscribe: () => () => undefined,
    getSurfaceSnapshot: () =>
      ({ market: {}, ui: {}, view: {} }) as ReturnType<
        ConnectorRendererModel["getSurfaceSnapshot"]
      >,
    subscribeSurface: () => () => undefined,
    ...overrides
  };
}

function i18nFixture(): ConnectorMarketI18nRuntime {
  return {
    has: () => true,
    t: (key: string) => key,
    tFirst: (keys: readonly string[]) => keys[0] ?? ""
  } as ConnectorMarketI18nRuntime;
}

function connectorFixture(presentation: ConnectorPresentation): Connector {
  return {
    authorization: { state: "connected" },
    compatibility: { state: "supported" },
    installation: { state: "installed" },
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
