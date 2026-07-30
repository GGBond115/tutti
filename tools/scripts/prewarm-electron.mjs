import {
  closeSync,
  mkdirSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync
} from "node:fs";
import { createRequire } from "node:module";
import { homedir, hostname } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const workspaceRoot = resolve(scriptDirectory, "..", "..");
const desktopPackageJson = join(
  workspaceRoot,
  "apps",
  "desktop",
  "package.json"
);
const lockPollIntervalMilliseconds = 100;

if (isMainModule()) {
  prewarmElectron().catch((error) => {
    console.error(error instanceof Error ? error.stack : error);
    process.exitCode = 1;
  });
}

export async function prewarmElectron() {
  const desktopRequire = createRequire(desktopPackageJson);
  const electronPackage = desktopRequire("electron/package.json");
  const cacheRoot = getElectronCacheRoot({ env: process.env });
  const electronPath = await withElectronCacheLock(
    {
      arch: process.arch,
      cacheRoot,
      electronVersion: electronPackage.version,
      platform: process.platform
    },
    () => desktopRequire("electron")
  );

  console.log(
    `Electron ${electronPackage.version} is ready in this worktree: ${electronPath}`
  );
}

export function getElectronCacheRoot({
  env = process.env,
  homeDirectory = homedir(),
  platform = process.platform
} = {}) {
  const configuredCacheRoot = env.electron_config_cache?.trim();
  if (configuredCacheRoot) {
    return resolve(configuredCacheRoot);
  }

  if (platform === "darwin") {
    return join(homeDirectory, "Library", "Caches", "electron");
  }

  if (platform === "win32") {
    return join(
      env.LOCALAPPDATA?.trim() || join(homeDirectory, "AppData", "Local"),
      "electron",
      "Cache"
    );
  }

  return join(
    env.XDG_CACHE_HOME?.trim() || join(homeDirectory, ".cache"),
    "electron"
  );
}

export function getElectronCacheLockPath({
  arch,
  cacheRoot,
  electronVersion,
  platform
}) {
  const artifactIdentity = [electronVersion, platform, arch]
    .map((value) => value.replaceAll(/[^a-zA-Z0-9._-]/gu, "_"))
    .join("-");
  return join(cacheRoot, `.tutti-electron-prewarm-${artifactIdentity}.lock`);
}

export async function withElectronCacheLock(
  {
    arch,
    cacheRoot,
    electronVersion,
    hostName = hostname(),
    platform,
    pollIntervalMilliseconds = lockPollIntervalMilliseconds,
    processId = process.pid
  },
  operation
) {
  const lockPath = getElectronCacheLockPath({
    arch,
    cacheRoot,
    electronVersion,
    platform
  });
  const descriptor = await acquireLock({
    hostName,
    lockPath,
    pollIntervalMilliseconds,
    processId
  });

  try {
    return await operation();
  } finally {
    closeSync(descriptor);
    rmSync(lockPath, { force: true });
  }
}

async function acquireLock({
  hostName,
  lockPath,
  pollIntervalMilliseconds,
  processId
}) {
  mkdirSync(dirname(lockPath), { recursive: true });

  for (;;) {
    try {
      const descriptor = openSync(lockPath, "wx");
      writeFileSync(
        descriptor,
        `${JSON.stringify({ hostName, processId, startedAt: new Date().toISOString() })}\n`
      );
      return descriptor;
    } catch (error) {
      if (error?.code !== "EEXIST") {
        throw error;
      }

      if (isStaleLock(lockPath, hostName)) {
        await reclaimStaleLock({
          hostName,
          lockPath,
          pollIntervalMilliseconds,
          processId
        });
        continue;
      }

      await sleep(pollIntervalMilliseconds);
    }
  }
}

async function reclaimStaleLock({
  hostName,
  lockPath,
  pollIntervalMilliseconds,
  processId
}) {
  const reclaimPath = `${lockPath}.reclaim`;
  let descriptor;

  try {
    descriptor = openSync(reclaimPath, "wx");
    writeFileSync(
      descriptor,
      `${JSON.stringify({ hostName, processId, startedAt: new Date().toISOString() })}\n`
    );
  } catch (error) {
    if (error?.code !== "EEXIST") {
      throw error;
    }

    if (isStaleLock(reclaimPath, hostName)) {
      rmSync(reclaimPath, { force: true });
    }
    await sleep(pollIntervalMilliseconds);
    return;
  }

  try {
    if (isStaleLock(lockPath, hostName)) {
      rmSync(lockPath, { force: true });
    }
  } finally {
    closeSync(descriptor);
    rmSync(reclaimPath, { force: true });
  }
}

function isStaleLock(lockPath, currentHostName) {
  try {
    const metadata = JSON.parse(readFileSync(lockPath, "utf8"));
    return (
      metadata.hostName === currentHostName &&
      Number.isInteger(metadata.processId) &&
      !isProcessRunning(metadata.processId)
    );
  } catch {
    return false;
  }
}

function isProcessRunning(processId) {
  if (processId <= 0) {
    return false;
  }

  try {
    process.kill(processId, 0);
    return true;
  } catch (error) {
    return error?.code !== "ESRCH";
  }
}

function sleep(milliseconds) {
  return new Promise((resolvePromise) => {
    setTimeout(resolvePromise, milliseconds);
  });
}

function isMainModule() {
  return process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1];
}
