import { app, BrowserWindow, ipcMain, shell } from "electron";
import type {
  MinimumVersionCheckResponse,
  MinimumVersionUpgradeState
} from "../../shared/contracts/ipc.ts";
import { desktopIpcChannels } from "../../shared/contracts/ipc.ts";
import type { DesktopLogger } from "../logging.ts";
import { outboundFetch } from "../net/outboundFetch.ts";
import type { AppUpdateService } from "./appUpdateService.ts";
import {
  releaseMeetsMinimum,
  resolveMinimumVersionRuntimeTarget,
  shouldCheckMinimumVersionAfterForeground
} from "./minimumVersionPolicy.ts";

const startupTimeoutMs = 3_000;
const productionControlPlaneBaseUrl = "https://tutti.sh/api/desktop/v1";
const officialDesktopDownloadUrl = "https://tutti.sh/desktop/download";

interface ControllerOptions {
  logger: DesktopLogger;
  preloadPath: string;
  rendererFilePath: string;
  rendererUrl?: string;
  updateService: AppUpdateService;
  openBusinessWindow(): Promise<void>;
  normalUpdatePreferences(): {
    channel: "stable" | "rc";
    policy: "off" | "prompt" | "auto";
  };
  now?: () => number;
}

export interface MinimumVersionUpgradeController {
  runStartupCheck(): Promise<boolean>;
  configureNormalUpdates(): void;
  checkAfterForegroundRestore(): Promise<void>;
  dispose(): void;
}

function logCheck(
  logger: DesktopLogger,
  level: "info" | "error",
  details: Record<string, unknown>
): void {
  logger[level](`[minimum-version-check] ${JSON.stringify(details)}`);
}

function requestPayload() {
  const target = resolveMinimumVersionRuntimeTarget(
    process.platform,
    process.arch
  );
  if (!target) {
    return null;
  }
  return {
    product: "tutti-desktop",
    ...target,
    currentVersion: app.getVersion()
  };
}

async function checkMinimumVersion(
  payload: NonNullable<ReturnType<typeof requestPayload>>,
  signal?: AbortSignal
): Promise<MinimumVersionCheckResponse> {
  const baseUrl = (
    process.env.TUTTI_DESKTOP_CONTROL_PLANE_BASE_URL ??
    productionControlPlaneBaseUrl
  ).replace(/\/+$/u, "");
  const response = await outboundFetch(
    `${baseUrl}/public/desktop-version/check`,
    {
      body: JSON.stringify(payload),
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        "User-Agent": "Tutti Desktop"
      },
      method: "POST",
      signal
    }
  );
  if (!response.ok) {
    throw new Error(`minimum version check returned HTTP ${response.status}`);
  }
  return (await response.json()) as MinimumVersionCheckResponse;
}

export function createMinimumVersionUpgradeController(
  options: ControllerOptions
): MinimumVersionUpgradeController {
  const now = options.now ?? Date.now;
  let lastCheckAt = 0;
  let foregroundPrompted = false;
  let state: MinimumVersionUpgradeState | null = null;
  let window: BrowserWindow | null = null;
  let mode: "startup" | "foreground" = "startup";
  let forcedFlowStarted = false;
  let installRequested = false;
  let disposed = false;
  let activeCheck: Promise<MinimumVersionCheckResponse | null> | null = null;
  let appQuitStarted = false;

  const handleBeforeQuit = () => {
    appQuitStarted = true;
  };
  app.on("before-quit", handleBeforeQuit);

  const emitState = () => {
    if (state && window && !window.isDestroyed()) {
      window.webContents.send(desktopIpcChannels.update.minimumState, state);
    }
  };
  const applyState = (
    phase: MinimumVersionUpgradeState["phase"],
    update = options.updateService.getState(),
    message: string | null = null
  ) => {
    if (state) {
      state = { ...state, phase, update, message };
      emitState();
    }
  };
  const closeWindow = () => {
    if (window && !window.isDestroyed()) {
      window.destroy();
    }
    window = null;
  };
  const openWindow = (nextMode: "startup" | "foreground") => {
    mode = nextMode;
    if (window && !window.isDestroyed()) {
      window.show();
      window.focus();
      return;
    }
    window = new BrowserWindow({
      width: 520,
      height: 420,
      minWidth: 480,
      minHeight: 380,
      resizable: false,
      maximizable: false,
      fullscreenable: false,
      autoHideMenuBar: true,
      show: false,
      webPreferences: {
        preload: options.preloadPath,
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true
      }
    });
    window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    window.webContents.on("will-navigate", (event) => event.preventDefault());
    window.on("close", (event) => {
      if (!appQuitStarted && (mode === "startup" || forcedFlowStarted)) {
        event.preventDefault();
        app.quit();
      }
    });
    window.once("ready-to-show", () => window?.show());
    const search = `view=minimum-upgrade&mode=${nextMode}`;
    if (options.rendererUrl) {
      void window.loadURL(`${options.rendererUrl}/?${search}`);
    } else {
      void window.loadFile(options.rendererFilePath, { search });
    }
  };

  const runPolicyCheck = async (
    bounded: boolean
  ): Promise<MinimumVersionCheckResponse | null> => {
    const payload = requestPayload();
    if (!payload) {
      lastCheckAt = now();
      logCheck(options.logger, "info", {
        stage: bounded ? "startup" : "foreground",
        result: "success",
        decision: "notApplicable",
        reason: "unsupportedRuntime",
        platform: process.platform,
        architecture: process.arch
      });
      return null;
    }
    const startedAt = now();
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | null = null;
    try {
      const result = await (bounded
        ? Promise.race([
            checkMinimumVersion(payload, controller.signal),
            new Promise<never>((_resolve, reject) => {
              timer = setTimeout(() => {
                controller.abort();
                reject(new Error("minimum version startup check timed out"));
              }, startupTimeoutMs);
            })
          ])
        : checkMinimumVersion(payload));
      lastCheckAt = now();
      logCheck(options.logger, "info", {
        stage: bounded ? "startup" : "foreground",
        result: "success",
        decision: result.decision,
        reason: result.reason,
        currentVersion: result.currentVersion,
        minimumVersion: result.minimumVersion,
        policyRevision: result.policyRevision,
        elapsedMs: now() - startedAt
      });
      return result;
    } catch (error) {
      lastCheckAt = now();
      logCheck(options.logger, "error", {
        stage: bounded ? "startup" : "foreground",
        result: "failure",
        error: error instanceof Error ? error.message : String(error),
        elapsedMs: now() - startedAt
      });
      return null;
    } finally {
      if (timer) {
        clearTimeout(timer);
      }
    }
  };
  const checkPolicy = async (
    bounded: boolean
  ): Promise<MinimumVersionCheckResponse | null> => {
    if (activeCheck) {
      return activeCheck;
    }
    const pendingCheck = runPolicyCheck(bounded);
    activeCheck = pendingCheck;
    try {
      return await pendingCheck;
    } finally {
      if (activeCheck === pendingCheck) {
        activeCheck = null;
      }
    }
  };
  const configureNormalUpdates = async (): Promise<void> => {
    try {
      await options.updateService.configure(options.normalUpdatePreferences());
    } catch (error) {
      logCheck(options.logger, "error", {
        stage: "normal-update-configure",
        result: "failure",
        error: error instanceof Error ? error.message : String(error)
      });
    }
  };

  const releaseBlock = () => {
    forcedFlowStarted = false;
    installRequested = false;
    closeWindow();
    void configureNormalUpdates();
    void options.openBusinessWindow().catch((error) => {
      logCheck(options.logger, "error", {
        stage: "business-window-open",
        result: "failure",
        error: error instanceof Error ? error.message : String(error)
      });
    });
  };

  const prepareUpdate = async () => {
    if (!state) {
      return;
    }
    try {
      forcedFlowStarted = true;
      applyState("checking");
      await options.updateService.configure({
        channel: state.check.channel === "rc" ? "rc" : "stable",
        policy: "prompt"
      });
      const update = await options.updateService.checkForUpdates();
      if (
        (update.status !== "available" && update.status !== "downloaded") ||
        !releaseMeetsMinimum(update.latestVersion, state.check.minimumVersion)
      ) {
        applyState("error", update, "releaseBelowMinimum");
        return;
      }
      if (update.status === "downloaded") {
        applyState("downloaded", update);
        if (!installRequested) {
          installRequested = true;
          await options.updateService.installUpdate();
        }
        return;
      }
      applyState("ready", update);
      await options.updateService.downloadUpdate();
    } catch (error) {
      logCheck(options.logger, "error", {
        stage: "forced-update",
        result: "failure",
        error: error instanceof Error ? error.message : String(error)
      });
      applyState("error", options.updateService.getState(), "updateFailed");
    }
  };

  const unsubscribeUpdate = options.updateService.onStateChanged((update) => {
    if (!state || !forcedFlowStarted) {
      return;
    }
    if (update.status === "downloading") {
      applyState("downloading", update);
    } else if (update.status === "downloaded") {
      applyState("downloaded", update);
      if (!installRequested) {
        installRequested = true;
        void options.updateService.installUpdate().catch((error) => {
          installRequested = false;
          logCheck(options.logger, "error", {
            stage: "install",
            result: "failure",
            error: error instanceof Error ? error.message : String(error)
          });
          applyState(
            "error",
            options.updateService.getState(),
            "installFailed"
          );
        });
      }
    } else if (update.status === "error") {
      applyState("error", update, "updateFailed");
    }
  });

  ipcMain.handle(desktopIpcChannels.update.minimumGetState, () => state);
  ipcMain.handle(desktopIpcChannels.update.minimumStart, async () => {
    if (mode === "foreground") {
      for (const candidate of BrowserWindow.getAllWindows()) {
        if (candidate !== window && !candidate.isDestroyed()) {
          candidate.hide();
        }
      }
    }
    await prepareUpdate();
    return state;
  });
  ipcMain.handle(desktopIpcChannels.update.minimumRetry, async () => {
    const response = await checkPolicy(false);
    if (!response) {
      applyState(
        "error",
        options.updateService.getState(),
        "policyCheckFailed"
      );
    } else if (response.decision !== "upgradeRequired") {
      releaseBlock();
    } else if (state) {
      state = { ...state, check: response };
      await prepareUpdate();
    }
    return state;
  });
  ipcMain.handle(desktopIpcChannels.update.minimumLater, () => {
    if (mode === "foreground" && !forcedFlowStarted) {
      closeWindow();
    }
  });
  ipcMain.handle(desktopIpcChannels.update.minimumManualDownload, () =>
    shell.openExternal(
      `${officialDesktopDownloadUrl}?channel=${state?.check.channel === "rc" ? "preview" : "stable"}&platform=macos&arch=universal&format=dmg`
    )
  );
  ipcMain.handle(desktopIpcChannels.update.minimumExit, () => app.quit());

  return {
    async runStartupCheck() {
      if (!app.isPackaged) {
        return false;
      }
      const response = await checkPolicy(true);
      if (!response || response.decision !== "upgradeRequired") {
        return false;
      }
      state = {
        phase: "blocked",
        check: response,
        update: options.updateService.getState(),
        message: null
      };
      openWindow("startup");
      return true;
    },
    configureNormalUpdates() {
      void configureNormalUpdates();
    },
    async checkAfterForegroundRestore() {
      if (
        !shouldCheckMinimumVersionAfterForeground({
          disposed,
          packaged: app.isPackaged,
          foregroundPrompted,
          lastCheckAt,
          now: now()
        })
      ) {
        return;
      }
      const response = await checkPolicy(false);
      if (!response || response.decision !== "upgradeRequired") {
        return;
      }
      foregroundPrompted = true;
      state = {
        phase: "blocked",
        check: response,
        update: options.updateService.getState(),
        message: null
      };
      openWindow("foreground");
    },
    dispose() {
      disposed = true;
      app.removeListener("before-quit", handleBeforeQuit);
      unsubscribeUpdate();
      closeWindow();
      for (const channel of [
        desktopIpcChannels.update.minimumGetState,
        desktopIpcChannels.update.minimumStart,
        desktopIpcChannels.update.minimumRetry,
        desktopIpcChannels.update.minimumLater,
        desktopIpcChannels.update.minimumManualDownload,
        desktopIpcChannels.update.minimumExit
      ]) {
        ipcMain.removeHandler(channel);
      }
    }
  };
}
