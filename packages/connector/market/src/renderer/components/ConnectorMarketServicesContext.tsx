import {
  createContext,
  useContext,
  useMemo,
  useSyncExternalStore,
  type ReactNode
} from "react";

import type { ConnectorMarketI18nRuntime } from "../../i18n/connectorMarketI18n.ts";
import {
  requireConnectorRendererSecureSubmissionPort,
  type ConnectorRendererModel,
  type ConnectorRendererSurfaceSnapshot
} from "../connectorRendererModel.ts";

export interface ConnectorMarketServices {
  i18n: ConnectorMarketI18nRuntime;
  locale: string;
  model: ConnectorRendererModel;
  snapshot: ConnectorRendererSurfaceSnapshot;
  secureSubmission: ReturnType<
    typeof requireConnectorRendererSecureSubmissionPort
  >;
  onError?: (message: string) => void;
  onTryConnector?: (connectorKey: string) => void;
}

export interface ConnectorMarketRendererProviderProps {
  children: ReactNode;
  i18n: ConnectorMarketI18nRuntime;
  locale?: string;
  onError?: (message: string) => void;
  onTryConnector?: (connectorKey: string) => void;
  model: ConnectorRendererModel;
}

const ConnectorMarketServicesContext =
  createContext<ConnectorMarketServices | null>(null);

export function ConnectorMarketServicesProvider({
  children,
  services
}: {
  children: ReactNode;
  services: ConnectorMarketServices;
}) {
  return (
    <ConnectorMarketServicesContext.Provider value={services}>
      {children}
    </ConnectorMarketServicesContext.Provider>
  );
}

export function ConnectorMarketRendererProvider({
  children,
  i18n,
  locale = "en-US",
  onError,
  onTryConnector,
  model
}: ConnectorMarketRendererProviderProps) {
  const snapshot = useSyncExternalStore(
    model.subscribeSurface,
    model.getSurfaceSnapshot,
    model.getSurfaceSnapshot
  );
  const services = useMemo(
    () => ({
      i18n,
      locale,
      model,
      snapshot,
      secureSubmission: requireConnectorRendererSecureSubmissionPort(model),
      onError,
      onTryConnector
    }),
    [i18n, locale, model, onError, onTryConnector, snapshot]
  );

  return (
    <ConnectorMarketServicesProvider services={services}>
      {children}
    </ConnectorMarketServicesProvider>
  );
}

export function useConnectorMarketServices(): ConnectorMarketServices {
  const services = useContext(ConnectorMarketServicesContext);
  if (!services) {
    throw new Error(
      "useConnectorMarketServices must be used within ConnectorMarketServicesProvider"
    );
  }
  return services;
}
