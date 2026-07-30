import { useEffect, useState } from "react";
import { Button, LoadingIcon } from "@tutti-os/ui-system";
import type { MinimumVersionUpgradeState } from "@shared/contracts/ipc.ts";
import type { DesktopUpdateApi } from "@preload/types";
import { useTranslation } from "../i18n";

type MinimumVersionUpgradePort = DesktopUpdateApi["minimumVersion"];

function percent(value: number | null): string {
  return `${Math.max(0, Math.min(100, Math.round(value ?? 0)))}%`;
}

export function MinimumVersionUpgradeApp({
  port
}: {
  port: MinimumVersionUpgradePort;
}) {
  const { t } = useTranslation();
  const foreground =
    new URLSearchParams(window.location.search).get("mode") === "foreground";
  const [state, setState] = useState<MinimumVersionUpgradeState | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    let disposed = false;
    void port.getState().then((value) => {
      if (!disposed) {
        setState(value ?? null);
      }
    });
    const unsubscribe = port.onState(setState);
    return () => {
      disposed = true;
      unsubscribe?.();
    };
  }, [port]);

  const run = (operation: () => Promise<unknown>) => {
    setPending(true);
    void operation()
      .catch(() => undefined)
      .finally(() => setPending(false));
  };

  if (!state) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-[var(--background)] text-[var(--foreground)]">
        <LoadingIcon className="size-6 animate-spin" />
      </main>
    );
  }

  const prompt = foreground && state.phase === "blocked";
  const checking = state.phase === "checking";
  const downloading = state.phase === "downloading";
  const failed = state.phase === "error";
  const title = prompt
    ? t("minimumVersion.foregroundTitle")
    : failed
      ? t("minimumVersion.failedTitle")
      : downloading
        ? t("minimumVersion.downloadingTitle")
        : checking
          ? t("minimumVersion.checkingTitle")
          : t("minimumVersion.startupTitle");

  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--background)] p-8 text-[var(--foreground)]">
      <section className="w-full max-w-[440px] rounded-xl border border-[var(--border)] bg-[var(--card)] p-6 shadow-sm">
        <h1 className="text-lg font-semibold">{title}</h1>
        <p className="mt-2 text-sm leading-5 text-[var(--muted-foreground)]">
          {prompt
            ? t("minimumVersion.foregroundDetail")
            : failed
              ? t(
                  `minimumVersion.errors.${state.message ?? "updateFailed"}` as
                    | "minimumVersion.errors.releaseBelowMinimum"
                    | "minimumVersion.errors.policyCheckFailed"
                    | "minimumVersion.errors.installFailed"
                    | "minimumVersion.errors.updateFailed"
                )
              : t("minimumVersion.startupDetail")}
        </p>
        {failed && state.update.message ? (
          <p className="mt-2 break-words text-xs leading-5 text-[var(--destructive)]">
            {state.update.message}
          </p>
        ) : null}
        <dl className="mt-6 grid grid-cols-[auto_1fr] gap-2 rounded-lg bg-[var(--muted)] p-4 text-sm">
          <dt className="text-[var(--muted-foreground)]">
            {t("minimumVersion.currentVersion")}
          </dt>
          <dd className="text-right">{state.check.currentVersion}</dd>
          <dt className="text-[var(--muted-foreground)]">
            {t("minimumVersion.minimumVersion")}
          </dt>
          <dd className="text-right">{state.check.minimumVersion}</dd>
        </dl>
        {downloading ? (
          <div className="mt-5">
            <div className="mb-2 flex justify-between text-xs text-[var(--muted-foreground)]">
              <span>{t("minimumVersion.downloadProgress")}</span>
              <span>{percent(state.update.downloadPercent)}</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-[var(--muted)]">
              <div
                className="h-full bg-[var(--primary)] transition-[width]"
                style={{ width: percent(state.update.downloadPercent) }}
              />
            </div>
            <p className="mt-3 text-xs text-[var(--muted-foreground)]">
              {t("minimumVersion.autoRestartNotice")}
            </p>
          </div>
        ) : null}
        <div className="mt-6 flex flex-wrap justify-end gap-2">
          {prompt ? (
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => run(() => port.later())}
            >
              {t("minimumVersion.later")}
            </Button>
          ) : null}
          {failed ? (
            <>
              <Button
                variant="secondary"
                disabled={pending}
                onClick={() => run(() => port.exit())}
              >
                {t("minimumVersion.exit")}
              </Button>
              <Button
                variant="secondary"
                disabled={pending}
                onClick={() => run(() => port.openManualDownload())}
              >
                {t("minimumVersion.manualDownload")}
              </Button>
              <Button
                disabled={pending}
                onClick={() => run(() => port.retry())}
              >
                {t("minimumVersion.retry")}
              </Button>
            </>
          ) : state.phase === "blocked" ? (
            <Button disabled={pending} onClick={() => run(() => port.start())}>
              {prompt
                ? t("minimumVersion.upgradeNow")
                : t("minimumVersion.upgrade")}
            </Button>
          ) : null}
        </div>
      </section>
    </main>
  );
}
