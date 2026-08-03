import Foundation
import React

@objc(TuttiMobilePreferences)
final class MobilePreferencesModule: NSObject {
  private let defaults = UserDefaults.standard

  @objc
  static func requiresMainQueueSetup() -> Bool {
    false
  }

  @objc
  func loadThemePreference() -> String {
    Self.normalizeThemePreference(
      defaults.string(forKey: Self.themePreferenceKey)
    )
  }

  @objc(saveThemePreference:resolver:rejecter:)
  func saveThemePreference(
    _ preference: String,
    resolver resolve: RCTPromiseResolveBlock,
    rejecter reject: RCTPromiseRejectBlock
  ) {
    guard Self.supportedThemePreferences.contains(preference) else {
      reject(
        "INVALID_THEME_PREFERENCE",
        "Unsupported Mobile theme preference",
        nil
      )
      return
    }
    defaults.set(preference, forKey: Self.themePreferenceKey)
    guard defaults.string(forKey: Self.themePreferenceKey) == preference else {
      reject(
        "THEME_PREFERENCE_WRITE_FAILED",
        "Unable to save Mobile theme preference",
        nil
      )
      return
    }
    resolve(nil)
  }

  private static let supportedThemePreferences: Set<String> = [
    "system", "light", "dark",
  ]
  private static let themePreferenceKey = "themePreference"

  private static func normalizeThemePreference(_ value: String?) -> String {
    guard let value, supportedThemePreferences.contains(value) else {
      return "system"
    }
    return value
  }
}
