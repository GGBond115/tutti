import type { DesktopUpdateApi } from "@preload/types";
import { resolveDesktopEnvironment } from "@renderer/platform/desktop/resolveDesktopEnvironment";

export interface MinimumVersionUpgradeWindowContainer {
  port: DesktopUpdateApi["minimumVersion"];
}

export function createMinimumVersionUpgradeWindowContainer(): MinimumVersionUpgradeWindowContainer {
  const environment = resolveDesktopEnvironment(window.tutti);
  return {
    port: environment.desktopApi.update.minimumVersion
  };
}
