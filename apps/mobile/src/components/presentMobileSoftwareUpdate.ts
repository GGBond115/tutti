import { Alert } from "react-native";
import { t } from "../i18n";
import {
  isMobileUpdateInstallPermissionRequired,
  type MobileUpdateService
} from "../services/mobileUpdateService";

export function presentMobileSoftwareUpdate(
  mobileUpdateService: MobileUpdateService
): void {
  void mobileUpdateService
    .checkForUpdates()
    .then((nextSnapshot) => {
      if (nextSnapshot.status === "unsupported") {
        Alert.alert(t("softwareUpdate"), t("updatesUnavailable"));
        return;
      }
      if (nextSnapshot.status === "upToDate") {
        Alert.alert(t("softwareUpdate"), t("upToDate"));
        return;
      }
      if (nextSnapshot.status !== "available" || !nextSnapshot.release) {
        return;
      }

      Alert.alert(
        t("updateAvailable"),
        t("updateAvailableDescription", {
          version: nextSnapshot.release.versionName
        }),
        [
          { style: "cancel", text: t("cancel") },
          {
            onPress: () => {
              void mobileUpdateService.installUpdate().catch((error) => {
                if (!isMobileUpdateInstallPermissionRequired(error)) {
                  Alert.alert(t("updateInstallFailed"));
                }
              });
            },
            text: t("downloadAndInstall")
          }
        ]
      );
    })
    .catch(() => {
      Alert.alert(t("updateCheckFailed"));
    });
}
