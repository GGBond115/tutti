export const desktopUpdateAdmissionDevelopmentEnvironment = {
  currentVersion: "DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION",
  development: "DESKTOP_UPDATE_ADMISSION_DEV",
  download: "DESKTOP_UPDATE_ADMISSION_DOWNLOAD",
  foregroundIntervalMs: "DESKTOP_UPDATE_ADMISSION_FOREGROUND_INTERVAL_MS",
  featureKeys: "DESKTOP_UPDATE_ADMISSION_FEATURE_KEYS",
  install: "DESKTOP_UPDATE_ADMISSION_INSTALL",
  latestVersion: "DESKTOP_UPDATE_ADMISSION_LATEST_VERSION",
  minimumVersion: "DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION",
  mockServerUrl: "DESKTOP_UPDATE_ADMISSION_MOCK_SERVER_URL",
  policy: "DESKTOP_UPDATE_ADMISSION_POLICY",
  policySequence: "DESKTOP_UPDATE_ADMISSION_POLICY_SEQUENCE",
  scenario: "DESKTOP_UPDATE_ADMISSION_SCENARIO",
  transport: "DESKTOP_UPDATE_ADMISSION_TRANSPORT",
  updater: "DESKTOP_UPDATE_ADMISSION_UPDATER"
} as const;

export function invalidDevelopmentScenario(message: string): never {
  throw new Error(`invalid desktop update development scenario: ${message}`);
}

export function readRequiredDevelopmentEnvironment(
  env: Readonly<Record<string, string | undefined>>,
  name: string
): string {
  const value = env[name]?.trim();
  if (!value) {
    return invalidDevelopmentScenario(`${name} is required`);
  }
  return value;
}

export function readDevelopmentEnabled(
  env: Readonly<Record<string, string | undefined>>
): boolean {
  const name = desktopUpdateAdmissionDevelopmentEnvironment.development;
  const value = env[name]?.trim().toLowerCase();
  if (!value || ["0", "false", "no", "off"].includes(value)) {
    return false;
  }
  if (["1", "true", "yes", "on"].includes(value)) {
    return true;
  }
  return invalidDevelopmentScenario(`${name} must be a boolean flag`);
}
