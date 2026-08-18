import { cn } from "@tutti-os/ui-system/utils";

import type { ConnectorMarketI18nRuntime } from "../../i18n/connectorMarketI18n.ts";
import type { ConnectorRendererModel } from "../connectorRendererModel.ts";
import { ConnectorMarketRendererProvider } from "./ConnectorMarketServicesContext.tsx";
import { ConnectorCatalog } from "./catalog/ConnectorCatalog.tsx";
import { ConnectorMarketToolbar } from "./toolbar/ConnectorMarketToolbar.tsx";

export interface ConnectorMarketPanelProps {
  className?: string;
  i18n: ConnectorMarketI18nRuntime;
  locale?: string;
  onError?: (message: string) => void;
  model: ConnectorRendererModel;
}

export function ConnectorMarketPanel({
  className,
  i18n,
  locale,
  onError,
  model
}: ConnectorMarketPanelProps) {
  return (
    <ConnectorMarketRendererProvider
      i18n={i18n}
      locale={locale}
      onError={onError}
      model={model}
    >
      <section
        className={cn("flex min-h-0 flex-1 flex-col gap-5", className)}
        data-testid="connector-market-panel"
      >
        <header className="shrink-0">
          <p className="mb-0 mt-1 text-[12px] leading-[1.5] text-[var(--text-secondary)]">
            {i18n.t("description")}
          </p>
        </header>
        <div>
          <ConnectorMarketToolbar />
        </div>
        <ConnectorCatalog />
      </section>
    </ConnectorMarketRendererProvider>
  );
}
