import type { ConnectorMarketI18nRuntime } from "../../i18n/connectorMarketI18n.ts";
import type { ConnectorRendererModel } from "../connectorRendererModel.ts";
import { ConnectorMarketRendererProvider } from "./ConnectorMarketServicesContext.tsx";
import { ConnectorMarketDialogs } from "./dialogs/ConnectorMarketDialogs.tsx";

export interface ConnectorMarketDialogHostProps {
  i18n: ConnectorMarketI18nRuntime;
  locale?: string;
  onError?: (message: string) => void;
  model: ConnectorRendererModel;
}

/**
 * Window-level host for the connector market's canonical installation,
 * authorization, installation, and compatibility dialogs.
 */
export function ConnectorMarketDialogHost({
  i18n,
  locale,
  onError,
  model
}: ConnectorMarketDialogHostProps) {
  return (
    <ConnectorMarketRendererProvider
      i18n={i18n}
      locale={locale}
      onError={onError}
      model={model}
    >
      <ConnectorMarketDialogs />
    </ConnectorMarketRendererProvider>
  );
}
