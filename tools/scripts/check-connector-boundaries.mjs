import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import {
  dirname,
  extname,
  join,
  normalize,
  relative,
  resolve,
  sep
} from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const defaultWorkspaceRoot = resolve(scriptDirectory, "../..");
const sourceExtensions = new Set([
  ".cjs",
  ".cts",
  ".go",
  ".js",
  ".jsx",
  ".mjs",
  ".mts",
  ".ts",
  ".tsx"
]);
const moduleExtensions = [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"];
const ignoredDirectoryNames = new Set([
  ".git",
  ".turbo",
  "dist",
  "node_modules",
  "out",
  "vendor"
]);

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) {
  const workspaceRoot =
    process.env.TUTTI_WORKSPACE_ROOT ?? defaultWorkspaceRoot;
  const violations = inspectConnectorBoundaries(workspaceRoot);
  if (violations.length > 0) {
    process.stderr.write("Connector architecture boundary violations:\n\n");
    for (const violation of violations) {
      process.stderr.write(
        `- [${violation.rule}] ${violation.file}:${violation.line} ${violation.message}\n`
      );
    }
    process.exitCode = 1;
  } else {
    process.stdout.write("Connector architecture boundary check passed\n");
  }
}

export function inspectConnectorBoundaries(workspaceRoot) {
  const root = resolve(workspaceRoot);
  const violations = [];
  const workspacePackages = discoverWorkspacePackages(root);
  const connectorRoot = join(root, "packages/connector");
  const agentGuiRoot = join(root, "packages/agent/gui");

  for (const file of productionFiles(connectorRoot)) {
    inspectConnectorProductionFile({
      file,
      root,
      violations,
      workspacePackages
    });
  }
  for (const file of productionFiles(agentGuiRoot)) {
    inspectAgentGuiProductionFile({ file, root, violations });
  }

  inspectRendererGraph({ root, violations });
  inspectConnectorPackageParity({ root, violations });
  return deduplicateViolations(violations).sort((left, right) =>
    `${left.file}:${left.line}:${left.rule}`.localeCompare(
      `${right.file}:${right.line}:${right.rule}`
    )
  );
}

function inspectConnectorProductionFile({
  file,
  root,
  violations,
  workspacePackages
}) {
  const relativeFile = toPosix(relative(root, file));
  const source = readFileSync(file, "utf8");
  const imports = importSpecifiers(file, source);

  for (const imported of imports) {
    if (isForbiddenConnectorDependency(imported.specifier, relativeFile)) {
      addViolation(violations, {
        file: relativeFile,
        line: lineNumber(source, imported.index),
        message: `Connector production code cannot depend on ${imported.specifier}`,
        rule: "connector-host-dependency"
      });
    }

    if (imported.specifier.startsWith(".")) {
      const packageRoot = connectorPackageRoot(root, file);
      const target = resolve(dirname(file), imported.specifier);
      if (packageRoot && !isWithin(packageRoot, target)) {
        addViolation(violations, {
          file: relativeFile,
          line: lineNumber(source, imported.index),
          message: `relative import escapes the owning Connector package: ${imported.specifier}`,
          rule: "connector-private-deep-import"
        });
      }
      continue;
    }

    inspectWorkspacePackageExport({
      file,
      imported,
      root,
      source,
      violations,
      workspacePackages
    });
  }

  if (isConnectorApplicationFile(relativeFile)) {
    for (const imported of imports) {
      if (
        /^(?:react(?:\/|$)|react-dom(?:\/|$))/u.test(imported.specifier) ||
        /(?:^|\/)renderer(?:\/|$)/iu.test(imported.specifier)
      ) {
        addViolation(violations, {
          file: relativeFile,
          line: lineNumber(source, imported.index),
          message: `Connector application/services code cannot import renderer dependencies: ${imported.specifier}`,
          rule: "connector-application-renderer-dependency"
        });
      }
    }
  }
}

function inspectAgentGuiProductionFile({ file, root, violations }) {
  const relativeFile = toPosix(relative(root, file));
  const source = readFileSync(file, "utf8");

  for (const imported of importSpecifiers(file, source)) {
    if (/(?:^|[/@-])connector(?:[/@-]|$)/iu.test(imported.specifier)) {
      addViolation(violations, {
        file: relativeFile,
        line: lineNumber(source, imported.index),
        message: `AgentGUI must use the neutral primaryCapability slot instead of importing ${imported.specifier}`,
        rule: "agent-gui-connector-import"
      });
    }
  }

  const vocabularyPatterns = [
    {
      label: "Connector domain identifier",
      pattern:
        /\b(?:connectorKey|connectionID|supportedConnectorKeys|connectorStatus|connectorState|connectorAuthorization|connectorAvailability)\b/giu
    },
    {
      label: "Connector wire discriminator",
      pattern: /\btype\s*:\s*["']connector["']/giu
    },
    {
      label: "Connector domain vocabulary",
      pattern: /\bconnectors?\b/giu
    },
    {
      label: "Connector domain type or component",
      pattern: /\bConnector[A-Z][A-Za-z0-9_$]*\b/gu
    }
  ];
  for (const { label, pattern } of vocabularyPatterns) {
    for (const match of source.matchAll(pattern)) {
      addViolation(violations, {
        file: relativeFile,
        line: lineNumber(source, match.index ?? 0),
        message: `${label} belongs to the Connector-owned adapter, not AgentGUI`,
        rule: "agent-gui-connector-vocabulary"
      });
    }
  }
}

function inspectRendererGraph({ root, violations }) {
  const rendererRoot = join(root, "packages/connector/market/src/renderer");
  if (!existsSync(rendererRoot)) return;

  const queue = productionFiles(rendererRoot);
  const seen = new Set();
  while (queue.length > 0) {
    const file = queue.shift();
    if (!file || seen.has(file)) continue;
    seen.add(file);
    const relativeFile = toPosix(relative(root, file));
    const source = readFileSync(file, "utf8");

    for (const match of source.matchAll(/\b(?:window|globalThis)\s*[.[]/gu)) {
      addViolation(violations, {
        file: relativeFile,
        line: lineNumber(source, match.index ?? 0),
        message:
          "Connector renderer must receive state through application ports",
        rule: "connector-renderer-global-state"
      });
    }

    for (const imported of importSpecifiers(file, source)) {
      if (!imported.specifier.startsWith(".")) {
        if (!isAllowedRendererExternal(imported.specifier)) {
          addViolation(violations, {
            file: relativeFile,
            line: lineNumber(source, imported.index),
            message: `renderer dependency is outside React, UI System, or Connector contracts: ${imported.specifier}`,
            rule: "connector-renderer-dependency"
          });
        }
        continue;
      }

      const target = resolveModuleFile(dirname(file), imported.specifier);
      if (!target) continue;
      const relativeTarget = toPosix(relative(root, target));
      if (!isAllowedRendererOwnedPath(relativeTarget)) {
        addViolation(violations, {
          file: relativeFile,
          line: lineNumber(source, imported.index),
          message: `renderer cannot reach services/core/infrastructure or host UI: ${relativeTarget}`,
          rule: "connector-renderer-dependency"
        });
        continue;
      }
      if (!seen.has(target) && sourceExtensions.has(extname(target))) {
        queue.push(target);
      }
    }
  }
}

function inspectConnectorPackageParity({ root, violations }) {
  const connectorRoot = join(root, "packages/connector");
  if (!existsSync(connectorRoot)) return;

  for (const packageDirectory of childDirectories(connectorRoot)) {
    const manifestPath = join(packageDirectory, "package.json");
    if (!existsSync(manifestPath)) continue;
    const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
    if (manifest.private !== false) continue;
    const relativeManifest = toPosix(relative(root, manifestPath));
    const developmentExports = manifest.exports ?? {};
    const publishedExports = manifest.publishConfig?.exports ?? {};
    const developmentKeys = new Set(Object.keys(developmentExports));
    const publishedKeys = new Set(Object.keys(publishedExports));
    compareSets({
      actual: publishedKeys,
      actualLabel: "publishConfig.exports",
      expected: developmentKeys,
      expectedLabel: "workspace exports",
      file: relativeManifest,
      rule: "connector-package-export-parity",
      violations
    });

    const codeExportEntries = Object.entries(developmentExports).filter(
      ([, value]) => exportLeaves(value).some(isCodePath)
    );
    const codeExportKeys = new Set(codeExportEntries.map(([key]) => key));
    for (const [key, value] of codeExportEntries) {
      const sourcePath = exportLeaves(value).find(isCodePath);
      if (!sourcePath || !existsSync(resolve(packageDirectory, sourcePath))) {
        addViolation(violations, {
          file: relativeManifest,
          line: 1,
          message: `workspace export ${key} points to missing source ${sourcePath ?? "<none>"}`,
          rule: "connector-package-export-parity"
        });
      }
      const published = publishedExports[key];
      const publishedTypes = objectLeaf(published, "types");
      const publishedRuntime =
        objectLeaf(published, "import") ?? objectLeaf(published, "default");
      if (!publishedTypes || !publishedRuntime) {
        addViolation(violations, {
          file: relativeManifest,
          line: 1,
          message: `code export ${key} requires both declaration and runtime publish entries`,
          rule: "connector-package-declaration-parity"
        });
      }
    }

    const tsupPath = join(packageDirectory, "tsup.config.ts");
    if (existsSync(tsupPath)) {
      const entries = parseTsupEntries(readFileSync(tsupPath, "utf8"));
      const entryByKey = new Map(
        entries.map((entry) => [exportKeyForEntry(entry.name), entry])
      );
      const buildKeys = new Set(
        entries.map(({ name }) => exportKeyForEntry(name))
      );
      compareSets({
        actual: buildKeys,
        actualLabel: "tsup build entries",
        expected: codeExportKeys,
        expectedLabel: "workspace code exports",
        file: toPosix(relative(root, tsupPath)),
        rule: "connector-package-build-parity",
        violations
      });
      const sourceByKey = new Map(
        codeExportEntries.map(([key, value]) => [
          key,
          normalizeExportPath(exportLeaves(value).find(isCodePath))
        ])
      );
      for (const entry of entries) {
        const key = exportKeyForEntry(entry.name);
        const expectedSource = sourceByKey.get(key);
        if (
          expectedSource &&
          normalizeExportPath(entry.source) !== expectedSource
        ) {
          addViolation(violations, {
            file: toPosix(relative(root, tsupPath)),
            line: 1,
            message: `build entry ${key} uses ${entry.source}; workspace export uses ${expectedSource}`,
            rule: "connector-package-build-parity"
          });
        }
      }
      for (const key of codeExportKeys) {
        const entry = entryByKey.get(key);
        if (!entry) continue;
        const published = publishedExports[key];
        const publishedTypes = objectLeaf(published, "types");
        const publishedRuntime =
          objectLeaf(published, "import") ?? objectLeaf(published, "default");
        const expectedTypes = `./dist/${entry.name}.d.ts`;
        const expectedRuntime = `./dist/${entry.name}.js`;
        if (publishedTypes && publishedTypes !== expectedTypes) {
          addViolation(violations, {
            file: relativeManifest,
            line: 1,
            message: `published declaration ${key} uses ${publishedTypes}; build entry emits ${expectedTypes}`,
            rule: "connector-package-declaration-parity"
          });
        }
        if (publishedRuntime && publishedRuntime !== expectedRuntime) {
          addViolation(violations, {
            file: relativeManifest,
            line: 1,
            message: `published runtime ${key} uses ${publishedRuntime}; build entry emits ${expectedRuntime}`,
            rule: "connector-package-build-parity"
          });
        }
      }
    }

    const expectedTypeKeys = new Set(
      [...codeExportKeys]
        .filter((key) => key !== ".")
        .map((key) => key.replace(/^\.\//u, ""))
    );
    const typesVersions = manifest.publishConfig?.typesVersions?.["*"] ?? {};
    compareSets({
      actual: new Set(Object.keys(typesVersions)),
      actualLabel: "typesVersions",
      expected: expectedTypeKeys,
      expectedLabel: "typed subpath exports",
      file: relativeManifest,
      rule: "connector-package-declaration-parity",
      violations
    });
    for (const key of codeExportKeys) {
      if (key === ".") continue;
      const typeKey = key.replace(/^\.\//u, "");
      const publishedTypes = objectLeaf(publishedExports[key], "types");
      const versionTargets = typesVersions[typeKey];
      if (
        publishedTypes &&
        (!Array.isArray(versionTargets) || versionTargets[0] !== publishedTypes)
      ) {
        addViolation(violations, {
          file: relativeManifest,
          line: 1,
          message: `typesVersions ${typeKey} must point to ${publishedTypes}`,
          rule: "connector-package-declaration-parity"
        });
      }
    }

    const rootTypes = objectLeaf(publishedExports["."], "types");
    if (rootTypes && manifest.publishConfig?.types !== rootTypes) {
      addViolation(violations, {
        file: relativeManifest,
        line: 1,
        message: `publishConfig.types must match the root declaration entry ${rootTypes}`,
        rule: "connector-package-declaration-parity"
      });
    }
    const rootSource = exportLeaves(developmentExports["."]).find(isCodePath);
    if (rootSource && manifest.types !== rootSource) {
      addViolation(violations, {
        file: relativeManifest,
        line: 1,
        message: `workspace types must match the root source entry ${rootSource}`,
        rule: "connector-package-declaration-parity"
      });
    }

    if (manifest.name === "@tutti-os/connector-market") {
      inspectConnectorRendererExports({
        developmentExports,
        packageDirectory,
        publishedExports,
        relativeManifest,
        violations
      });
    }
  }
}

function inspectConnectorRendererExports({
  developmentExports,
  packageDirectory,
  publishedExports,
  relativeManifest,
  violations
}) {
  for (const key of ["./renderer", "./ui"]) {
    if (!(key in developmentExports) || !(key in publishedExports)) {
      addViolation(violations, {
        file: relativeManifest,
        line: 1,
        message: `${key} must exist in workspace and publish exports during the compatibility window`,
        rule: "connector-renderer-export"
      });
    }
  }
  if (
    normalizeExportPath(developmentExports["./renderer"]) !==
    "src/renderer/index.ts"
  ) {
    addViolation(violations, {
      file: relativeManifest,
      line: 1,
      message: "./renderer must be the canonical Connector renderer source",
      rule: "connector-renderer-export"
    });
  }

  const compatibilityPath = join(packageDirectory, "src/ui/index.ts");
  if (!existsSync(compatibilityPath)) {
    addViolation(violations, {
      file: relativeManifest,
      line: 1,
      message: "./ui compatibility entry is missing",
      rule: "connector-renderer-export"
    });
    return;
  }
  const compatibilitySource = readFileSync(compatibilityPath, "utf8").replace(
    /\/\*[\s\S]*?\*\/|\/\/.*$/gmu,
    ""
  );
  if (
    !/^\s*export\s+\*\s+from\s+["']\.\.\/renderer\/index\.ts["'];?\s*$/u.test(
      compatibilitySource
    )
  ) {
    addViolation(violations, {
      file: toPosix(relative(packageDirectory, compatibilityPath)),
      line: 1,
      message: "./ui may only re-export the canonical ./renderer entry",
      rule: "connector-renderer-export"
    });
  }
}

function inspectWorkspacePackageExport({
  file,
  imported,
  root,
  source,
  violations,
  workspacePackages
}) {
  const targetPackage = workspacePackages.find(
    ({ name }) =>
      imported.specifier === name || imported.specifier.startsWith(`${name}/`)
  );
  if (!targetPackage || isWithin(targetPackage.directory, file)) return;
  const subpath =
    imported.specifier === targetPackage.name
      ? "."
      : `./${imported.specifier.slice(targetPackage.name.length + 1)}`;
  if (Object.hasOwn(targetPackage.manifest.exports ?? {}, subpath)) return;
  addViolation(violations, {
    file: toPosix(relative(root, file)),
    line: lineNumber(source, imported.index),
    message: `${imported.specifier} is not a public workspace package export`,
    rule: "connector-private-deep-import"
  });
}

function isForbiddenConnectorDependency(specifier, importerFile) {
  const normalized = toPosix(specifier);
  if (
    /(?:^|\/)packages\/clients\/market-go(?:\/|$)/u.test(normalized) &&
    isMarketSourceAdapter(importerFile)
  ) {
    return false;
  }
  return (
    /^@tutti-os\/(?:agent(?:-|\/|$)|desktop(?:-|\/|$)|clients?(?:-|\/|$))/u.test(
      normalized
    ) ||
    /^@tsh(?:-|\/|$)/u.test(normalized) ||
    /^(?:tsh|tsh-server)(?:\/|$)/u.test(normalized) ||
    /(?:^|\/)packages\/agent(?:\/|$)/u.test(normalized) ||
    /(?:^|\/)apps\/desktop(?:\/|$)/u.test(normalized) ||
    /(?:^|\/)packages\/clients(?:\/|$)/u.test(normalized) ||
    /(?:^|\/)preload(?:\/|$)/u.test(normalized) ||
    /github\.com\/[^/]+\/tsh(?:-server)?(?:\/|$)/u.test(normalized)
  );
}

function isMarketSourceAdapter(importerFile) {
  return (
    importerFile === "packages/connector/daemon/catalog_source.go" ||
    /^packages\/connector\/market\/source(?:\/|$)/u.test(importerFile)
  );
}

function isConnectorApplicationFile(relativeFile) {
  return (
    /^packages\/connector\/[^/]+\/src\/(?:application|services)(?:\/|$)/u.test(
      relativeFile
    ) || /^packages\/connector\/(?:host|daemon)(?:\/|$)/u.test(relativeFile)
  );
}

function isAllowedRendererExternal(specifier) {
  return (
    /^(?:react|react-dom)(?:\/|$)/u.test(specifier) ||
    /^@tutti-os\/ui-system(?:\/|$)/u.test(specifier) ||
    specifier === "@tutti-os/ui-i18n-runtime" ||
    /^@tutti-os\/connector-authorization-protocol(?:\/|$)/u.test(specifier)
  );
}

function isAllowedRendererOwnedPath(relativeTarget) {
  return /^packages\/connector\/market\/src\/(?:renderer|contracts|i18n|application|ports)(?:\/|$)/u.test(
    relativeTarget
  );
}

function importSpecifiers(file, source) {
  if (file.endsWith(".go")) return goImportSpecifiers(source);
  const imports = [];
  const pattern =
    /\bfrom\s*["']([^"']+)["']|\bimport\s*["']([^"']+)["']|\b(?:import|require)\s*\(\s*["']([^"']+)["']/gu;
  for (const match of source.matchAll(pattern)) {
    const specifier = match[1] ?? match[2] ?? match[3];
    imports.push({
      index: (match.index ?? 0) + match[0].indexOf(specifier),
      specifier
    });
  }
  return imports;
}

function goImportSpecifiers(source) {
  const imports = [];
  const pattern =
    /(?:^|\n)\s*(?:import\s+)?(?:[A-Za-z_$][\w$]*\s+)?"([^"]+)"/gu;
  for (const match of source.matchAll(pattern)) {
    const quoteOffset = match[0].lastIndexOf('"') - match[1].length - 1;
    imports.push({
      index: (match.index ?? 0) + Math.max(0, quoteOffset),
      specifier: match[1]
    });
  }
  return imports;
}

function productionFiles(directory) {
  if (!existsSync(directory)) return [];
  return walkFiles(directory).filter((file) => {
    if (!sourceExtensions.has(extname(file))) return false;
    const name = file.split(sep).at(-1) ?? "";
    return !(
      /(?:^|[._-])(?:test|spec)(?:[._-]|$)/iu.test(name) ||
      name.endsWith("_test.go") ||
      name.endsWith(".d.ts")
    );
  });
}

function walkFiles(directory) {
  const files = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.isDirectory() && ignoredDirectoryNames.has(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...walkFiles(path));
    else if (entry.isFile()) files.push(path);
  }
  return files;
}

function childDirectories(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => join(directory, entry.name));
}

function discoverWorkspacePackages(root) {
  const packagesRoot = join(root, "packages");
  if (!existsSync(packagesRoot)) return [];
  const packages = [];
  for (const group of childDirectories(packagesRoot)) {
    for (const directory of childDirectories(group)) {
      const manifestPath = join(directory, "package.json");
      if (!existsSync(manifestPath)) continue;
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
      if (typeof manifest.name === "string") {
        packages.push({ directory, manifest, name: manifest.name });
      }
    }
  }
  return packages.sort((left, right) => right.name.length - left.name.length);
}

function connectorPackageRoot(root, file) {
  const relativeFile = toPosix(
    relative(join(root, "packages/connector"), file)
  );
  const packageName = relativeFile.split("/")[0];
  return packageName ? join(root, "packages/connector", packageName) : null;
}

function resolveModuleFile(directory, specifier) {
  const candidate = resolve(directory, specifier);
  const candidates = [
    candidate,
    ...moduleExtensions.map((extension) => `${candidate}${extension}`),
    ...moduleExtensions.map((extension) => join(candidate, `index${extension}`))
  ];
  return candidates.find((path) => existsSync(path) && statSync(path).isFile());
}

function parseTsupEntries(source) {
  const marker = /\bentry\s*:\s*\{/gu.exec(source);
  if (!marker) return [];
  const start = source.indexOf("{", marker.index);
  const end = balancedBraceEnd(source, start);
  if (end === -1) return [];
  const body = source.slice(start + 1, end);
  const entries = [];
  const pattern =
    /(?:["']([^"']+)["']|([A-Za-z_$][\w$]*))\s*:\s*["']([^"']+)["']/gu;
  for (const match of body.matchAll(pattern)) {
    entries.push({ name: match[1] ?? match[2], source: match[3] });
  }
  return entries;
}

function balancedBraceEnd(source, start) {
  let depth = 0;
  let quote = null;
  let escaped = false;
  for (let index = start; index < source.length; index += 1) {
    const character = source[index];
    if (quote) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === quote) quote = null;
      continue;
    }
    if (character === '"' || character === "'" || character === "`") {
      quote = character;
    } else if (character === "{") {
      depth += 1;
    } else if (character === "}") {
      depth -= 1;
      if (depth === 0) return index;
    }
  }
  return -1;
}

function exportKeyForEntry(name) {
  if (name === "index") return ".";
  if (name.endsWith("/index")) return `./${name.slice(0, -"/index".length)}`;
  return `./${name}`;
}

function exportLeaves(value) {
  if (typeof value === "string") return [value];
  if (!value || typeof value !== "object") return [];
  return Object.values(value).flatMap(exportLeaves);
}

function objectLeaf(value, key) {
  return value && typeof value === "object" && typeof value[key] === "string"
    ? value[key]
    : null;
}

function isCodePath(path) {
  return typeof path === "string" && /\.[cm]?[jt]sx?$/u.test(path);
}

function normalizeExportPath(path) {
  return typeof path === "string"
    ? toPosix(normalize(path)).replace(/^\.\//u, "")
    : "";
}

function compareSets({
  actual,
  actualLabel,
  expected,
  expectedLabel,
  file,
  rule,
  violations
}) {
  for (const missing of [...expected].filter((key) => !actual.has(key))) {
    addViolation(violations, {
      file,
      line: 1,
      message: `${actualLabel} is missing ${missing} from ${expectedLabel}`,
      rule
    });
  }
  for (const extra of [...actual].filter((key) => !expected.has(key))) {
    addViolation(violations, {
      file,
      line: 1,
      message: `${actualLabel} exposes ${extra} without a matching ${expectedLabel} entry`,
      rule
    });
  }
}

function addViolation(violations, violation) {
  violations.push(violation);
}

function deduplicateViolations(violations) {
  const seen = new Set();
  return violations.filter((violation) => {
    const key = `${violation.rule}:${violation.file}:${violation.line}:${violation.message}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function lineNumber(source, index) {
  return source.slice(0, index).split("\n").length;
}

function isWithin(directory, candidate) {
  const relativePath = relative(resolve(directory), resolve(candidate));
  return (
    relativePath === "" ||
    (!relativePath.startsWith("..") && !relativePath.includes(`..${sep}`))
  );
}

function toPosix(path) {
  return path.replaceAll("\\", "/");
}
