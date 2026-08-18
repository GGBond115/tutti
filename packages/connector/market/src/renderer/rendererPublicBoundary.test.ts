import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const rendererDirectory = dirname(fileURLToPath(import.meta.url));
const sourceDirectory = dirname(rendererDirectory);
const packageDirectory = dirname(sourceDirectory);

test("public renderer surfaces accept only the readonly renderer model", () => {
  const publicSources = [
    join(rendererDirectory, "index.ts"),
    join(rendererDirectory, "components", "ConnectorMarketPanel.tsx"),
    join(rendererDirectory, "components", "ConnectorMarketDialogHost.tsx")
  ].map((path) => readFileSync(path, "utf8"));

  for (const source of publicSources) {
    assert.doesNotMatch(source, /IConnectorMarketRoot/u);
    assert.doesNotMatch(source, /\broot\s*:/u);
  }
  assert.doesNotMatch(
    publicSources[0]!,
    /normalizeConnectorPresentation|projectConnectorRendererSnapshot|projectConnectorStatus/u
  );
  assert.match(publicSources[1]!, /model:\s*ConnectorRendererModel/u);
  assert.match(publicSources[2]!, /model:\s*ConnectorRendererModel/u);
});

test("canonical renderer import graph never crosses into services or legacy ui", () => {
  for (const path of relativeImportGraph(sourceFiles(rendererDirectory))) {
    assert.match(
      path,
      /\/src\/(?:renderer|contracts|i18n|application|ports)\//u
    );
    assert.doesNotMatch(readFileSync(path, "utf8"), /qrcode-generator/u);
  }
});

test("published typed subpaths match the canonical build entries", () => {
  const manifest = JSON.parse(
    readFileSync(join(packageDirectory, "package.json"), "utf8")
  ) as {
    exports: Record<string, unknown>;
    publishConfig: {
      exports: Record<string, unknown>;
      typesVersions: Record<string, Record<string, string[]>>;
    };
  };
  const expected = [
    "contracts",
    "composition",
    "core",
    "i18n",
    "renderer",
    "services",
    "ui"
  ];

  assert.equal(manifest.exports["./authorization"], undefined);
  assert.equal(manifest.publishConfig.exports["./authorization"], undefined);
  assert.deepEqual(
    Object.keys(manifest.publishConfig.typesVersions["*"] ?? {}).sort(),
    [...expected].sort()
  );
  for (const subpath of expected) {
    assert.deepEqual(manifest.publishConfig.typesVersions["*"]?.[subpath], [
      `./dist/${subpath}/index.d.ts`
    ]);
  }
});

test("application core never imports the renderer", () => {
  const coreDirectory = join(sourceDirectory, "services", "core");
  for (const path of sourceFiles(coreDirectory)) {
    assert.doesNotMatch(
      readFileSync(path, "utf8"),
      /from\s+["'][^"']*renderer[^"']*["']/u,
      path
    );
  }
});

test("compatibility ui entry only forwards the canonical renderer API", () => {
  assert.equal(
    readFileSync(join(sourceDirectory, "ui", "index.ts"), "utf8").trim(),
    '/** @deprecated Import the canonical implementation from `/renderer`. */\nexport * from "../renderer/index.ts";'
  );
});

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.(?:ts|tsx)$/u.test(entry.name) && !/\.test\./u.test(entry.name)
      ? [path]
      : [];
  });
}

function relativeImportGraph(entrypoints: readonly string[]): Set<string> {
  const visited = new Set<string>();
  const queue = [...entrypoints];
  while (queue.length) {
    const path = queue.pop()!;
    if (visited.has(path)) continue;
    visited.add(path);
    const source = readFileSync(path, "utf8");
    for (const match of source.matchAll(/from\s+["'](\.[^"']+)["']/gu)) {
      const target = resolve(dirname(path), match[1]!);
      const resolved = [target, `${target}.ts`, `${target}.tsx`].find(
        existsSync
      );
      if (resolved) queue.push(resolved);
    }
  }
  return visited;
}
