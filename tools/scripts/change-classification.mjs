import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

import { createIsolatedGitEnvironment } from "./git-environment.mjs";
import { selectedRepositoryCheckGroups } from "./repository-checks.mjs";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const workspaceRoot = resolve(scriptDirectory, "../..");

export function classifyChangedFiles(
  changedFiles,
  {
    isPackageManifestPackRelevant = () => true,
    releasePackages = discoverReleasePackages()
  } = {}
) {
  const normalizedFiles = changedFiles.map((file) =>
    file.replaceAll("\\", "/")
  );
  const groups = selectedRepositoryCheckGroups(normalizedFiles);
  const packSelection = selectPackPackages(normalizedFiles, {
    isPackageManifestPackRelevant,
    releasePackages
  });

  return {
    packAll: packSelection.packAll,
    packPackages: packSelection.packageNames,
    runBoundaries: groups.has("boundaries"),
    runContracts: groups.has("contracts"),
    runGenerated: groups.has("generated"),
    runGo: normalizedFiles.some(isGoRelevant),
    runPack: packSelection.packAll || packSelection.packageNames.length > 0,
    runTs: normalizedFiles.some(isTypeScriptRelevant)
  };
}

export function discoverReleasePackages(root = workspaceRoot) {
  const packagesRoot = join(root, "packages");
  const packages = [];

  for (const group of readDirectories(packagesRoot)) {
    const groupRoot = join(packagesRoot, group);
    for (const packageName of readDirectories(groupRoot)) {
      const packageRoot = join(groupRoot, packageName);
      const manifestPath = join(packageRoot, "package.json");
      try {
        const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
        if (
          manifest.private === false &&
          manifest.publishConfig?.access === "public"
        ) {
          packages.push({
            name: manifest.name,
            root: packageRoot.slice(root.length + 1).replaceAll("\\", "/")
          });
        }
      } catch {
        // Non-package directories are outside the release surface.
      }
    }
  }

  return packages.sort((left, right) => left.root.localeCompare(right.root));
}

export function formatClassificationOutputs(classification) {
  return [
    ["pack_all", classification.packAll],
    ["pack_packages", JSON.stringify(classification.packPackages)],
    ["run_boundaries", classification.runBoundaries],
    ["run_contracts", classification.runContracts],
    ["run_generated", classification.runGenerated],
    ["run_go", classification.runGo],
    ["run_pack", classification.runPack],
    ["run_ts", classification.runTs]
  ]
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function isGoRelevant(file) {
  return (
    file.endsWith(".go") ||
    /(?:^|\/)go\.(?:mod|sum)$/u.test(file) ||
    ["go.work", "go.work.sum"].includes(file) ||
    file.startsWith("services/tuttid/.golangci")
  );
}

function isTypeScriptRelevant(file) {
  return (
    /\.(?:cjs|cts|js|jsx|mjs|mts|ts|tsx)$/u.test(file) ||
    /(?:^|\/)(?:package\.json|tsconfig[^/]*\.json)$/u.test(file) ||
    ["pnpm-lock.yaml", "pnpm-workspace.yaml"].includes(file) ||
    file.startsWith("packages/configs/")
  );
}

function selectPackPackages(
  files,
  { isPackageManifestPackRelevant, releasePackages }
) {
  const packAll = files.some((file) => isGlobalPackRelevant(file));
  if (packAll) {
    return {
      packAll: true,
      packageNames: releasePackages.map((packageConfig) => packageConfig.name)
    };
  }

  const packageNames = new Set();
  for (const file of files) {
    const packageConfig = releasePackages.find(
      ({ root }) => file === root || file.startsWith(`${root}/`)
    );

    if (!packageConfig) {
      if (/^packages\/[^/]+\/[^/]+\/package\.json$/u.test(file)) {
        return {
          packAll: true,
          packageNames: releasePackages.map(
            (releasePackage) => releasePackage.name
          )
        };
      }
      continue;
    }

    const relativePath = file.slice(packageConfig.root.length + 1);
    if (relativePath === "package.json") {
      if (!isPackageManifestPackRelevant(file)) {
        continue;
      }
      return {
        packAll: true,
        packageNames: releasePackages.map(
          (releasePackage) => releasePackage.name
        )
      };
    }
    if (isPackagePackRelevantPath(relativePath)) {
      packageNames.add(packageConfig.name);
    }
  }

  return {
    packAll: false,
    packageNames: [...packageNames].sort()
  };
}

function isGlobalPackRelevant(file) {
  return (
    ["package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"].includes(file) ||
    file === ".changeset/config.json" ||
    file === "tools/scripts/build-npm-packages.mjs" ||
    file === "tools/scripts/check-package-packs.mjs" ||
    file === "tools/scripts/npm-release-packages.mjs" ||
    file === "tools/scripts/run-package-pack-check.mjs"
  );
}

export function isPackagePackRelevantPath(file) {
  return !(
    /\.(?:test|spec)\.(?:ts|tsx|mts|cts|js|jsx|mjs|cjs)$/u.test(file) ||
    /^vitest(?:\.[^/]+)*\.(?:ts|tsx|mts|cts|js|jsx|mjs|cjs)$/u.test(
      basename(file)
    )
  );
}

export function createPackageManifestPackRelevance({
  baseRef,
  root = workspaceRoot
}) {
  const cache = new Map();

  return (file) => {
    if (!cache.has(file)) {
      cache.set(file, isPackageManifestPackRelevant(baseRef, file, root));
    }
    return cache.get(file);
  };
}

function isPackageManifestPackRelevant(baseRef, file, root) {
  try {
    const before = normalizedManifestAtRef(baseRef, file, root);
    const candidates = [
      normalizedManifestAtRef("HEAD", file, root),
      JSON.stringify(
        withoutTestScripts(JSON.parse(readFileSync(join(root, file), "utf8")))
      )
    ];
    return candidates.some((candidate) => candidate !== before);
  } catch {
    return true;
  }
}

function normalizedManifestAtRef(ref, file, root) {
  const manifest = JSON.parse(
    execFileSync("git", ["show", `${ref}:${file}`], {
      cwd: root,
      encoding: "utf8",
      env: createIsolatedGitEnvironment(root)
    })
  );
  return JSON.stringify(withoutTestScripts(manifest));
}

function withoutTestScripts(manifest) {
  const normalized = structuredClone(manifest);
  if (!normalized.scripts || typeof normalized.scripts !== "object") {
    return normalized;
  }

  for (const name of Object.keys(normalized.scripts)) {
    if (/^(?:pre|post)?test(?::|$)/u.test(name)) {
      delete normalized.scripts[name];
    }
  }
  if (Object.keys(normalized.scripts).length === 0) {
    delete normalized.scripts;
  }
  return normalized;
}

function readDirectories(root) {
  try {
    return readdirSync(root, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
  } catch {
    return [];
  }
}

function readOption(name) {
  const index = process.argv.indexOf(name);
  return index === -1 ? null : (process.argv[index + 1] ?? null);
}

function isMainModule() {
  return Boolean(
    process.argv[1] &&
    resolve(process.argv[1]) === fileURLToPath(import.meta.url)
  );
}

if (isMainModule()) {
  const base = readOption("--base");
  if (!base) {
    throw new Error("--base is required");
  }
  const changedFiles = execFileSync(
    "git",
    ["diff", "--name-only", `${base}...HEAD`],
    { cwd: workspaceRoot, encoding: "utf8" }
  )
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  const output = formatClassificationOutputs(
    classifyChangedFiles(changedFiles, {
      isPackageManifestPackRelevant: createPackageManifestPackRelevance({
        baseRef: base
      })
    })
  );
  const githubOutput = readOption("--github-output");
  if (githubOutput) {
    writeFileSync(githubOutput, `${output}\n`, { flag: "a" });
  }
  console.log(output);
}
