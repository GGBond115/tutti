import {
  desktopUpdateAdmissionDevelopmentEnvironment,
  invalidDevelopmentScenario,
  readDevelopmentEnabled
} from "./environment.ts";
import {
  compareDevelopmentManagedVersions,
  parseDevelopmentManagedVersion,
  validateStrictDevelopmentSemVer
} from "./version.ts";
import { normalizeDesktopFeatureKeys } from "../feature-availability/core.ts";

export type DesktopUpdateDevelopmentPolicyOutcome =
  | "allowed"
  | "upgradeRequired"
  | "minimumNotConfigured"
  | "unsupported"
  | "unmanagedPrerelease"
  | "error"
  | "timeout";

export type DesktopUpdateDevelopmentPolicyMinimum =
  | {
      kind: "configured";
      version: string;
    }
  | {
      kind: "requestCurrentVersion";
    };

export type DesktopUpdateDevelopmentPolicyStep =
  | {
      outcome: "allowed" | "upgradeRequired";
      minimum: DesktopUpdateDevelopmentPolicyMinimum;
    }
  | {
      outcome: "minimumNotConfigured" | "unsupported" | "unmanagedPrerelease";
    }
  | {
      outcome: "error";
      message: string;
    }
  | {
      outcome: "timeout";
    };

export interface DesktopUpdateDevelopmentPolicyScenario {
  featureKeys: readonly string[];
  policySteps: readonly DesktopUpdateDevelopmentPolicyStep[];
}

function resolveFeatureKeys(
  env: Readonly<Record<string, string | undefined>>
): readonly string[] {
  const raw =
    env[desktopUpdateAdmissionDevelopmentEnvironment.featureKeys]?.trim();
  if (!raw) {
    return Object.freeze([]);
  }
  return normalizeDesktopFeatureKeys(
    raw.split(",").map((value) => value.trim())
  );
}

function parseOutcome(value: string): DesktopUpdateDevelopmentPolicyOutcome {
  switch (value) {
    case "allowed":
    case "upgradeRequired":
    case "minimumNotConfigured":
    case "unsupported":
    case "unmanagedPrerelease":
    case "error":
    case "timeout":
      return value;
    default:
      return invalidDevelopmentScenario(
        `unknown policy outcome ${JSON.stringify(value)}`
      );
  }
}

function configuredMinimum(
  value: string,
  name: string
): DesktopUpdateDevelopmentPolicyMinimum {
  const version = validateStrictDevelopmentSemVer(value, name);
  if (!parseDevelopmentManagedVersion(version)) {
    return invalidDevelopmentScenario(
      `${name} must be a managed stable or RC version`
    );
  }
  return { kind: "configured", version };
}

function createPolicyStep(
  token: string,
  fallbackMinimumVersion: string | undefined
): DesktopUpdateDevelopmentPolicyStep {
  const [rawOutcome, rawMinimumVersion, ...extra] = token.split("@");
  if (!rawOutcome || extra.length > 0) {
    return invalidDevelopmentScenario(
      `invalid policy step ${JSON.stringify(token)}`
    );
  }
  const outcome = parseOutcome(rawOutcome.trim());
  if (outcome === "allowed" || outcome === "upgradeRequired") {
    return {
      minimum: configuredMinimum(
        rawMinimumVersion?.trim() || fallbackMinimumVersion || "",
        desktopUpdateAdmissionDevelopmentEnvironment.minimumVersion
      ),
      outcome
    };
  }
  if (rawMinimumVersion !== undefined) {
    return invalidDevelopmentScenario(
      `policy outcome ${outcome} must not include a minimum version`
    );
  }
  if (outcome === "error") {
    return {
      message: "Development minimum-version policy check failed",
      outcome
    };
  }
  return { outcome };
}

function createPresetPolicySteps(
  name: string,
  minimumVersion: string | undefined
): readonly DesktopUpdateDevelopmentPolicyStep[] {
  const requiredMinimum = (): DesktopUpdateDevelopmentPolicyMinimum =>
    configuredMinimum(
      minimumVersion || "",
      desktopUpdateAdmissionDevelopmentEnvironment.minimumVersion
    );
  switch (name) {
    case "startup-force-success":
    case "startup-updater-unavailable":
    case "startup-target-below-minimum":
    case "startup-download-error":
      return [
        {
          minimum: requiredMinimum(),
          outcome: "upgradeRequired"
        }
      ];
    case "startup-policy-timeout":
      return [{ outcome: "timeout" }];
    case "retry-policy-released":
      return [
        {
          minimum: requiredMinimum(),
          outcome: "upgradeRequired"
        },
        { outcome: "minimumNotConfigured" }
      ];
    case "foreground-upgrade-required":
      return [
        {
          minimum: { kind: "requestCurrentVersion" },
          outcome: "allowed"
        },
        {
          minimum: requiredMinimum(),
          outcome: "upgradeRequired"
        }
      ];
    default:
      return invalidDevelopmentScenario(
        `unknown named scenario ${JSON.stringify(name)}`
      );
  }
}

function resolvePolicySteps(
  env: Readonly<Record<string, string | undefined>>
): readonly DesktopUpdateDevelopmentPolicyStep[] {
  const names = desktopUpdateAdmissionDevelopmentEnvironment;
  const fallbackMinimumVersion = env[names.minimumVersion]?.trim();
  const preset = env[names.scenario]?.trim();
  if (preset) {
    for (const conflictingName of [names.policy, names.policySequence]) {
      if (env[conflictingName]?.trim()) {
        return invalidDevelopmentScenario(
          `${names.scenario} and ${conflictingName} are mutually exclusive`
        );
      }
    }
    return createPresetPolicySteps(preset, fallbackMinimumVersion);
  }
  const sequence = env[names.policySequence]?.trim();
  const single = env[names.policy]?.trim();
  if (sequence && single) {
    return invalidDevelopmentScenario(
      `${names.policy} and ${names.policySequence} are mutually exclusive`
    );
  }
  const rawSteps = sequence
    ? sequence.split(",").map((value) => value.trim())
    : single
      ? [single]
      : invalidDevelopmentScenario(
          `${names.policy} or ${names.policySequence} is required`
        );
  if (rawSteps.some((value) => value.length === 0)) {
    return invalidDevelopmentScenario(
      `${names.policySequence} contains an empty step`
    );
  }
  return rawSteps.map((token) =>
    createPolicyStep(token, fallbackMinimumVersion)
  );
}

function configuredVersion(
  minimum: DesktopUpdateDevelopmentPolicyMinimum,
  currentVersion: string
): string {
  return minimum.kind === "configured" ? minimum.version : currentVersion;
}

export function validateDevelopmentPolicyScenarioForCurrentVersion(
  scenario: DesktopUpdateDevelopmentPolicyScenario,
  currentVersion: string
): void {
  const normalizedCurrent = validateStrictDevelopmentSemVer(
    currentVersion,
    desktopUpdateAdmissionDevelopmentEnvironment.currentVersion
  );
  const current = parseDevelopmentManagedVersion(normalizedCurrent);
  for (const step of scenario.policySteps) {
    if (step.outcome === "unmanagedPrerelease") {
      if (current) {
        invalidDevelopmentScenario(
          "unmanagedPrerelease requires an unmanaged currentVersion"
        );
      }
      continue;
    }
    if (step.outcome !== "allowed" && step.outcome !== "upgradeRequired") {
      if (
        (step.outcome === "minimumNotConfigured" ||
          step.outcome === "unsupported") &&
        !current
      ) {
        invalidDevelopmentScenario(
          `${step.outcome} requires a managed currentVersion`
        );
      }
      continue;
    }
    if (!current) {
      invalidDevelopmentScenario(
        `${step.outcome} requires a managed currentVersion`
      );
    }
    const minimumVersion = configuredVersion(step.minimum, normalizedCurrent);
    const minimum = parseDevelopmentManagedVersion(minimumVersion);
    if (!minimum || minimum.channel !== current.channel) {
      invalidDevelopmentScenario(
        `minimumVersion must use the ${current.channel} channel`
      );
    }
    const compared = compareDevelopmentManagedVersions(current, minimum);
    if (step.outcome === "allowed" && compared < 0) {
      invalidDevelopmentScenario(
        "allowed requires currentVersion to meet minimumVersion"
      );
    }
    if (step.outcome === "upgradeRequired" && compared >= 0) {
      invalidDevelopmentScenario(
        "upgradeRequired requires currentVersion below minimumVersion"
      );
    }
  }
}

export function resolveDesktopUpdateDevelopmentPolicyScenario(input: {
  env: Readonly<Record<string, string | undefined>>;
}): DesktopUpdateDevelopmentPolicyScenario | null {
  if (!readDevelopmentEnabled(input.env)) {
    return null;
  }
  const scenario: DesktopUpdateDevelopmentPolicyScenario = {
    featureKeys: resolveFeatureKeys(input.env),
    policySteps: resolvePolicySteps(input.env)
  };
  return Object.freeze({
    featureKeys: scenario.featureKeys,
    policySteps: Object.freeze(
      scenario.policySteps.map((step) =>
        Object.freeze(
          "minimum" in step
            ? {
                ...step,
                minimum: Object.freeze({ ...step.minimum })
              }
            : { ...step }
        )
      )
    )
  });
}
