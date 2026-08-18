import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

import { inspectConnectorBoundaries } from "./check-connector-boundaries.mjs";
import { selectRepositoryChecks } from "./repository-checks.mjs";

test("rejects Connector imports of Agent, app client, and private package paths", (t) => {
  const root = fixtureRoot(t, {
    "packages/agent/example/package.json": JSON.stringify({
      name: "@tutti-os/agent-example",
      exports: { ".": "./src/index.ts" }
    }),
    "packages/connector/example/src/index.ts": `
      import "@tutti-os/agent-example";
      import "@tutti-os/agent-example/src/private.ts";
    `,
    "packages/connector/example/adapter.go": `package example
      import market "github.com/tutti-os/tutti/packages/clients/market-go"
      var _ = market.Client{}
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(({ rule }) => rule === "connector-host-dependency"),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ rule }) => rule === "connector-private-deep-import"),
    formatViolations(violations)
  );
});

test("allows the pinned Market client only in market/source, never daemon", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/daemon/go.mod": `module github.com/tutti-os/tutti/packages/connector/daemon
      require github.com/tutti-os/tutti/packages/clients/market-go v0.0.0
      replace github.com/tutti-os/tutti/packages/clients/market-go => ../../clients/market-go
    `,
    "packages/connector/daemon/catalog_source.go": `package daemon
      import market "github.com/tutti-os/tutti/packages/clients/market-go"
      var _ = market.Client{}
    `,
    "packages/connector/market/source/go.mod": `module github.com/tutti-os/tutti/packages/connector/market/source
      require github.com/tutti-os/tutti/packages/clients/market-go v0.0.0
      replace github.com/tutti-os/tutti/packages/clients/market-go => ../../../clients/market-go
    `,
    "packages/connector/market/source/catalog_source.go": `package source
      import market "github.com/tutti-os/tutti/packages/clients/market-go"
      import marketv1 "github.com/tutti-os/tutti/packages/clients/market-go/generated/sandbox/v1"
      var _ = market.Client{}
      var _ = marketv1.MarketItem{}
    `
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-host-dependency"
  );
  assert.ok(
    violations.some(({ file }) => file.endsWith("daemon/catalog_source.go")),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ file }) => file.endsWith("daemon/go.mod")),
    formatViolations(violations)
  );
  assert.ok(
    violations.every(({ file }) => !/market\/source/u.test(file)),
    formatViolations(violations)
  );
});

test("rejects restoration of the legacy Connector Host path anywhere in Go", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/host/go.mod":
      "module github.com/tutti-os/tutti/packages/connector/host\n",
    "packages/connector/host/compat.go": "package host\n",
    "services/example/consumer.go": `package example
      import legacy "github.com/tutti-os/tutti/packages/connector/host"
      var _ legacy.Application
    `,
    "services/example/go.mod": `module example
      require github.com/tutti-os/tutti/packages/connector/host v0.0.0
      replace github.com/tutti-os/tutti/packages/connector/host => ../../packages/connector/host
    `
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-legacy-host"
  );
  assert.ok(violations.length >= 5, formatViolations(violations));
  assert.ok(
    violations.some(({ file }) => file === "services/example/consumer.go"),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ file }) => file === "packages/connector/host/go.mod"),
    formatViolations(violations)
  );
});

test("enforces the Connector Go module dependency DAG", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/contracts/reverse.go": `package contracts
      import _ "github.com/tutti-os/tutti/packages/connector/application"
      import _ "github.com/tutti-os/tutti/packages/connector/daemon"
      import _ "github.com/tutti-os/tutti/packages/connector/runtime"
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
    `,
    "packages/connector/application/reverse.go": `package application
      import _ "github.com/tutti-os/tutti/packages/connector/daemon"
      import _ "github.com/tutti-os/tutti/packages/connector/runtime"
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
      import _ "github.com/tutti-os/tutti/services/tuttid/api/generated/v1"
    `,
    "packages/connector/daemon/reverse.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/runtime"
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
    `,
    "packages/connector/runtime/reverse.go": `package runtime
      import _ "github.com/tutti-os/tutti/packages/connector/daemon"
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
    `,
    "packages/connector/store-sqlite/reverse.go": `package storesqlite
      import _ "github.com/tutti-os/tutti/packages/connector/daemon"
      import _ "github.com/tutti-os/tutti/packages/connector/runtime"
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
    `,
    "packages/connector/market/source/reverse.go": `package source
      import _ "github.com/tutti-os/tutti/packages/connector/daemon"
      import _ "github.com/tutti-os/tutti/packages/connector/runtime"
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
    `
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-go-module-dag"
  );
  const forbiddenEdges = new Map([
    [
      "contracts",
      ["application", "daemon", "runtime", "store-sqlite", "market/source"]
    ],
    ["application", ["daemon", "runtime", "store-sqlite", "market/source"]],
    ["daemon", ["runtime", "store-sqlite", "market/source"]],
    ["runtime", ["daemon", "store-sqlite", "market/source"]],
    ["store-sqlite", ["daemon", "runtime", "market/source"]],
    ["market/source", ["daemon", "runtime", "store-sqlite"]]
  ]);
  for (const [importer, dependencies] of forbiddenEdges) {
    for (const dependency of dependencies) {
      assert.ok(
        violations.some(({ message }) =>
          message.includes(
            `${importer} cannot depend on Connector ${dependency};`
          )
        ),
        `${importer} -> ${dependency} was not rejected:\n${formatViolations(violations)}`
      );
    }
  }
  assert.ok(
    violations.some(({ message }) => message.includes("generated transport")),
    formatViolations(violations)
  );
});

test("accepts only the forward Connector Go dependency edges", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/contracts/types.go": "package contracts\n",
    "packages/connector/application/service.go": `package application
      import _ "github.com/tutti-os/tutti/packages/connector/contracts"
    `,
    "packages/connector/daemon/host.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/application"
      import _ "github.com/tutti-os/tutti/packages/connector/contracts"
    `,
    "packages/connector/runtime/adapter.go": `package runtime
      import _ "github.com/tutti-os/tutti/packages/connector/application"
      import _ "github.com/tutti-os/tutti/packages/connector/contracts"
      import _ "github.com/tutti-os/tutti/packages/connector/runtime/process"
    `,
    "packages/connector/store-sqlite/store.go": `package storesqlite
      import _ "github.com/tutti-os/tutti/packages/connector/application"
      import _ "github.com/tutti-os/tutti/packages/connector/contracts"
    `,
    "packages/connector/market/source/source.go": `package source
      import _ "github.com/tutti-os/tutti/packages/connector/application"
      import _ "github.com/tutti-os/tutti/packages/connector/contracts"
      import _ "github.com/tutti-os/tutti/packages/clients/market-go"
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.equal(violations.length, 0, formatViolations(violations));
});

test("rejects daemon-to-Market-source source and go.mod edges", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/daemon/go.mod": `module github.com/tutti-os/tutti/packages/connector/daemon
      require github.com/tutti-os/tutti/packages/connector/market/source v0.0.0
      replace github.com/tutti-os/tutti/packages/connector/market/source => ../market/source
    `,
    "packages/connector/daemon/catalog_source.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/market/source"
    `
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-go-module-dag"
  );
  assert.ok(
    violations.some(
      ({ file, message }) =>
        file === "packages/connector/daemon/catalog_source.go" &&
        message.includes("daemon cannot depend on Connector market/source")
    ),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(
      ({ file, message }) =>
        file === "packages/connector/daemon/go.mod" &&
        message.includes(
          "daemon go.mod cannot depend on Connector market/source"
        )
    ),
    formatViolations(violations)
  );
});

test("rejects a forbidden Connector go.mod edge without a test-only import", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/application/go.mod": `module github.com/tutti-os/tutti/packages/connector/application
      require github.com/tutti-os/tutti/packages/connector/daemon v0.0.0
      replace github.com/tutti-os/tutti/packages/connector/daemon => ../daemon
    `,
    "packages/connector/application/service.go": "package application\n",
    "packages/connector/daemon/host.go": "package daemon\n"
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-go-module-dag"
  );
  assert.ok(
    violations.some(
      ({ file, message }) =>
        file === "packages/connector/application/go.mod" &&
        message.includes("application go.mod cannot depend on Connector daemon")
    ),
    formatViolations(violations)
  );
});

test("accepts a go.mod edge proven to be test-only", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/daemon/go.mod": `module github.com/tutti-os/tutti/packages/connector/daemon
      require github.com/tutti-os/tutti/packages/connector/store-sqlite v0.0.0
      replace github.com/tutti-os/tutti/packages/connector/store-sqlite => ../store-sqlite
    `,
    "packages/connector/daemon/host.go": "package daemon\n",
    "packages/connector/daemon/host_test.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
    `,
    "packages/connector/store-sqlite/store.go": "package storesqlite\n"
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-go-module-dag"
  );
  assert.equal(violations.length, 0, formatViolations(violations));
});

test("rejects a test-only go.mod exception once production imports the edge", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/daemon/go.mod": `module github.com/tutti-os/tutti/packages/connector/daemon
      require github.com/tutti-os/tutti/packages/connector/store-sqlite v0.0.0
      replace github.com/tutti-os/tutti/packages/connector/store-sqlite => ../store-sqlite
    `,
    "packages/connector/daemon/host.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
    `,
    "packages/connector/daemon/host_test.go": `package daemon
      import _ "github.com/tutti-os/tutti/packages/connector/store-sqlite"
    `,
    "packages/connector/store-sqlite/store.go": "package storesqlite\n"
  });

  const violations = inspectConnectorBoundaries(root).filter(
    ({ rule }) => rule === "connector-go-module-dag"
  );
  assert.ok(
    violations.some(({ file }) => file === "packages/connector/daemon/host.go"),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ file }) => file === "packages/connector/daemon/go.mod"),
    formatViolations(violations)
  );
});

test("rejects the legacy AgentGUI Connector import and wire vocabulary", (t) => {
  const root = fixtureRoot(t, {
    "packages/agent/gui/agent-gui/composer/LegacyConnectorMenu.tsx": `
      import { ConnectorMenu } from "@tutti-os/connector-market/renderer";
      export const block = { type: "connector", connectorKey: "github" };
      export { ConnectorMenu };
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(({ rule }) => rule === "agent-gui-connector-import"),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ rule }) => rule === "agent-gui-connector-vocabulary"),
    formatViolations(violations)
  );
});

test("rejects React and renderer imports from Connector application services", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/market/src/services/legacyService.ts": `
      import React from "react";
      import { ConnectorComposerEntry } from "../renderer/index.ts";
      export const legacy = [React, ConnectorComposerEntry];
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.filter(
      ({ rule }) => rule === "connector-application-renderer-dependency"
    ).length >= 2,
    formatViolations(violations)
  );
});

test("walks renderer dependencies and rejects services plus global host state", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/market/src/renderer/index.ts": `
      export { render } from "./panel.ts";
    `,
    "packages/connector/market/src/renderer/panel.ts": `
      import { service } from "../services/service.ts";
      export const render = () => window.tutti.open(service);
    `,
    "packages/connector/market/src/services/service.ts": `
      export const service = "legacy";
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(({ rule }) => rule === "connector-renderer-dependency"),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(({ rule }) => rule === "connector-renderer-global-state"),
    formatViolations(violations)
  );
});

test("accepts the closed Connector renderer dependency surface", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/market/src/contracts/index.ts": `
      export interface Snapshot { readonly revision: number }
    `,
    "packages/connector/market/src/i18n/index.ts": `
      import { createPackageI18nRuntime } from "@tutti-os/ui-i18n-runtime";
      export const i18n = createPackageI18nRuntime;
    `,
    "packages/connector/market/src/renderer/index.ts": `
      import type { Snapshot } from "../contracts/index.ts";
      import { i18n } from "../i18n/index.ts";
      import { Button } from "@tutti-os/ui-system/components";
      import type { ReactNode } from "react";
      export const render = (snapshot: Snapshot): ReactNode => Button && i18n && snapshot.revision;
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.equal(violations.length, 0, formatViolations(violations));
});

test("rejects transport clients reached transitively through Connector contracts", (t) => {
  const root = fixtureRoot(t, {
    "packages/connector/market/src/contracts/index.ts": `
      import type { ConnectorMarketCanonicalSnapshot } from "@tutti-os/client-tuttid-ts";
      export type Snapshot = ConnectorMarketCanonicalSnapshot;
    `,
    "packages/connector/market/src/renderer/index.ts": `
      import type { Snapshot } from "../contracts/index.ts";
      export const revision = (snapshot: Snapshot) => snapshot.revision;
    `
  });

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(
      ({ rule, message }) =>
        rule === "connector-renderer-dependency" &&
        message.includes("@tutti-os/client-tuttid-ts")
    ),
    formatViolations(violations)
  );
});

test("detects unpublished exports and missing build entries bidirectionally", (t) => {
  const root = fixtureRoot(
    t,
    validPackageFixture({
      workspaceExtra: { "./forgotten": "./src/forgotten.ts" },
      publishExtra: { "./ghost": publishEntry("./dist/ghost") }
    })
  );

  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(
      ({ rule, message }) =>
        rule === "connector-package-export-parity" &&
        message.includes("forgotten")
    ),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(
      ({ rule, message }) =>
        rule === "connector-package-build-parity" &&
        message.includes("forgotten")
    ),
    formatViolations(violations)
  );
  assert.ok(
    violations.some(
      ({ rule, message }) =>
        rule === "connector-package-export-parity" && message.includes("ghost")
    ),
    formatViolations(violations)
  );
});

test("accepts renderer canonical export parity and a forwarding-only ui entry", (t) => {
  const root = fixtureRoot(t, validPackageFixture());
  const violations = inspectConnectorBoundaries(root);
  assert.equal(violations.length, 0, formatViolations(violations));
});

test("rejects logic in the legacy ui compatibility entry", (t) => {
  const fixture = validPackageFixture();
  fixture["packages/connector/market/src/ui/index.ts"] = `
    export * from "../renderer/index.ts";
    export const fallback = true;
  `;
  const root = fixtureRoot(t, fixture);
  const violations = inspectConnectorBoundaries(root);
  assert.ok(
    violations.some(({ rule }) => rule === "connector-renderer-export"),
    formatViolations(violations)
  );
});

test("repository checks select Connector boundaries and pinned Market generation", () => {
  const boundaryKeys = selectRepositoryChecks([
    "packages/connector/market/src/renderer/index.ts"
  ]).map(({ key }) => key);
  assert.ok(boundaryKeys.includes("boundary:connector"));

  for (const file of [
    "packages/connector/market/openapi/connector-market.v1.yaml",
    "packages/clients/market-go/source.lock.json",
    "packages/clients/market-go/generated/sandbox/v1/market.pb.go",
    "tools/scripts/sync-market-go-client.mjs"
  ]) {
    const keys = selectRepositoryChecks([file], { group: "generated" }).map(
      ({ key }) => key
    );
    assert.ok(
      keys.includes("generated:api"),
      `${file} must select generated:api`
    );
  }
});

function validPackageFixture({ workspaceExtra = {}, publishExtra = {} } = {}) {
  const workspaceExports = {
    ".": "./src/index.ts",
    "./renderer": "./src/renderer/index.ts",
    "./ui": "./src/ui/index.ts",
    ...workspaceExtra
  };
  const publishExports = {
    ".": publishEntry("./dist/index"),
    "./renderer": publishEntry("./dist/renderer/index"),
    "./ui": publishEntry("./dist/ui/index"),
    ...publishExtra
  };
  return {
    "packages/connector/market/package.json": JSON.stringify({
      name: "@tutti-os/connector-market",
      private: false,
      exports: workspaceExports,
      types: "./src/index.ts",
      publishConfig: {
        access: "public",
        exports: publishExports,
        types: "./dist/index.d.ts",
        typesVersions: {
          "*": {
            renderer: ["./dist/renderer/index.d.ts"],
            ui: ["./dist/ui/index.d.ts"]
          }
        }
      }
    }),
    "packages/connector/market/tsup.config.ts": `
      export default defineConfig({ entry: {
        index: "src/index.ts",
        "renderer/index": "src/renderer/index.ts",
        "ui/index": "src/ui/index.ts"
      }});
    `,
    "packages/connector/market/src/index.ts": "export const root = true;\n",
    "packages/connector/market/src/renderer/index.ts":
      "export const renderer = true;\n",
    "packages/connector/market/src/ui/index.ts":
      '/** @deprecated */\nexport * from "../renderer/index.ts";\n',
    ...Object.fromEntries(
      Object.values(workspaceExtra).map((path) => [
        `packages/connector/market/${path.replace(/^\.\//u, "")}`,
        "export const value = true;\n"
      ])
    )
  };
}

function publishEntry(base) {
  return { import: `${base}.js`, types: `${base}.d.ts` };
}

function fixtureRoot(t, files) {
  const root = mkdtempSync(join(tmpdir(), "tutti-connector-boundaries-"));
  t.after(() => rmSync(root, { force: true, recursive: true }));
  for (const [path, content] of Object.entries(files)) {
    const absolutePath = join(root, path);
    mkdirSync(dirname(absolutePath), { recursive: true });
    writeFileSync(absolutePath, content, "utf8");
  }
  return root;
}

function formatViolations(violations) {
  return violations
    .map(
      ({ file, line, message, rule }) => `[${rule}] ${file}:${line} ${message}`
    )
    .join("\n");
}
