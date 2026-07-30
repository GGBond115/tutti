import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readdirSync,
  rmSync,
  writeFileSync
} from "node:fs";
import { hostname, tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  getElectronCacheLockPath,
  getElectronCacheRoot,
  withElectronCacheLock
} from "./prewarm-electron.mjs";

test("resolves Electron's platform cache locations", () => {
  assert.equal(
    getElectronCacheRoot({
      env: {},
      homeDirectory: "/Users/developer",
      platform: "darwin"
    }),
    "/Users/developer/Library/Caches/electron"
  );
  assert.equal(
    getElectronCacheRoot({
      env: { XDG_CACHE_HOME: "/cache" },
      homeDirectory: "/home/developer",
      platform: "linux"
    }),
    "/cache/electron"
  );
  assert.equal(
    getElectronCacheRoot({
      env: { electron_config_cache: "/custom/electron-cache" },
      platform: "darwin"
    }),
    "/custom/electron-cache"
  );
});

test("serializes prewarm calls for the same Electron artifact", async () => {
  const cacheRoot = mkdtempSync(join(tmpdir(), "tutti-electron-cache-"));
  const lockInput = {
    arch: "arm64",
    cacheRoot,
    electronVersion: "43.2.0",
    platform: "darwin",
    pollIntervalMilliseconds: 1
  };
  let releaseFirstOperation;
  let secondOperationStarted = false;
  let signalFirstOperation;
  const firstOperationStarted = new Promise((resolvePromise) => {
    signalFirstOperation = resolvePromise;
  });

  try {
    const first = withElectronCacheLock(lockInput, async () => {
      signalFirstOperation();
      await new Promise((resolvePromise) => {
        releaseFirstOperation = resolvePromise;
      });
    });
    await firstOperationStarted;

    const second = withElectronCacheLock(lockInput, async () => {
      secondOperationStarted = true;
    });
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 20));
    assert.equal(secondOperationStarted, false);

    releaseFirstOperation();
    await Promise.all([first, second]);
    assert.equal(secondOperationStarted, true);
  } finally {
    rmSync(cacheRoot, { force: true, recursive: true });
  }
});

test("reclaims a prewarm lock left by a dead local process", async () => {
  const cacheRoot = mkdtempSync(join(tmpdir(), "tutti-electron-cache-"));
  const lockInput = {
    arch: "arm64",
    cacheRoot,
    electronVersion: "43.2.0",
    platform: "darwin",
    pollIntervalMilliseconds: 1
  };
  const lockPath = getElectronCacheLockPath(lockInput);
  mkdirSync(cacheRoot, { recursive: true });
  writeFileSync(
    lockPath,
    `${JSON.stringify({ hostName: hostname(), processId: 99_999_999 })}\n`
  );

  try {
    const result = await withElectronCacheLock(lockInput, () => "ready");
    assert.equal(result, "ready");
  } finally {
    rmSync(cacheRoot, { force: true, recursive: true });
  }
});

test("serializes concurrent recovery of a stale prewarm lock", async () => {
  const cacheRoot = mkdtempSync(join(tmpdir(), "tutti-electron-cache-"));
  const readyDirectory = join(cacheRoot, "ready");
  const startPath = join(cacheRoot, "start");
  const activePath = join(cacheRoot, "active");
  const lockInput = {
    arch: "arm64",
    cacheRoot,
    electronVersion: "43.2.0",
    platform: "darwin",
    pollIntervalMilliseconds: 1
  };
  const lockPath = getElectronCacheLockPath(lockInput);

  mkdirSync(cacheRoot, { recursive: true });
  mkdirSync(readyDirectory, { recursive: true });
  writeFileSync(
    lockPath,
    `${JSON.stringify({ hostName: hostname(), processId: 99_999_999 })}\n`
  );

  try {
    const children = Array.from({ length: 4 }, () =>
      spawn(process.execPath, ["--input-type=module", "--eval", childScript], {
        env: {
          ...process.env,
          TUTTI_ELECTRON_LOCK_ACTIVE_PATH: activePath,
          TUTTI_ELECTRON_LOCK_INPUT: JSON.stringify(lockInput),
          TUTTI_ELECTRON_LOCK_MODULE_URL: new URL(
            "./prewarm-electron.mjs",
            import.meta.url
          ).href,
          TUTTI_ELECTRON_LOCK_READY_DIRECTORY: readyDirectory,
          TUTTI_ELECTRON_LOCK_START_PATH: startPath
        }
      })
    );
    await waitFor(() => readdirSync(readyDirectory).length === children.length);
    writeFileSync(startPath, "start\n");

    const outcomes = await Promise.all(children.map(waitForChild));
    for (const outcome of outcomes) {
      assert.equal(outcome.code, 0, outcome.stderr);
    }
  } finally {
    rmSync(cacheRoot, { force: true, recursive: true });
  }
});

const childScript = `
  import { closeSync, existsSync, openSync, rmSync, writeFileSync } from "node:fs";
  import { join } from "node:path";
  const { withElectronCacheLock } = await import(process.env.TUTTI_ELECTRON_LOCK_MODULE_URL);

  const input = JSON.parse(process.env.TUTTI_ELECTRON_LOCK_INPUT);
  const readyDirectory = process.env.TUTTI_ELECTRON_LOCK_READY_DIRECTORY;
  const startPath = process.env.TUTTI_ELECTRON_LOCK_START_PATH;
  const activePath = process.env.TUTTI_ELECTRON_LOCK_ACTIVE_PATH;
  writeFileSync(join(readyDirectory, String(process.pid)), "ready\\n");
  while (!existsSync(startPath)) {
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 1));
  }
  await withElectronCacheLock(input, async () => {
    const descriptor = openSync(activePath, "wx");
    try {
      await new Promise((resolvePromise) => setTimeout(resolvePromise, 30));
    } finally {
      closeSync(descriptor);
      rmSync(activePath, { force: true });
    }
  });
`;

async function waitFor(predicate) {
  const deadline = Date.now() + 5_000;
  while (!predicate()) {
    if (Date.now() >= deadline) {
      throw new Error("timed out waiting for concurrent lock test children");
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 5));
  }
}

function waitForChild(child) {
  return new Promise((resolvePromise, rejectPromise) => {
    let stderr = "";
    child.stderr.on("data", (data) => {
      stderr += data;
    });
    child.on("error", rejectPromise);
    child.on("close", (code) => {
      resolvePromise({ code, stderr });
    });
  });
}
