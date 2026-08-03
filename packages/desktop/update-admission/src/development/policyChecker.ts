import type {
  DesktopProduct,
  MinimumVersionCheckRequest,
  MinimumVersionCheckResponse
} from "../contracts/index.ts";
import { validateMinimumVersionResponse } from "../core/index.ts";
import type {
  DesktopUpdateDevelopmentPolicyMinimum,
  DesktopUpdateDevelopmentPolicyScenario,
  DesktopUpdateDevelopmentPolicyStep
} from "./policyScenario.ts";
import { validateDevelopmentPolicyScenarioForCurrentVersion } from "./policyScenario.ts";

function developmentChannel(
  currentVersion: string
): "stable" | "rc" | "unmanaged" {
  if (
    /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-rc\.(0|[1-9]\d*)$/u.test(
      currentVersion
    )
  ) {
    return "rc";
  }
  if (
    /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/u.test(
      currentVersion
    )
  ) {
    return "stable";
  }
  return "unmanaged";
}

function abortError(): Error {
  const error = new Error("development minimum-version policy check aborted");
  error.name = "AbortError";
  return error;
}

function waitForAbort(signal: AbortSignal): Promise<never> {
  if (signal.aborted) {
    return Promise.reject(abortError());
  }
  return new Promise<never>((_resolve, reject) => {
    signal.addEventListener("abort", () => reject(abortError()), {
      once: true
    });
  });
}

function responseForStep<TProduct extends DesktopProduct>(
  request: MinimumVersionCheckRequest<TProduct>,
  step: DesktopUpdateDevelopmentPolicyStep,
  revision: number,
  featureKeys: readonly string[]
): MinimumVersionCheckResponse {
  const channel = developmentChannel(request.currentVersion);
  const base = {
    channel,
    featureAvailability: {
      keys: featureKeys
    },
    policyRevision: `development-policy-${revision}`
  } as const;
  const minimumVersion = (
    minimum: DesktopUpdateDevelopmentPolicyMinimum
  ): string =>
    minimum.kind === "configured" ? minimum.version : request.currentVersion;
  const validatedResponse = (value: unknown): MinimumVersionCheckResponse => {
    validateMinimumVersionResponse(value, request);
    return value as MinimumVersionCheckResponse;
  };
  switch (step.outcome) {
    case "allowed":
      return validatedResponse({
        ...base,
        channel,
        decision: "allowed",
        minimumVersion: minimumVersion(step.minimum),
        reason: "meetsMinimum"
      });
    case "upgradeRequired":
      return validatedResponse({
        ...base,
        channel,
        decision: "upgradeRequired",
        minimumVersion: minimumVersion(step.minimum),
        reason: "belowMinimum"
      });
    case "minimumNotConfigured":
      return validatedResponse({
        ...base,
        decision: "allowed",
        reason: "minimumNotConfigured"
      });
    case "unsupported":
      return validatedResponse({
        ...base,
        decision: "notApplicable",
        reason: "unsupportedRelease"
      });
    case "unmanagedPrerelease":
      return validatedResponse({
        ...base,
        channel: "unmanaged",
        decision: "notApplicable",
        reason: "unmanagedPrerelease"
      });
    case "error":
    case "timeout":
      throw new Error(`cannot synthesize ${step.outcome} as a policy response`);
  }
}

export function createDevelopmentMinimumVersionChecker(
  policy: DesktopUpdateDevelopmentPolicyScenario,
  options: {
    expectedCurrentVersion?: string;
  } = {}
): <TProduct extends DesktopProduct>(
  request: MinimumVersionCheckRequest<TProduct>,
  signal: AbortSignal
) => Promise<MinimumVersionCheckResponse> {
  const checkIndexes = new Map<string, number>();
  const policyFeatureKeys = policy.featureKeys;
  return async <TProduct extends DesktopProduct>(
    request: MinimumVersionCheckRequest<TProduct>,
    signal: AbortSignal
  ): Promise<MinimumVersionCheckResponse> => {
    if (
      options.expectedCurrentVersion &&
      request.currentVersion !== options.expectedCurrentVersion
    ) {
      throw new Error(
        `development policy expected currentVersion ${options.expectedCurrentVersion}, received ${request.currentVersion}`
      );
    }
    const requestIdentity = [
      request.product,
      request.platform,
      request.architecture
    ].join(":");
    const checkIndex = checkIndexes.get(requestIdentity) ?? 0;
    const stepIndex = Math.min(checkIndex, policy.policySteps.length - 1);
    const step = policy.policySteps[stepIndex]!;
    checkIndexes.set(requestIdentity, checkIndex + 1);
    if (step.outcome === "timeout") {
      return await waitForAbort(signal);
    }
    if (step.outcome === "error") {
      throw new Error(step.message);
    }
    validateDevelopmentPolicyScenarioForCurrentVersion(
      { featureKeys: policyFeatureKeys, policySteps: [step] },
      request.currentVersion
    );
    return responseForStep(request, step, stepIndex + 1, policyFeatureKeys);
  };
}
