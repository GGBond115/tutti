const foregroundIntervalMs = 30 * 60 * 1_000;

export function resolveMinimumVersionRuntimeTarget(
  platform: NodeJS.Platform,
  architecture: string
): {
  platform: "macos" | "windows" | "linux";
  architecture: "arm64" | "x64";
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
  return { platform: normalizedPlatform, architecture: normalizedArchitecture };
}

function versionParts(value: string): number[] | null {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$/u.exec(value.trim());
  return match ? match.slice(1).map((part) => Number(part || 0)) : null;
}

export function releaseMeetsMinimum(
  releaseVersion: string | null,
  minimumVersion: string
): boolean {
  if (!releaseVersion) {
    return false;
  }
  const release = versionParts(releaseVersion);
  const minimum = versionParts(minimumVersion);
  if (!release || !minimum) {
    return false;
  }
  for (let index = 0; index < 4; index += 1) {
    if (release[index] !== minimum[index]) {
      return (release[index] ?? 0) > (minimum[index] ?? 0);
    }
  }
  return true;
}

export function shouldCheckMinimumVersionAfterForeground(input: {
  disposed: boolean;
  packaged: boolean;
  foregroundPrompted: boolean;
  lastCheckAt: number;
  now: number;
}): boolean {
  return !(
    input.disposed ||
    !input.packaged ||
    input.foregroundPrompted ||
    input.now - input.lastCheckAt < foregroundIntervalMs
  );
}
