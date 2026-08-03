import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type {
  DesktopUpdateAdmissionBackend,
  DesktopUpdateAdmissionRefreshResult,
  DesktopUpdateAdmissionSnapshot
} from "@tutti-os/desktop-update-admission/contracts";

export function createTuttidDesktopUpdateAdmissionBackend(
  client: TuttidClient
): DesktopUpdateAdmissionBackend<"tutti-desktop"> {
  return {
    async getStartupSnapshot(signal) {
      return (await client.getDesktopUpdateAdmissionStartup({
        signal
      })) as DesktopUpdateAdmissionSnapshot<"tutti-desktop">;
    },
    async refresh(trigger, signal) {
      return (await client.refreshDesktopUpdateAdmission(trigger, {
        signal
      })) as DesktopUpdateAdmissionRefreshResult<"tutti-desktop">;
    }
  };
}
