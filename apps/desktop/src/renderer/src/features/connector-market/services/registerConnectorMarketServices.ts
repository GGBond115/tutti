import type { ServiceRegistry } from "@tutti-os/infra/di";
import {
  ConnectorMarketService,
  IConnectorMarketService,
  type IConnectorMarketService as ConnectorMarketServiceInterface
} from "@tutti-os/connector-market/services";
import type {
  ConnectorMarketClient,
  TuttidEventStreamClient
} from "@tutti-os/client-tuttid-ts";
import { createDesktopConnectorMarketBackend } from "./internal/desktopConnectorMarketBackend.ts";
import { createDesktopConnectorMarketEvents } from "./internal/desktopConnectorMarketEvents.ts";

export interface ConnectorMarketServiceRegistrationInput {
  client: ConnectorMarketClient;
  eventStreamClient: TuttidEventStreamClient;
  openAuthorizationUrl?: (url: string) => Promise<void>;
  reportDiagnostic?: (error: unknown) => void;
  workspaceId: string;
}

export function registerConnectorMarketServices(
  registry: ServiceRegistry,
  input: ConnectorMarketServiceRegistrationInput
): ConnectorMarketServiceInterface {
  const service = new ConnectorMarketService({
    backend: createDesktopConnectorMarketBackend(input.client),
    events: createDesktopConnectorMarketEvents(input.eventStreamClient),
    workspaceId: input.workspaceId,
    openAuthorizationUrl: input.openAuthorizationUrl,
    reportDiagnostic: input.reportDiagnostic
  });
  service.start();
  void service.ensureLoaded().catch(() => undefined);
  registry.registerInstance(IConnectorMarketService, service);
  return service;
}
