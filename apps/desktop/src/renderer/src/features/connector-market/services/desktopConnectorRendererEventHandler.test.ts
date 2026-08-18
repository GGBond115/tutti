import assert from "node:assert/strict";
import test from "node:test";

import { handleDesktopConnectorRendererEvent } from "./desktopConnectorRendererEventHandler.ts";

test("routes Connector renderer navigation through closed Desktop ports", () => {
  const routed: string[] = [];
  const ports = {
    openCatalog: () => {
      routed.push("catalog");
    },
    openConnector: (connectorKey: string) => {
      routed.push(`connector:${connectorKey}`);
    },
    openExternalUrl: (url: string) => {
      routed.push(`url:${url}`);
    },
    requestAccountAdmission: () => {
      routed.push("admission");
    },
    tryConnector: (connectorKey: string) => {
      routed.push(`try:${connectorKey}`);
    }
  };

  handleDesktopConnectorRendererEvent({ type: "catalog.requested" }, ports);
  handleDesktopConnectorRendererEvent(
    { type: "authorization.requested", connectorKey: "github" },
    ports
  );
  handleDesktopConnectorRendererEvent(
    { type: "connector.details.requested", connectorKey: "notion" },
    ports
  );
  handleDesktopConnectorRendererEvent(
    { type: "external-url.requested", url: "https://example.test" },
    ports
  );
  handleDesktopConnectorRendererEvent(
    { type: "account-admission.requested" },
    ports
  );
  handleDesktopConnectorRendererEvent(
    { type: "try-connector.requested", connectorKey: "figma" },
    ports
  );

  assert.deepEqual(routed, [
    "catalog",
    "connector:github",
    "connector:notion",
    "url:https://example.test",
    "admission",
    "try:figma"
  ]);
});
