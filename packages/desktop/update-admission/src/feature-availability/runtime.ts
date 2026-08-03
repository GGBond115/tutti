import type {
  DesktopFeatureAvailabilityRuntime,
  DesktopFeatureAvailabilitySnapshot,
  DesktopProduct,
  DesktopUpdateAdmissionSnapshot,
  MinimumVersionCheckRequest
} from "../contracts/index.ts";
import {
  isDesktopFeatureSupported,
  normalizeDesktopFeatureKeys
} from "./core.ts";

export interface DesktopFeatureAvailabilityLogger {
  info(message: string): void;
  error(message: string): void;
}

export interface MutableDesktopFeatureAvailabilityRuntime<
  TProduct extends DesktopProduct = DesktopProduct
> extends DesktopFeatureAvailabilityRuntime<TProduct> {
  acceptDaemonSnapshot(
    snapshot: DesktopUpdateAdmissionSnapshot<TProduct>
  ): void;
  dispose(): void;
}

function emptySnapshot<TProduct extends DesktopProduct>(
  identity: MinimumVersionCheckRequest<TProduct>
): DesktopFeatureAvailabilitySnapshot<TProduct> {
  return Object.freeze({
    ...identity,
    fetchedAt: null,
    keys: Object.freeze([]),
    policyRevision: null,
    source: "empty"
  });
}

function log(
  logger: DesktopFeatureAvailabilityLogger,
  level: "info" | "error",
  details: Record<string, unknown>
): void {
  logger[level](`[desktop-feature-availability] ${JSON.stringify(details)}`);
}

export function createDesktopFeatureAvailabilityRuntime<
  TProduct extends DesktopProduct
>(input: {
  identity: MinimumVersionCheckRequest<TProduct>;
  logger: DesktopFeatureAvailabilityLogger;
}): MutableDesktopFeatureAvailabilityRuntime<TProduct> {
  let snapshot = emptySnapshot(input.identity);
  const listeners = new Set<
    (value: DesktopFeatureAvailabilitySnapshot<TProduct>) => void
  >();
  let disposed = false;

  return {
    acceptDaemonSnapshot(value) {
      if (disposed) {
        return;
      }
      if (
        value.identity.product !== input.identity.product ||
        value.identity.platform !== input.identity.platform ||
        value.identity.architecture !== input.identity.architecture ||
        value.identity.currentVersion !== input.identity.currentVersion
      ) {
        throw new Error("feature availability daemon identity mismatch");
      }
      const next: DesktopFeatureAvailabilitySnapshot<TProduct> = Object.freeze({
        ...input.identity,
        fetchedAt: value.featureAvailability.fetchedAt,
        keys: normalizeDesktopFeatureKeys(value.featureAvailability.keys),
        policyRevision: value.featureAvailability.policyRevision,
        source: value.featureAvailability.source
      });
      snapshot = next;
      log(input.logger, "info", {
        count: next.keys.length,
        policyRevision: next.policyRevision,
        result: "accepted",
        source: next.source,
        stage: "daemon-snapshot"
      });
      for (const listener of listeners) {
        try {
          listener(next);
        } catch (error) {
          log(input.logger, "error", {
            error: error instanceof Error ? error.message : String(error),
            result: "failure",
            stage: "subscriber-notify"
          });
        }
      }
    },
    dispose() {
      disposed = true;
      listeners.clear();
    },
    getSnapshot() {
      return snapshot;
    },
    isSupported(key) {
      return isDesktopFeatureSupported(snapshot, key);
    },
    subscribe(listener) {
      if (disposed) {
        return () => undefined;
      }
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  };
}
