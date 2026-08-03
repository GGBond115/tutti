import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import type {
  DesktopUpdateAdmissionBackend,
  DesktopUpdateAdmissionSnapshot,
  MinimumVersionAppUpdateService
} from "../contracts/index.ts";
import { createDesktopUpdateAdmissionController } from "./createDesktopUpdateAdmissionController.ts";

function allowedSnapshot(): DesktopUpdateAdmissionSnapshot<"tutti-desktop"> {
  const platform =
    process.platform === "darwin"
      ? "macos"
      : process.platform === "win32"
        ? "windows"
        : "linux";
  const architecture = process.arch === "arm64" ? "arm64" : "x64";
  return {
    featureAvailability: {
      fetchedAt: "2026-08-02T09:00:00Z",
      keys: ["workspace.example"],
      policyRevision: "v1",
      source: "cache"
    },
    identity: {
      architecture,
      currentVersion: "1.0.0",
      platform,
      product: "tutti-desktop"
    },
    lastAttemptAt: "2026-08-02T09:00:00Z",
    nextForegroundCheckAt: "2026-08-02T09:30:00Z",
    policy: {
      response: {
        channel: "stable",
        decision: "allowed",
        minimumVersion: "1.0.0",
        policyRevision: "v1",
        reason: "meetsMinimum"
      },
      status: "resolved"
    }
  };
}

function updateService(): MinimumVersionAppUpdateService {
  return {
    async acquireMandatorySession() {
      throw new Error("not used");
    },
    getState() {
      return {
        channel: "stable",
        checkedAt: null,
        currentVersion: "1.0.0",
        downloadedBytes: null,
        downloadPercent: null,
        latestVersion: null,
        message: null,
        policy: "prompt",
        releaseDate: null,
        releaseName: null,
        releaseNotesUrl: null,
        status: "idle",
        totalBytes: null
      };
    },
    subscribe() {
      return () => undefined;
    }
  };
}

test("controller consumes daemon startup and foreground snapshots without owning timing", async () => {
  const app = new EventEmitter();
  const snapshot = allowedSnapshot();
  const calls: string[] = [];
  const backend: DesktopUpdateAdmissionBackend<"tutti-desktop"> = {
    async getStartupSnapshot() {
      calls.push("startup");
      return snapshot;
    },
    async refresh(trigger) {
      calls.push(trigger);
      return {
        performed: false,
        skipReason: "throttled",
        snapshot
      };
    }
  };
  const featureSnapshots: DesktopUpdateAdmissionSnapshot<"tutti-desktop">[] =
    [];
  const registeredHandlers = new Map<string, (...args: unknown[]) => unknown>();
  const controller = createDesktopUpdateAdmissionController({
    backend,
    electron: {
      app: app as never,
      BrowserWindow: class {
        constructor() {
          throw new Error("allowed policy must not open an upgrade window");
        }
      } as never,
      ipcMain: {
        handle(channel: string, handler: (...args: unknown[]) => unknown) {
          registeredHandlers.set(channel, handler);
        },
        removeHandler(channel: string) {
          registeredHandlers.delete(channel);
        }
      } as never,
      shell: { openExternal: async () => undefined } as never
    },
    featureAvailability: {
      acceptDaemonSnapshot(value) {
        featureSnapshots.push(value);
      }
    },
    listBusinessWindows: () => [],
    logger: { error() {}, info() {} },
    manualDownloadUrl: () => "https://tutti.sh/desktop/download",
    onPolicyReleased() {},
    preloadPath: "/preload.cjs",
    product: "tutti-desktop",
    rendererFilePath: "/minimum-version.html",
    runtime: {
      checksEnabled: true,
      currentVersion: "1.0.0",
      development: true
    },
    updateService: updateService()
  });

  assert.equal(await controller.runStartupCheck(), false);
  await controller.checkAfterForegroundRestore();
  assert.deepEqual(calls, ["startup", "foreground"]);
  assert.equal(featureSnapshots.length, 2);
  controller.dispose();
  assert.equal(registeredHandlers.size, 0);
});
