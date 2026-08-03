import {
  MobileThemePreferenceService,
  normalizeMobileThemePreference,
  type MobileThemePreference,
  type MobileThemePreferencePort
} from "./mobileThemePreferenceService";

test("normalizes missing and unsupported stored preferences to system", () => {
  expect(normalizeMobileThemePreference(undefined)).toBe("system");
  expect(normalizeMobileThemePreference("contrast")).toBe("system");
  expect(normalizeMobileThemePreference("light")).toBe("light");
  expect(normalizeMobileThemePreference("dark")).toBe("dark");
});

test("loads and immediately applies the stored device preference", () => {
  const applied: MobileThemePreference[] = [];
  const service = new MobileThemePreferenceService(
    createPort({
      applyNativeColorScheme: (preference) => applied.push(preference),
      loadThemePreference: () => "dark"
    })
  );

  expect(service.getSnapshot()).toEqual({ preference: "dark" });
  expect(applied).toEqual(["dark"]);
});

test("publishes a selection immediately and persists it", async () => {
  const applied: MobileThemePreference[] = [];
  const saved: MobileThemePreference[] = [];
  const service = new MobileThemePreferenceService(
    createPort({
      applyNativeColorScheme: (preference) => applied.push(preference),
      saveThemePreference: async (preference) => {
        saved.push(preference);
      }
    })
  );
  const snapshots: MobileThemePreference[] = [];
  service.subscribe(() => snapshots.push(service.getSnapshot().preference));

  const save = service.setPreference("light");

  expect(service.getSnapshot()).toEqual({ preference: "light" });
  await save;
  expect(applied).toEqual(["system", "light"]);
  expect(saved).toEqual(["light"]);
  expect(snapshots).toEqual(["light"]);
});

test("rolls back the visible and native theme when persistence fails", async () => {
  const applied: MobileThemePreference[] = [];
  const service = new MobileThemePreferenceService(
    createPort({
      applyNativeColorScheme: (preference) => applied.push(preference),
      saveThemePreference: async () => {
        throw new Error("storage unavailable");
      }
    })
  );
  const snapshots: MobileThemePreference[] = [];
  service.subscribe(() => snapshots.push(service.getSnapshot().preference));

  await expect(service.setPreference("dark")).rejects.toThrow(
    "storage unavailable"
  );

  expect(service.getSnapshot()).toEqual({ preference: "system" });
  expect(applied).toEqual(["system", "dark", "system"]);
  expect(snapshots).toEqual(["dark", "system"]);
});

function createPort(
  overrides: Partial<MobileThemePreferencePort> = {}
): MobileThemePreferencePort {
  return {
    applyNativeColorScheme: () => undefined,
    loadThemePreference: () => "system",
    saveThemePreference: async () => undefined,
    ...overrides
  };
}
