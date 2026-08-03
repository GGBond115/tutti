import type {
  DesktopFeatureAvailability,
  DesktopFeatureAvailabilitySnapshot,
  DesktopProduct,
  MinimumVersionCheckRequest
} from "../contracts/index.ts";

const featureKeyPattern = /^[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*$/u;
const maximumFeatureKeyLength = 128;
const maximumFeatureKeys = 256;

export function isValidDesktopFeatureKey(value: string): boolean {
  return (
    value.length <= maximumFeatureKeyLength && featureKeyPattern.test(value)
  );
}

export function normalizeDesktopFeatureKeys(value: unknown): readonly string[] {
  if (!Array.isArray(value) || value.length > maximumFeatureKeys) {
    throw new Error(
      "feature availability keys must be an array of at most 256 keys"
    );
  }
  const keys = value.map((entry) => {
    if (typeof entry !== "string" || !isValidDesktopFeatureKey(entry)) {
      throw new Error("feature availability contains an invalid key");
    }
    return entry;
  });
  const sorted = [...new Set(keys)].sort();
  if (sorted.length !== keys.length) {
    throw new Error("feature availability contains duplicate keys");
  }
  return Object.freeze(sorted);
}

export function parseDesktopFeatureAvailability(
  response: unknown
): DesktopFeatureAvailability | null {
  if (!response || typeof response !== "object") {
    throw new Error("desktop version response must be an object");
  }
  const value = (response as Record<string, unknown>).featureAvailability;
  if (value === undefined) {
    return null;
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("feature availability must be an object");
  }
  const record = value as Record<string, unknown>;
  if (
    Object.keys(record).some((key) => key !== "keys") ||
    !Object.prototype.hasOwnProperty.call(record, "keys")
  ) {
    throw new Error("feature availability has invalid fields");
  }
  return Object.freeze({
    keys: normalizeDesktopFeatureKeys(record.keys)
  });
}

export function desktopFeatureAvailabilityIdentityMatches<
  TProduct extends DesktopProduct
>(
  snapshot: DesktopFeatureAvailabilitySnapshot,
  identity: MinimumVersionCheckRequest<TProduct>
): snapshot is DesktopFeatureAvailabilitySnapshot<TProduct> {
  return (
    snapshot.product === identity.product &&
    snapshot.platform === identity.platform &&
    snapshot.architecture === identity.architecture &&
    snapshot.currentVersion === identity.currentVersion
  );
}

export function isDesktopFeatureSupported(
  snapshot: Pick<DesktopFeatureAvailabilitySnapshot, "keys">,
  key: string
): boolean {
  return isValidDesktopFeatureKey(key) && snapshot.keys.includes(key);
}
