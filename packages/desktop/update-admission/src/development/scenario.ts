import type { DesktopUpdateAdmissionRuntime } from "../contracts/index.ts";
import {
  desktopUpdateAdmissionDevelopmentEnvironment,
  invalidDevelopmentScenario,
  readDevelopmentEnabled,
  readRequiredDevelopmentEnvironment
} from "./environment.ts";
import {
  compareDevelopmentManagedVersions,
  parseDevelopmentManagedVersion,
  validateStrictDevelopmentSemVer
} from "./version.ts";

export type DesktopUpdateDevelopmentUpdaterScenario =
  | {
      check: "available" | "downloaded" | "targetBelowMinimum";
      latestVersion: string;
      download: "success" | "error";
      install: "simulated" | "error";
    }
  | {
      check: "unavailable" | "error";
    };

interface DesktopUpdateDevelopmentScenarioBase {
  currentVersion: string;
  updater: DesktopUpdateDevelopmentUpdaterScenario;
}

export type DesktopUpdateDevelopmentScenario =
  | (DesktopUpdateDevelopmentScenarioBase & {
      mockServerUrl: null;
      transport: "in-process";
    })
  | (DesktopUpdateDevelopmentScenarioBase & {
      mockServerUrl: string;
      transport: "loopback";
    });

export interface DesktopUpdateDevelopmentResolution {
  runtime: DesktopUpdateAdmissionRuntime;
  scenario: DesktopUpdateDevelopmentScenario | null;
}

function resolveUpdater(
  env: Readonly<Record<string, string | undefined>>,
  options: {
    defaultCheck: "available" | "unavailable";
    preset: string | undefined;
  }
): DesktopUpdateDevelopmentUpdaterScenario {
  const names = desktopUpdateAdmissionDevelopmentEnvironment;
  if (options.preset) {
    for (const conflictingName of [
      names.updater,
      names.download,
      names.install
    ]) {
      if (env[conflictingName]?.trim()) {
        return invalidDevelopmentScenario(
          `${names.scenario} and ${conflictingName} are mutually exclusive`
        );
      }
    }
  }
  const configured = env[names.updater]?.trim();
  const check =
    options.preset === "startup-updater-unavailable"
      ? "unavailable"
      : options.preset === "startup-target-below-minimum"
        ? "targetBelowMinimum"
        : options.preset === "retry-policy-released"
          ? "error"
          : configured || options.defaultCheck;
  if (check === "unavailable" || check === "error") {
    return { check };
  }
  if (
    check !== "available" &&
    check !== "downloaded" &&
    check !== "targetBelowMinimum"
  ) {
    return invalidDevelopmentScenario(
      `unknown updater outcome ${JSON.stringify(check)}`
    );
  }
  const download =
    options.preset === "startup-download-error"
      ? "error"
      : env[names.download]?.trim() || "success";
  if (download !== "success" && download !== "error") {
    return invalidDevelopmentScenario(
      `${names.download} must be success or error`
    );
  }
  const install = env[names.install]?.trim() || "simulated";
  if (install !== "simulated" && install !== "error") {
    return invalidDevelopmentScenario(
      `${names.install} must be simulated or error`
    );
  }
  return {
    check,
    download,
    install,
    latestVersion: validateStrictDevelopmentSemVer(
      readRequiredDevelopmentEnvironment(env, names.latestVersion),
      names.latestVersion
    )
  };
}

function validateLoopbackUrl(raw: string): string {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return invalidDevelopmentScenario("mock server URL must be a valid URL");
  }
  if (
    parsed.protocol !== "http:" ||
    parsed.hostname !== "127.0.0.1" ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash ||
    (parsed.pathname !== "" && parsed.pathname !== "/")
  ) {
    return invalidDevelopmentScenario(
      "mock server URL must be an http://127.0.0.1 origin"
    );
  }
  return parsed.origin;
}

function rejectLoopbackPolicyEnvironment(
  env: Readonly<Record<string, string | undefined>>
): void {
  const names = desktopUpdateAdmissionDevelopmentEnvironment;
  for (const name of [
    names.featureKeys,
    names.minimumVersion,
    names.policy,
    names.policySequence,
    names.scenario
  ]) {
    if (env[name]?.trim()) {
      invalidDevelopmentScenario(
        `${name} belongs to the loopback mock server, not the client`
      );
    }
  }
}

function validateUpdater(
  updater: DesktopUpdateDevelopmentUpdaterScenario,
  currentVersion: string
): void {
  if (!("latestVersion" in updater)) {
    return;
  }
  const current = parseDevelopmentManagedVersion(currentVersion);
  if (!current) {
    invalidDevelopmentScenario(
      "an available updater requires a managed currentVersion"
    );
  }
  const latest = parseDevelopmentManagedVersion(updater.latestVersion);
  if (!latest || latest.channel !== current.channel) {
    invalidDevelopmentScenario(
      `latestVersion must use the ${current.channel} channel`
    );
  }
  if (compareDevelopmentManagedVersions(latest, current) <= 0) {
    invalidDevelopmentScenario(
      "an available updater requires latestVersion above currentVersion"
    );
  }
}

export function resolveDesktopUpdateDevelopmentScenario(input: {
  env: Readonly<Record<string, string | undefined>>;
  isPackaged: boolean;
}): DesktopUpdateDevelopmentScenario | null {
  if (input.isPackaged || !readDevelopmentEnabled(input.env)) {
    return null;
  }
  const names = desktopUpdateAdmissionDevelopmentEnvironment;
  const currentVersion = validateStrictDevelopmentSemVer(
    readRequiredDevelopmentEnvironment(input.env, names.currentVersion),
    names.currentVersion
  );
  const transport = input.env[names.transport]?.trim() || "in-process";
  if (transport !== "in-process" && transport !== "loopback") {
    return invalidDevelopmentScenario(
      `${names.transport} must be in-process or loopback`
    );
  }
  if (transport === "loopback") {
    rejectLoopbackPolicyEnvironment(input.env);
  }
  const preset =
    transport === "in-process" ? input.env[names.scenario]?.trim() : undefined;
  const updater = resolveUpdater(input.env, {
    defaultCheck: transport === "in-process" ? "available" : "unavailable",
    preset
  });
  const scenario: DesktopUpdateDevelopmentScenario =
    transport === "in-process"
      ? {
          currentVersion,
          mockServerUrl: null,
          transport,
          updater
        }
      : {
          currentVersion,
          mockServerUrl: validateLoopbackUrl(
            readRequiredDevelopmentEnvironment(input.env, names.mockServerUrl)
          ),
          transport,
          updater
        };
  validateUpdater(scenario.updater, currentVersion);
  if (scenario.transport === "in-process") {
    return Object.freeze({
      ...scenario,
      updater: Object.freeze({ ...scenario.updater })
    });
  }
  return Object.freeze({
    ...scenario,
    updater: Object.freeze({ ...scenario.updater })
  });
}

export function resolveDesktopUpdateAdmissionDevelopment(input: {
  applicationVersion: string;
  env: Readonly<Record<string, string | undefined>>;
  isPackaged: boolean;
}): DesktopUpdateDevelopmentResolution {
  const applicationVersion = input.applicationVersion.trim();
  if (!applicationVersion) {
    throw new Error("desktop application version is required");
  }
  const scenario = resolveDesktopUpdateDevelopmentScenario(input);
  if (input.isPackaged) {
    return {
      runtime: {
        checksEnabled: true,
        currentVersion: applicationVersion,
        development: false
      },
      scenario: null
    };
  }
  if (!scenario) {
    return {
      runtime: {
        checksEnabled: false,
        currentVersion: applicationVersion,
        development: false
      },
      scenario: null
    };
  }
  return {
    runtime: {
      checksEnabled: true,
      currentVersion: scenario.currentVersion,
      development: true
    },
    scenario
  };
}
