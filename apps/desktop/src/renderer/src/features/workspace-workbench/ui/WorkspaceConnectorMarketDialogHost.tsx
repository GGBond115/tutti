import { ConnectorDialogHost } from "@tutti-os/connector-market/renderer";
import { createConnectorMarketI18nRuntime } from "@tutti-os/connector-market/i18n";
import { getConnectorRendererModel } from "@tutti-os/connector-market/composition";
import { IConnectorMarketModule } from "@tutti-os/connector-market/services";
import { useService } from "@tutti-os/infra/di";
import { INotificationService } from "@tutti-os/ui-notifications";
import { useCallback, useMemo } from "react";
import { useTranslation } from "@renderer/i18n";

/** One canonical connector-market dialog host for each workbench window. */
export function WorkspaceConnectorMarketDialogHost() {
  const { i18n: appI18n, locale } = useTranslation();
  const i18n = useMemo(
    () => createConnectorMarketI18nRuntime(appI18n),
    [appI18n]
  );
  const connectorMarketModule = useService(IConnectorMarketModule);
  const notifications = useService(INotificationService);
  const handleError = useCallback(
    (message: string) => notifications.error({ title: message }),
    [notifications]
  );

  return (
    <ConnectorDialogHost
      i18n={i18n}
      locale={locale}
      onError={handleError}
      model={getConnectorRendererModel(connectorMarketModule.rendererPorts)}
    />
  );
}
