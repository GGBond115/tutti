import { memo, useEffect } from "react";
import registrationCreditsBackgroundUrl from "@tutti-os/commerce/assets/registration-credits-bg.png";
import titiRunUrl from "@tutti-os/commerce/assets/titi-run.png";
import { CloseIcon } from "@tutti-os/ui-system";
import type { RegistrationCreditsToastState } from "../index";

export interface RegistrationCreditsToastLabels {
  title: string;
  creditsUnit: string;
  close: string;
}

export interface RegistrationCreditsToastProps {
  toast: RegistrationCreditsToastState;
  labels: RegistrationCreditsToastLabels;
}

const defaultAutoDismissMs = 120_000;

export const RegistrationCreditsToast = memo(function RegistrationCreditsToast({
  toast,
  labels
}: RegistrationCreditsToastProps): React.JSX.Element | null {
  "use memo";
  useEffect(() => {
    if (!toast.visible) {
      return;
    }
    const timeout = window.setTimeout(
      toast.onDismiss,
      toast.autoDismissMs ?? defaultAutoDismissMs
    );
    return () => window.clearTimeout(timeout);
  }, [toast.autoDismissMs, toast.onDismiss, toast.visible]);

  if (!toast.visible) {
    return null;
  }

  return (
    <div
      className="nodrag relative mx-3 mb-1 w-[calc(100%-24px)] max-w-[calc(100%-24px)] overflow-hidden rounded-[16px] p-2.5 pr-9 text-white [-webkit-app-region:no-drag]"
      data-testid="commerce-registration-credits-toast"
      role="status"
      style={{
        backgroundImage: `linear-gradient(90deg, rgba(18, 60, 142, 0.08), rgba(20, 66, 160, 0.2) 58%, rgba(12, 32, 92, 0.34)), url(${JSON.stringify(
          registrationCreditsBackgroundUrl
        )})`,
        backgroundPosition: "center",
        backgroundRepeat: "no-repeat",
        backgroundSize: "cover"
      }}
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/90 to-transparent" />
      <div className="relative flex min-w-0 items-center gap-2.5">
        <img
          alt=""
          aria-hidden="true"
          className="h-14 w-14 shrink-0 object-contain"
          src={titiRunUrl}
        />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[12px] font-semibold leading-4 text-white">
            {labels.title}
          </span>
          <span className="block truncate text-[20px] font-semibold leading-6 text-white">
            +{toast.creditsLabel} {labels.creditsUnit}
          </span>
        </span>
      </div>
      <button
        type="button"
        aria-label={labels.close}
        className="nodrag absolute right-2.5 top-2.5 grid h-6 w-6 place-items-center rounded-[7px] text-white/85 hover:bg-white/18 hover:text-white [-webkit-app-region:no-drag]"
        onClick={toast.onDismiss}
      >
        <CloseIcon aria-hidden="true" size={16} />
      </button>
    </div>
  );
});
