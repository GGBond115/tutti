import { Button, Spinner } from "@tutti-os/ui-system/components";
import { useSnapshot } from "valtio";

import { useConnectorMarketServices } from "../ConnectorMarketServicesContext.tsx";
import { ConnectorCard } from "./ConnectorCard.tsx";

export function ConnectorCatalog() {
  const { i18n, market, view } = useConnectorMarketServices();
  const snapshot = useSnapshot(view.dataStore);

  if (snapshot.status === "loading") {
    return (
      <div className="flex min-h-48 items-center justify-center gap-2 text-[13px] text-[var(--text-secondary)]">
        <Spinner size={16} />
        {i18n.t("loading")}
      </div>
    );
  }
  if (snapshot.status === "error") {
    return (
      <div className="flex min-h-48 flex-col items-center justify-center gap-3 rounded-lg border border-[var(--border-1)] bg-[var(--transparency-block)] text-center">
        <div>
          <p className="m-0 text-[13px] font-medium text-[var(--text-primary)]">
            {i18n.t("catalogError")}
          </p>
          {snapshot.lastErrorCode ? (
            <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">
              {snapshot.lastErrorCode}
            </p>
          ) : null}
        </div>
        <Button
          size="sm"
          type="button"
          variant="secondary"
          onClick={() => void market.reload().catch(() => undefined)}
        >
          {i18n.t("actionRetry")}
        </Button>
      </div>
    );
  }

  const connectorKeys = snapshot.sections[0]?.connectorKeys ?? [];
  if (connectorKeys.length === 0) {
    return (
      <div className="flex min-h-48 items-center justify-center rounded-lg border border-dashed border-[var(--border-1)] text-[13px] text-[var(--text-tertiary)]">
        {i18n.t("catalogEmpty")}
      </div>
    );
  }

  return (
    <section aria-label={i18n.t("catalogSection")}>
      <h3 className="mb-3 mt-0 text-[13px] font-semibold text-[var(--text-secondary)]">
        {i18n.t("catalogSection")}
      </h3>
      <div className="grid grid-cols-2 gap-3 max-[760px]:grid-cols-1">
        {connectorKeys.map((connectorKey) => (
          <ConnectorCard key={connectorKey} connectorKey={connectorKey} />
        ))}
      </div>
    </section>
  );
}
