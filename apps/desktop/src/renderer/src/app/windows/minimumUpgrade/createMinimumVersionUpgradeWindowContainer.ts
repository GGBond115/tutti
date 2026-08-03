import type { DesktopMinimumVersionApi } from "@tutti-os/desktop-update-admission/contracts";

export interface MinimumVersionUpgradeWindowContainer {
  port: DesktopMinimumVersionApi;
}

export function createMinimumVersionUpgradeWindowContainer(): MinimumVersionUpgradeWindowContainer {
  if (!window.tuttiMinimumVersion) {
    throw new Error("minimum-version preload bridge is unavailable");
  }
  return {
    port: window.tuttiMinimumVersion
  };
}
