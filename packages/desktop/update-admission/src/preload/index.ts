import {
  desktopFeatureAvailabilityIpcChannels,
  desktopUpdateAdmissionIpcChannels,
  type DesktopFeatureAvailabilityApi,
  type DesktopFeatureAvailabilitySnapshot,
  type DesktopMinimumVersionApi,
  type MinimumVersionUpgradeState
} from "../contracts/index.ts";

export function createDesktopFeatureAvailabilityApi(input: {
  invoke<T>(channel: string, ...args: unknown[]): Promise<T>;
  on(
    channel: string,
    listener: (
      event: unknown,
      snapshot: DesktopFeatureAvailabilitySnapshot
    ) => void
  ): void;
  removeListener(
    channel: string,
    listener: (
      event: unknown,
      snapshot: DesktopFeatureAvailabilitySnapshot
    ) => void
  ): void;
}): DesktopFeatureAvailabilityApi {
  return {
    getSnapshot: () =>
      input.invoke<DesktopFeatureAvailabilitySnapshot>(
        desktopFeatureAvailabilityIpcChannels.getSnapshot
      ),
    isSupported: (key) =>
      input.invoke<boolean>(
        desktopFeatureAvailabilityIpcChannels.isSupported,
        key
      ),
    onChanged(listener) {
      const handler = (
        _event: unknown,
        snapshot: DesktopFeatureAvailabilitySnapshot
      ): void => listener(snapshot);
      input.on(desktopFeatureAvailabilityIpcChannels.changed, handler);
      return () =>
        input.removeListener(
          desktopFeatureAvailabilityIpcChannels.changed,
          handler
        );
    }
  };
}

export function createDesktopMinimumVersionApi(input: {
  invoke<T>(channel: string): Promise<T>;
  on(
    channel: string,
    listener: (event: unknown, state: MinimumVersionUpgradeState) => void
  ): void;
  removeListener(
    channel: string,
    listener: (event: unknown, state: MinimumVersionUpgradeState) => void
  ): void;
}): DesktopMinimumVersionApi {
  return {
    getState: () =>
      input.invoke<MinimumVersionUpgradeState | null>(
        desktopUpdateAdmissionIpcChannels.getState
      ),
    start: () =>
      input.invoke<MinimumVersionUpgradeState | null>(
        desktopUpdateAdmissionIpcChannels.start
      ),
    retry: () =>
      input.invoke<MinimumVersionUpgradeState | null>(
        desktopUpdateAdmissionIpcChannels.retry
      ),
    later: () => input.invoke<void>(desktopUpdateAdmissionIpcChannels.later),
    openManualDownload: () =>
      input.invoke<void>(desktopUpdateAdmissionIpcChannels.manualDownload),
    exit: () => input.invoke<void>(desktopUpdateAdmissionIpcChannels.exit),
    onState(listener) {
      const handler = (
        _event: unknown,
        state: MinimumVersionUpgradeState
      ): void => listener(state);
      input.on(desktopUpdateAdmissionIpcChannels.state, handler);
      return () =>
        input.removeListener(desktopUpdateAdmissionIpcChannels.state, handler);
    }
  };
}
