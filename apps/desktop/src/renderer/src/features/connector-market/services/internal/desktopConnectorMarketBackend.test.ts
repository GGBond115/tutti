import assert from "node:assert/strict";
import test from "node:test";
import type {
  ConnectorMarketCanonicalConnector,
  ConnectorMarketCanonicalSnapshot,
  ConnectorMarketClient
} from "@tutti-os/client-tuttid-ts";
import { ConnectorMarketClientError } from "@tutti-os/client-tuttid-ts";
import { createDesktopConnectorMarketBackend } from "./desktopConnectorMarketBackend.ts";

test("desktop connector market backend maps canonical snapshot into the Market-owned contract", async () => {
  let calls = 0;
  const snapshot: ConnectorMarketCanonicalSnapshot = {
    catalogFreshness: {
      state: "fresh",
      snapshotId: "snapshot-7",
      sourceRevision: "sha256:catalog"
    },
    connectors: [canonicalConnector()],
    operations: [],
    revision: 7,
    eventCursor: 11
  };
  const client = {
    async getConnectorMarket() {
      calls += 1;
      return snapshot;
    }
  } as ConnectorMarketClient;

  const backend = createDesktopConnectorMarketBackend(client);

  const result = await backend.getSnapshot();
  assert.notEqual(result, snapshot);
  assert.notEqual(result.connectors[0], snapshot.connectors[0]);
  assert.deepEqual(result.catalogFreshness, snapshot.catalogFreshness);
  assert.deepEqual(result.connectors[0]?.presentation, {
    state: "connected",
    allowedActions: ["details", "select", "remove_selection"]
  });
  assert.equal("key" in (result.connectors[0]?.release.artifact ?? {}), false);
  assert.equal(calls, 1);
});

test("desktop connector market backend fails closed for unknown canonical presentation and freshness", async () => {
  const connector = canonicalConnector() as unknown as Record<string, unknown>;
  connector.presentation = {
    state: "future_state",
    allowedActions: ["select"]
  };
  const missingPresentationConnector =
    canonicalConnector() as unknown as Record<string, unknown>;
  delete missingPresentationConnector.presentation;
  const legacyManagementConnector = canonicalConnector() as unknown as Record<
    string,
    unknown
  >;
  legacyManagementConnector.presentation = {
    state: "connected",
    allowedActions: ["manage"]
  };
  const snapshot = {
    catalogFreshness: { state: "future_state" },
    connectors: [
      connector,
      missingPresentationConnector,
      legacyManagementConnector
    ],
    operations: [],
    revision: 7,
    eventCursor: 11
  } as unknown as ConnectorMarketCanonicalSnapshot;
  const client = {
    async getConnectorMarket() {
      return snapshot;
    }
  } as ConnectorMarketClient;

  const result =
    await createDesktopConnectorMarketBackend(client).getSnapshot();

  assert.deepEqual(result.catalogFreshness, {
    state: "unavailable",
    lastFailure: "unsupported_catalog_freshness"
  });
  assert.deepEqual(result.connectors[0]?.presentation, {
    state: "unsupported",
    reasonCode: "unsupported_connector_presentation",
    allowedActions: ["details", "remove_selection"]
  });
  assert.deepEqual(
    result.connectors[1]?.presentation,
    result.connectors[0]?.presentation
  );
  assert.deepEqual(
    result.connectors[2]?.presentation,
    result.connectors[0]?.presentation
  );
});

test("desktop connector market backend terminates transport DTOs on every connector-bearing route", async () => {
  const source = canonicalConnector();
  const mutation = {
    outcome: "completed" as const,
    connector: source,
    revision: 8
  };
  const client = {
    async listConnectorMarketCatalog() {
      return {
        sectionId: "all",
        items: [{ categoryId: "all", featured: false, connector: source }],
        revision: 8
      };
    },
    async getConnectorMarketConnector() {
      return source;
    },
    async refreshConnectorMarket() {
      return mutation;
    },
    async installConnectorMarketConnector() {
      return mutation;
    },
    async uninstallConnectorMarketConnector() {
      return mutation;
    },
    async startConnectorMarketAuthorization() {
      return { ...mutation, authorizationUrl: "https://example.test/oauth" };
    },
    async cancelConnectorMarketAuthorization() {
      return mutation;
    },
    async disconnectConnectorMarketAuthorization() {
      return mutation;
    }
  } as unknown as ConnectorMarketClient;
  const backend = createDesktopConnectorMarketBackend(client);
  const mutationInput = {
    connectorKey: "github",
    clientRequestId: "request-8",
    expectedRevision: 8,
    expectedConnectorRevision: 4
  };
  const mapped = [
    (await backend.listCatalogPage({ sectionId: "all", pageSize: 20 })).items[0]
      ?.connector,
    await backend.getConnector({ connectorKey: "github" }),
    (await backend.refreshCatalog(mutationInput)).connector,
    (await backend.installConnector(mutationInput)).connector,
    (await backend.uninstallConnector(mutationInput)).connector,
    (await backend.beginAuthorization(mutationInput)).connector,
    (
      await backend.cancelAuthorization({
        ...mutationInput,
        operationId: "authorization-8"
      })
    ).connector,
    (await backend.disconnectAuthorization(mutationInput)).connector
  ];

  assert.equal(mapped.length, 8);
  for (const connector of mapped) {
    assert.ok(connector);
    assert.notEqual(connector, source);
    assert.equal("key" in connector.release.artifact, false);
    assert.deepEqual(connector.presentation, source.presentation);
  }
});

test("desktop connector market backend preserves mutation idempotency fields", async () => {
  const calls: unknown[] = [];
  const client = {
    async installConnectorMarketConnector(
      connectorKey: string,
      request: { clientRequestId: string; expectedRevision: number }
    ) {
      calls.push({ connectorKey, request });
      return {
        outcome: "accepted" as const,
        operation: {
          operationId: "operation-1",
          clientRequestId: request.clientRequestId,
          connectorKey,
          kind: "install" as const,
          state: "accepted" as const,
          attempt: 0,
          createdAt: "2026-08-03T00:00:00Z",
          updatedAt: "2026-08-03T00:00:00Z"
        },
        revision: 9
      };
    }
  } as ConnectorMarketClient;

  const backend = createDesktopConnectorMarketBackend(client);
  await backend.installConnector({
    connectorKey: "notion",
    clientRequestId: "request-1",
    expectedRevision: 8,
    expectedConnectorRevision: 7
  });

  assert.deepEqual(calls, [
    {
      connectorKey: "notion",
      request: {
        clientRequestId: "request-1",
        expectedRevision: 8,
        expectedConnectorRevision: 7
      }
    }
  ]);
});

test("desktop connector market backend delegates uninstall idempotency fields", async () => {
  const calls: unknown[] = [];
  const client = {
    async uninstallConnectorMarketConnector(
      connectorKey: string,
      request: { clientRequestId: string; expectedRevision: number }
    ) {
      calls.push({ connectorKey, request });
      return {
        outcome: "accepted" as const,
        operation: {
          operationId: "operation-uninstall-1",
          clientRequestId: request.clientRequestId,
          connectorKey,
          kind: "uninstall" as const,
          state: "accepted" as const,
          attempt: 0,
          createdAt: "2026-08-11T00:00:00Z",
          updatedAt: "2026-08-11T00:00:00Z"
        },
        revision: 10
      };
    }
  } as ConnectorMarketClient;

  const backend = createDesktopConnectorMarketBackend(client);
  await backend.uninstallConnector({
    connectorKey: "notion",
    clientRequestId: "request-uninstall-1",
    expectedRevision: 9,
    expectedConnectorRevision: 8
  });

  assert.deepEqual(calls, [
    {
      connectorKey: "notion",
      request: {
        clientRequestId: "request-uninstall-1",
        expectedRevision: 9,
        expectedConnectorRevision: 8
      }
    }
  ]);
});

test("desktop connector market backend delegates authorization cancellation", async () => {
  const calls: unknown[] = [];
  const client = {
    async cancelConnectorMarketAuthorization(
      connectorKey: string,
      request: {
        clientRequestId: string;
        expectedRevision: number;
        expectedConnectorRevision: number;
        operationId: string;
      }
    ) {
      calls.push({ connectorKey, request });
      return { outcome: "completed" as const, revision: 9 };
    }
  } as ConnectorMarketClient;

  const backend = createDesktopConnectorMarketBackend(client);
  await backend.cancelAuthorization({
    connectorKey: "supabase",
    clientRequestId: "cancel-1",
    expectedRevision: 8,
    expectedConnectorRevision: 7,
    operationId: "authorization-1"
  });

  assert.deepEqual(calls, [
    {
      connectorKey: "supabase",
      request: {
        clientRequestId: "cancel-1",
        expectedRevision: 8,
        expectedConnectorRevision: 7,
        operationId: "authorization-1"
      }
    }
  ]);
});

test("desktop connector market backend preserves structured daemon errors", async () => {
  const structuredError = new ConnectorMarketClientError(
    {
      code: "connector_market_revision_conflict",
      message: "connector market revision changed",
      retryable: true,
      revision: 12
    },
    409
  );
  const client = {
    async getConnectorMarket() {
      throw structuredError;
    }
  } as unknown as ConnectorMarketClient;
  const backend = createDesktopConnectorMarketBackend(client);

  await assert.rejects(backend.getSnapshot(), (error: unknown) => {
    assert.equal(error, structuredError);
    assert.equal(
      (error as ConnectorMarketClientError).code,
      structuredError.code
    );
    assert.equal(
      (error as ConnectorMarketClientError).retryable,
      structuredError.retryable
    );
    assert.deepEqual(
      (error as ConnectorMarketClientError).details,
      structuredError.details
    );
    return true;
  });
});

function canonicalConnector(): ConnectorMarketCanonicalConnector {
  return {
    key: "github",
    release: {
      schemaVersion: "1",
      releaseId: "github@1.0.0",
      connectorKey: "github",
      version: "1.0.0",
      releaseDigest: "sha256:release",
      manifestDigest: "sha256:manifest",
      manifest: {
        schemaVersion: "1",
        displayName: "GitHub",
        iconUrl: "data:image/png;base64,iVBORw0KGgo=",
        permissions: [],
        implementation: { kind: "builtin" },
        authorizationKind: "oauth"
      },
      artifact: {
        key: "connectors/github.tgz",
        sha256:
          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        sizeBytes: 1,
        mediaType: "application/vnd.tutti.connector+tar+gzip"
      },
      publishedAt: "2026-08-18T00:00:00Z",
      status: "available"
    },
    installation: {
      state: "installed",
      installedVersion: "1.0.0",
      installedReleaseId: "github@1.0.0",
      installedReleaseDigest: "sha256:release"
    },
    authorization: { state: "connected" },
    compatibility: { state: "supported" },
    presentation: {
      state: "connected",
      allowedActions: ["details", "select", "remove_selection"]
    },
    revision: 4
  };
}
