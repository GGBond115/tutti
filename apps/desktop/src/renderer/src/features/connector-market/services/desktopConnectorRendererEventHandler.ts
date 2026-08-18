import type { ConnectorRendererEvent } from "@tutti-os/connector-market/renderer";

export interface DesktopConnectorRendererEventPorts {
  openCatalog(): void;
  openConnector(connectorKey: string): void | Promise<void>;
  openExternalUrl?(url: string): void | Promise<void>;
  requestAccountAdmission?(): void | Promise<void>;
  tryConnector?(connectorKey: string): void | Promise<void>;
  reportUnsupportedEvent?(event: unknown): void;
}

/** Product-owned container/navigation routing for Connector renderer intents. */
export function handleDesktopConnectorRendererEvent(
  event: ConnectorRendererEvent,
  ports: DesktopConnectorRendererEventPorts
): void {
  switch (event.type) {
    case "catalog.requested":
      ports.openCatalog();
      return;
    case "authorization.requested":
    case "connector.details.requested":
      void ports.openConnector(event.connectorKey);
      return;
    case "external-url.requested":
      if (ports.openExternalUrl) {
        void ports.openExternalUrl(event.url);
      }
      return;
    case "account-admission.requested":
      if (ports.requestAccountAdmission) {
        void ports.requestAccountAdmission();
      }
      return;
    case "try-connector.requested":
      if (ports.tryConnector) {
        void ports.tryConnector(event.connectorKey);
      }
      return;
    default:
      ports.reportUnsupportedEvent?.(event);
  }
}
