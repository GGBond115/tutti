import type { DesktopUpdateDevelopmentScenario } from "./scenario.ts";

type DriverDisposer = () => void;

export interface DevelopmentUpdateInfo {
  downloadedFile: string;
  files: never[];
  path: string;
  releaseDate: string;
  releaseName: string;
  sha512: string;
  version: string;
}

export interface DevelopmentUpdateProgress {
  bytesPerSecond: number;
  delta: number;
  percent: number;
  total: number;
  transferred: number;
}

export interface DevelopmentAppUpdateDriver {
  checkForUpdates(): Promise<void>;
  configure(...args: unknown[]): void;
  downloadUpdate(): Promise<void>;
  onCheckingForUpdate(listener: () => void): DriverDisposer;
  onDownloadProgress(
    listener: (progress: DevelopmentUpdateProgress) => void
  ): DriverDisposer;
  onError(listener: (error: Error) => void): DriverDisposer;
  onUpdateAvailable(
    listener: (info: DevelopmentUpdateInfo) => void
  ): DriverDisposer;
  onUpdateDownloaded(
    listener: (info: DevelopmentUpdateInfo) => void
  ): DriverDisposer;
  onUpdateNotAvailable(
    listener: (info: DevelopmentUpdateInfo) => void
  ): DriverDisposer;
  quitAndInstall(): void;
  setFeedUrl(_url: string): void;
}

export class DevelopmentInstallSuppressedError extends Error {
  public constructor() {
    super(
      "development update simulation does not install or restart the application"
    );
    this.name = "DevelopmentInstallSuppressedError";
  }
}

export class DevelopmentInstallError extends Error {
  public constructor() {
    super("development update installation failed");
    this.name = "DevelopmentInstallError";
  }
}

export function completeDevelopmentUpdateInstallation(
  scenario: DesktopUpdateDevelopmentScenario
): never {
  if (
    scenario.updater.check === "available" ||
    scenario.updater.check === "downloaded" ||
    scenario.updater.check === "targetBelowMinimum"
  ) {
    if (scenario.updater.install === "error") {
      throw new DevelopmentInstallError();
    }
  }
  throw new DevelopmentInstallSuppressedError();
}

export function createDevelopmentAppUpdateDriver(
  scenario: DesktopUpdateDevelopmentScenario
): DevelopmentAppUpdateDriver {
  const updater = scenario.updater;
  const checkingListeners = new Set<() => void>();
  const progressListeners = new Set<
    (progress: DevelopmentUpdateProgress) => void
  >();
  const errorListeners = new Set<(error: Error) => void>();
  const availableListeners = new Set<(info: DevelopmentUpdateInfo) => void>();
  const downloadedListeners = new Set<(info: DevelopmentUpdateInfo) => void>();
  const notAvailableListeners = new Set<
    (info: DevelopmentUpdateInfo) => void
  >();
  const latestVersion =
    updater.check === "available" ||
    updater.check === "downloaded" ||
    updater.check === "targetBelowMinimum"
      ? updater.latestVersion
      : scenario.currentVersion;
  const updateInfo = (): DevelopmentUpdateInfo => ({
    downloadedFile: "development-update-simulation",
    files: [],
    path: "",
    releaseDate: new Date().toISOString(),
    releaseName: latestVersion,
    sha512: "",
    version: latestVersion
  });
  const emitError = (message: string): Error => {
    const error = new Error(message);
    for (const listener of errorListeners) {
      listener(error);
    }
    return error;
  };

  return {
    async checkForUpdates() {
      for (const listener of checkingListeners) {
        listener();
      }
      if (updater.check === "error") {
        throw emitError("Development update check failed");
      }
      const info = updateInfo();
      if (updater.check === "unavailable") {
        for (const listener of notAvailableListeners) {
          listener(info);
        }
        return;
      }
      for (const listener of availableListeners) {
        listener(info);
      }
      if (updater.check === "downloaded") {
        for (const listener of downloadedListeners) {
          listener(info);
        }
      }
    },
    configure() {},
    async downloadUpdate() {
      if (!("download" in updater)) {
        throw emitError("Development update is unavailable");
      }
      if (updater.download === "error") {
        throw emitError("Development update download failed");
      }
      const info = updateInfo();
      for (const listener of progressListeners) {
        listener({
          bytesPerSecond: 100,
          delta: 100,
          percent: 100,
          total: 100,
          transferred: 100
        });
      }
      for (const listener of downloadedListeners) {
        listener(info);
      }
    },
    onCheckingForUpdate(listener) {
      checkingListeners.add(listener);
      return () => checkingListeners.delete(listener);
    },
    onDownloadProgress(listener) {
      progressListeners.add(listener);
      return () => progressListeners.delete(listener);
    },
    onError(listener) {
      errorListeners.add(listener);
      return () => errorListeners.delete(listener);
    },
    onUpdateAvailable(listener) {
      availableListeners.add(listener);
      return () => availableListeners.delete(listener);
    },
    onUpdateDownloaded(listener) {
      downloadedListeners.add(listener);
      return () => downloadedListeners.delete(listener);
    },
    onUpdateNotAvailable(listener) {
      notAvailableListeners.add(listener);
      return () => notAvailableListeners.delete(listener);
    },
    quitAndInstall() {
      completeDevelopmentUpdateInstallation(scenario);
    },
    setFeedUrl() {}
  };
}
