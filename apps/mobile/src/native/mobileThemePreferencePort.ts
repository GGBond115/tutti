import { Appearance } from "react-native";
import { mobilePreferences } from "./mobileNative";
import type {
  MobileThemePreference,
  MobileThemePreferencePort
} from "../services/mobileThemePreferenceService";

export function createMobileThemePreferencePort(): MobileThemePreferencePort {
  return {
    applyNativeColorScheme(preference) {
      Appearance.setColorScheme(
        preference === "system" ? "unspecified" : preference
      );
    },
    loadThemePreference: () => mobilePreferences.loadThemePreference(),
    saveThemePreference: (preference: MobileThemePreference) =>
      mobilePreferences.saveThemePreference(preference)
  };
}
