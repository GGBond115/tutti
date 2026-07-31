#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

export const defaultTuttiAppRuntimeCatalogURL =
  "https://d1x7gb6wqsqmnm.cloudfront.net/tutti-app-runtimes/catalog.json";

export function validateTuttiAppRuntimeRelease({ catalog, lock }) {
  const expectedVersion = requiredString(
    lock?.runtimeVersion,
    "runtimeVersion"
  );
  const expectedVersionParts = parseRuntimeVersion(
    expectedVersion,
    "runtime lock runtimeVersion"
  );
  const platforms = Object.keys(lock?.platforms ?? {});
  if (platforms.length === 0) {
    throw new Error("runtime lock must declare at least one platform");
  }
  if (catalog?.schemaVersion !== "tutti.app.runtimes.v2") {
    throw new Error(
      `runtime catalog schemaVersion must be tutti.app.runtimes.v2, got ${JSON.stringify(catalog?.schemaVersion)}`
    );
  }

  const mismatches = [];
  for (const platform of platforms) {
    const publishedVersion = catalog.runtimes?.[platform]?.version;
    if (typeof publishedVersion !== "string") {
      mismatches.push(
        `${platform}: expected at least ${expectedVersion}, got missing`
      );
      continue;
    }
    let publishedVersionParts;
    try {
      publishedVersionParts = parseRuntimeVersion(
        publishedVersion,
        `${platform} published runtime version`
      );
    } catch (error) {
      mismatches.push(`${platform}: ${error.message}`);
      continue;
    }
    if (
      compareRuntimeVersions(publishedVersionParts, expectedVersionParts) < 0
    ) {
      mismatches.push(
        `${platform}: expected at least ${expectedVersion}, got ${publishedVersion}`
      );
    }
  }
  if (mismatches.length > 0) {
    throw new Error(
      [
        `managed app runtime ${expectedVersion} is not published for every desktop platform:`,
        ...mismatches.map((mismatch) => `- ${mismatch}`),
        "Run Publish Tutti App Runtime for this release target and verify the production catalog before promoting the desktop release."
      ].join("\n")
    );
  }

  return { platforms, runtimeVersion: expectedVersion };
}

export async function verifyTuttiAppRuntimeRelease({
  catalogURL = defaultTuttiAppRuntimeCatalogURL,
  fetchImpl = globalThis.fetch,
  lockFile = "config/tutti.app-runtime.lock.json"
} = {}) {
  const lock = JSON.parse(await readFile(lockFile, "utf8"));
  const response = await fetchImpl(catalogURL, {
    headers: {
      accept: "application/json",
      "cache-control": "no-cache"
    },
    redirect: "follow",
    signal: AbortSignal.timeout(15_000)
  });
  if (!response.ok) {
    throw new Error(
      `failed to fetch managed app runtime catalog ${catalogURL}: HTTP ${response.status}`
    );
  }
  const result = validateTuttiAppRuntimeRelease({
    catalog: await response.json(),
    lock
  });
  return { ...result, catalogURL };
}

export async function main(argv = process.argv.slice(2)) {
  const options = parseArgs(argv);
  const result = await verifyTuttiAppRuntimeRelease(options);
  console.log(
    `Verified managed app runtime ${result.runtimeVersion} for ${result.platforms.join(", ")} at ${result.catalogURL}`
  );
}

function parseArgs(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    switch (arg) {
      case "--catalog-url":
        options.catalogURL = requiredArgument(argv[++index], arg);
        break;
      case "--lock-file":
        options.lockFile = requiredArgument(argv[++index], arg);
        break;
      default:
        throw new Error(`unknown argument ${arg}`);
    }
  }
  return options;
}

function requiredArgument(value, flag) {
  if (!value || value.startsWith("--")) {
    throw new Error(`${flag} requires a value`);
  }
  return value;
}

function requiredString(value, name) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`runtime lock ${name} is required`);
  }
  return value.trim();
}

function parseRuntimeVersion(value, name) {
  const match = /^(\d{4})\.(\d{2})\.(0|[1-9]\d*)$/u.exec(value);
  if (!match) {
    throw new Error(
      `${name} must use YYYY.MM.PATCH, got ${JSON.stringify(value)}`
    );
  }
  const parts = match.slice(1).map(Number);
  if (parts[1] < 1 || parts[1] > 12) {
    throw new Error(`${name} has an invalid month in ${JSON.stringify(value)}`);
  }
  return parts;
}

function compareRuntimeVersions(left, right) {
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return left[index] - right[index];
    }
  }
  return 0;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
