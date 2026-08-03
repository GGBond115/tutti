import { AsyncLocalStorage } from "node:async_hooks";
import type {
  ConfigureDesktopUpdatesInput,
  DesktopUpdateState,
  MandatoryDesktopUpdateSession,
  MandatoryDesktopUpdateTarget
} from "../contracts/index.ts";
import { updaterTargetMeetsMinimum } from "../core/index.ts";

export class MandatoryUpdateTargetError extends Error {
  public readonly latestVersion: string | null;
  public readonly minimumVersion: string;

  public constructor(latestVersion: string | null, minimumVersion: string) {
    super(
      `mandatory update target ${latestVersion ?? "unknown"} is below ${minimumVersion}`
    );
    this.name = "MandatoryUpdateTargetError";
    this.latestVersion = latestVersion;
    this.minimumVersion = minimumVersion;
  }
}

export interface MandatoryUpdaterLeaseManager {
  assertAccess(): void;
  isMandatoryAccess(): boolean;
  acquire(
    input: MandatoryDesktopUpdateTarget
  ): Promise<MandatoryDesktopUpdateSession>;
}

export function createMandatoryUpdaterLeaseManager<
  TNormalConfiguration
>(options: {
  captureNormalConfiguration(): TNormalConfiguration | null;
  suspendNormalUpdates(): Promise<void>;
  prepareMandatoryUpdate(
    input: ConfigureDesktopUpdatesInput
  ): Promise<DesktopUpdateState>;
  downloadUpdate(): Promise<DesktopUpdateState>;
  installUpdate(): Promise<void>;
  getState(): DesktopUpdateState;
  restoreNormalConfiguration(
    configuration: TNormalConfiguration
  ): Promise<void>;
}): MandatoryUpdaterLeaseManager {
  const mandatoryContext = new AsyncLocalStorage<symbol>();
  const normalRestoreContext = new AsyncLocalStorage<symbol>();
  let mandatoryOwner: symbol | null = null;

  const assertAccess = (): void => {
    if (mandatoryOwner && mandatoryContext.getStore() !== mandatoryOwner) {
      throw new Error(
        "application updater is owned by the mandatory update session"
      );
    }
  };

  return {
    assertAccess,
    isMandatoryAccess: () =>
      mandatoryOwner !== null &&
      mandatoryContext.getStore() === mandatoryOwner &&
      normalRestoreContext.getStore() !== mandatoryOwner,
    async acquire(input) {
      if (mandatoryOwner) {
        throw new Error("a mandatory update session is already active");
      }
      const owner = Symbol(
        `mandatory-update:${input.policyRevision}:${input.minimumVersion}`
      );
      mandatoryOwner = owner;
      const configurationToRestore = options.captureNormalConfiguration();
      try {
        await options.suspendNormalUpdates();
      } catch (error) {
        if (mandatoryOwner === owner) {
          mandatoryOwner = null;
        }
        throw error;
      }

      let target = input;
      let released = false;
      let releasePromise: Promise<void> | null = null;
      const run = <T>(operation: () => Promise<T>): Promise<T> => {
        if (released || mandatoryOwner !== owner) {
          return Promise.reject(
            new Error("mandatory update session is no longer active")
          );
        }
        return mandatoryContext.run(owner, operation);
      };
      const assertTargetVersion = (update: DesktopUpdateState): void => {
        if (
          !updaterTargetMeetsMinimum(
            update.latestVersion,
            target.minimumVersion
          )
        ) {
          throw new MandatoryUpdateTargetError(
            update.latestVersion,
            target.minimumVersion
          );
        }
      };
      const assertTargetWhenPresent = (update: DesktopUpdateState): void => {
        if (update.status === "available" || update.status === "downloaded") {
          assertTargetVersion(update);
        }
      };

      return {
        retarget: (nextTarget) => {
          if (released || mandatoryOwner !== owner) {
            throw new Error("mandatory update session is no longer active");
          }
          target = nextTarget;
        },
        prepare: () =>
          run(async () => {
            const update = await options.prepareMandatoryUpdate({
              channel: target.channel,
              policy: "prompt"
            });
            assertTargetWhenPresent(update);
            return update;
          }),
        downloadUpdate: () =>
          run(async () => {
            const update = await options.downloadUpdate();
            if (update.status === "downloaded") {
              assertTargetVersion(update);
            }
            return update;
          }),
        installUpdate: () =>
          run(async () => {
            assertTargetVersion(options.getState());
            await options.installUpdate();
          }),
        release: (releaseOptions) => {
          if (releasePromise) {
            return releasePromise;
          }
          releasePromise = (async () => {
            released = true;
            if (mandatoryOwner !== owner) {
              return;
            }
            try {
              if (
                releaseOptions?.restoreNormal !== false &&
                configurationToRestore
              ) {
                await mandatoryContext.run(owner, () =>
                  normalRestoreContext.run(owner, () =>
                    options.restoreNormalConfiguration(configurationToRestore)
                  )
                );
              }
            } finally {
              if (mandatoryOwner === owner) {
                mandatoryOwner = null;
              }
            }
          })();
          return releasePromise;
        }
      };
    }
  };
}
