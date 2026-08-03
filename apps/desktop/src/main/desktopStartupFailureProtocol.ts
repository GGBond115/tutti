import { formatErrorMessage } from "../shared/errors/desktopErrors.ts";

export const desktopStartupFailurePrefix = "[tutti-desktop-startup-failed] ";

export interface DesktopStartupFailure {
  cause?: {
    code: string;
    message: string;
  };
  message: string;
}

export function desktopStartupFailure(error: unknown): DesktopStartupFailure {
  const cause = structuredCause(error instanceof Error ? error.cause : null);
  return {
    ...(cause ? { cause } : {}),
    message: formatErrorMessage(error)
  };
}

function structuredCause(
  value: unknown
): { code: string; message: string } | null {
  const cause = value as { code?: unknown; message?: unknown };
  if (
    !cause ||
    typeof cause !== "object" ||
    typeof cause.code !== "string" ||
    !cause.code.trim() ||
    typeof cause.message !== "string" ||
    !cause.message.trim()
  ) {
    return null;
  }
  return {
    code: cause.code.trim(),
    message: cause.message.trim()
  };
}
