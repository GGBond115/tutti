import type {
  DesktopArchitecture,
  DesktopPlatform,
  DesktopProduct,
  MinimumVersionCheckRequest,
  MinimumVersionCheckResult
} from "../contracts/index.ts";

type ManagedVersion = {
  major: string;
  minor: string;
  patch: string;
  rc: string | null;
};

function parseManagedVersion(value: string): ManagedVersion | null {
  const match =
    /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.(0|[1-9]\d*))?$/u.exec(
      value.trim()
    );
  if (!match) {
    return null;
  }
  return {
    major: match[1]!,
    minor: match[2]!,
    patch: match[3]!,
    rc: match[4] === undefined ? null : match[4]
  };
}

function compareNumericIdentifier(left: string, right: string): number {
  if (left.length !== right.length) {
    return left.length < right.length ? -1 : 1;
  }
  return left === right ? 0 : left < right ? -1 : 1;
}

function compareManagedVersions(
  left: ManagedVersion,
  right: ManagedVersion
): number {
  for (const key of ["major", "minor", "patch"] as const) {
    if (left[key] !== right[key]) {
      return compareNumericIdentifier(left[key], right[key]);
    }
  }
  if (left.rc === right.rc) {
    return 0;
  }
  if (left.rc === null) {
    return 1;
  }
  if (right.rc === null) {
    return -1;
  }
  return compareNumericIdentifier(left.rc, right.rc);
}

export function updaterTargetMeetsMinimum(
  releaseVersion: string | null,
  minimumVersion: string
): boolean {
  if (!releaseVersion) {
    return false;
  }
  const release = parseManagedVersion(releaseVersion);
  const minimum = parseManagedVersion(minimumVersion);
  return Boolean(
    release && minimum && compareManagedVersions(release, minimum) >= 0
  );
}

export function resolveMinimumVersionRuntimeTarget(
  platform: NodeJS.Platform,
  architecture: string
): {
  platform: DesktopPlatform;
  architecture: DesktopArchitecture;
} | null {
  const normalizedPlatform =
    platform === "darwin"
      ? "macos"
      : platform === "win32"
        ? "windows"
        : platform === "linux"
          ? "linux"
          : null;
  const normalizedArchitecture =
    architecture === "arm64" ? "arm64" : architecture === "x64" ? "x64" : null;
  if (!normalizedPlatform || !normalizedArchitecture) {
    return null;
  }
  return {
    architecture: normalizedArchitecture,
    platform: normalizedPlatform
  };
}

export function validateMinimumVersionResponse<TProduct extends DesktopProduct>(
  value: unknown,
  request: MinimumVersionCheckRequest<TProduct>
): MinimumVersionCheckResult<TProduct> {
  if (!value || typeof value !== "object") {
    throw new Error("minimum version response must be an object");
  }
  const response = value as Record<string, unknown>;
  if (
    typeof response.policyRevision !== "string" ||
    response.policyRevision.trim() === ""
  ) {
    throw new Error("minimum version response has invalid policy revision");
  }
  if (response.channel === "unmanaged") {
    if (
      response.decision !== "notApplicable" ||
      response.reason !== "unmanagedPrerelease" ||
      "minimumVersion" in response
    ) {
      throw new Error("minimum version response has invalid unmanaged policy");
    }
    return {
      ...request,
      channel: response.channel,
      decision: response.decision,
      policyRevision: response.policyRevision,
      reason: response.reason
    };
  }
  if (response.channel !== "stable" && response.channel !== "rc") {
    throw new Error("minimum version response has invalid channel");
  }
  if (response.decision === "notApplicable") {
    if (
      response.reason !== "unsupportedRelease" ||
      "minimumVersion" in response
    ) {
      throw new Error(
        "minimum version response has invalid non-applicable policy"
      );
    }
    return {
      ...request,
      channel: response.channel,
      decision: response.decision,
      policyRevision: response.policyRevision,
      reason: response.reason
    };
  }
  if (
    response.decision === "allowed" &&
    response.reason === "minimumNotConfigured"
  ) {
    if ("minimumVersion" in response) {
      throw new Error(
        "minimum version response has invalid unconfigured policy"
      );
    }
    return {
      ...request,
      channel: response.channel,
      decision: response.decision,
      policyRevision: response.policyRevision,
      reason: response.reason
    };
  }
  if (
    response.decision !== "allowed" &&
    response.decision !== "upgradeRequired"
  ) {
    throw new Error("minimum version response has invalid decision");
  }
  const pattern =
    response.channel === "rc"
      ? /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-rc\.(0|[1-9]\d*)$/u
      : /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
  const expectedReason =
    response.decision === "allowed" ? "meetsMinimum" : "belowMinimum";
  if (
    typeof response.minimumVersion !== "string" ||
    !pattern.test(response.minimumVersion) ||
    response.reason !== expectedReason
  ) {
    throw new Error("minimum version response has invalid managed policy");
  }
  return {
    ...request,
    channel: response.channel,
    decision: response.decision,
    minimumVersion: response.minimumVersion,
    policyRevision: response.policyRevision,
    reason: response.reason
  } as MinimumVersionCheckResult<TProduct>;
}
