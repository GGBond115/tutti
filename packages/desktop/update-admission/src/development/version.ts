import type { DesktopUpdateChannel } from "../contracts/index.ts";
import { invalidDevelopmentScenario } from "./environment.ts";

const strictSemVerPattern =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/u;
const managedStablePattern =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/u;
const managedRcPattern =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-rc\.(0|[1-9]\d*)$/u;

export type DevelopmentManagedVersion = {
  channel: DesktopUpdateChannel;
  core: [string, string, string];
  rc: string | null;
};

export function validateStrictDevelopmentSemVer(
  value: string,
  name: string
): string {
  const normalized = value.trim();
  const match = strictSemVerPattern.exec(normalized);
  if (!match) {
    return invalidDevelopmentScenario(`${name} must be valid SemVer`);
  }
  const prerelease = match[4];
  if (prerelease) {
    for (const identifier of prerelease.split(".")) {
      if (
        /^\d+$/u.test(identifier) &&
        identifier.length > 1 &&
        identifier[0] === "0"
      ) {
        return invalidDevelopmentScenario(`${name} must be valid SemVer`);
      }
    }
  }
  return normalized;
}

export function parseDevelopmentManagedVersion(
  value: string
): DevelopmentManagedVersion | null {
  const stable = managedStablePattern.exec(value);
  if (stable) {
    return {
      channel: "stable",
      core: [stable[1]!, stable[2]!, stable[3]!],
      rc: null
    };
  }
  const rc = managedRcPattern.exec(value);
  if (!rc) {
    return null;
  }
  return {
    channel: "rc",
    core: [rc[1]!, rc[2]!, rc[3]!],
    rc: rc[4]!
  };
}

function compareNumericIdentifier(left: string, right: string): number {
  if (left.length !== right.length) {
    return left.length < right.length ? -1 : 1;
  }
  return left === right ? 0 : left < right ? -1 : 1;
}

export function compareDevelopmentManagedVersions(
  left: DevelopmentManagedVersion,
  right: DevelopmentManagedVersion
): number {
  for (const index of [0, 1, 2] as const) {
    const compared = compareNumericIdentifier(
      left.core[index],
      right.core[index]
    );
    if (compared !== 0) {
      return compared;
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
