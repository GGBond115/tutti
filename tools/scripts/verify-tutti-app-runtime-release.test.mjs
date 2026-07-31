import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  defaultTuttiAppRuntimeCatalogURL,
  validateTuttiAppRuntimeRelease,
  verifyTuttiAppRuntimeRelease
} from "./verify-tutti-app-runtime-release.mjs";

const runtimeSourcePath = new URL(
  "../../services/tuttid/service/managedruntime/runtime.go",
  import.meta.url
);

test("validateTuttiAppRuntimeRelease accepts the locked version for every platform", () => {
  const result = validateTuttiAppRuntimeRelease({
    catalog: runtimeCatalog("2026.07.0"),
    lock: runtimeLock("2026.07.0")
  });

  assert.deepEqual(result, {
    platforms: ["darwin-arm64", "linux-amd64"],
    runtimeVersion: "2026.07.0"
  });
});

test("validateTuttiAppRuntimeRelease accepts newer published runtimes", () => {
  const catalog = runtimeCatalog("2026.08.0");
  catalog.runtimes["linux-amd64"].version = "2027.01.2";

  const result = validateTuttiAppRuntimeRelease({
    catalog,
    lock: runtimeLock("2026.07.3")
  });

  assert.equal(result.runtimeVersion, "2026.07.3");
});

test("validateTuttiAppRuntimeRelease rejects stale or missing platforms with release guidance", () => {
  const catalog = runtimeCatalog("2026.06.0");
  delete catalog.runtimes["linux-amd64"];

  assert.throws(
    () =>
      validateTuttiAppRuntimeRelease({
        catalog,
        lock: runtimeLock("2026.07.0")
      }),
    (error) => {
      assert.match(
        error.message,
        /darwin-arm64: expected at least 2026\.07\.0, got 2026\.06\.0/
      );
      assert.match(
        error.message,
        /linux-amd64: expected at least 2026\.07\.0, got missing/
      );
      assert.match(error.message, /Publish Tutti App Runtime/);
      assert.match(error.message, /before promoting the desktop release/);
      return true;
    }
  );
});

test("validateTuttiAppRuntimeRelease rejects malformed runtime versions", () => {
  assert.throws(
    () =>
      validateTuttiAppRuntimeRelease({
        catalog: runtimeCatalog("2026.7.0"),
        lock: runtimeLock("2026.07.0")
      }),
    /darwin-arm64: darwin-arm64 published runtime version must use YYYY\.MM\.PATCH/
  );
  assert.throws(
    () =>
      validateTuttiAppRuntimeRelease({
        catalog: runtimeCatalog("2026.07.0"),
        lock: runtimeLock("2026.13.0")
      }),
    /runtime lock runtimeVersion has an invalid month/
  );
});

test("verifyTuttiAppRuntimeRelease fetches the production catalog without cache", async () => {
  const tempDir = await mkdtemp(path.join(tmpdir(), "tutti-runtime-gate-"));
  const lockFile = path.join(tempDir, "runtime-lock.json");
  await writeFile(
    lockFile,
    `${JSON.stringify(runtimeLock("2026.07.0"))}\n`,
    "utf8"
  );
  let request;

  const result = await verifyTuttiAppRuntimeRelease({
    fetchImpl: async (url, options) => {
      request = { options, url };
      return {
        json: async () => runtimeCatalog("2026.07.0"),
        ok: true,
        status: 200
      };
    },
    lockFile
  });

  assert.equal(request.url, defaultTuttiAppRuntimeCatalogURL);
  assert.equal(request.options.headers["cache-control"], "no-cache");
  assert.equal(request.options.redirect, "follow");
  assert.equal(result.runtimeVersion, "2026.07.0");
});

test("release gate default catalog stays aligned with tuttid", async () => {
  const runtimeSource = await readFile(runtimeSourcePath, "utf8");

  assert.match(
    runtimeSource,
    new RegExp(
      `defaultTuttiAppRuntimeCatalogURL = ${JSON.stringify(defaultTuttiAppRuntimeCatalogURL)}`
    )
  );
});

function runtimeLock(runtimeVersion) {
  return {
    runtimeVersion,
    platforms: {
      "darwin-arm64": {},
      "linux-amd64": {}
    }
  };
}

function runtimeCatalog(runtimeVersion) {
  return {
    schemaVersion: "tutti.app.runtimes.v2",
    runtimes: {
      "darwin-arm64": { version: runtimeVersion },
      "linux-amd64": { version: runtimeVersion }
    }
  };
}
