import type { ConnectorRendererCardView as ConnectorCardView } from "../../connectorRendererSurface.ts";

export function connectorCardActionStartsInstallation(
  action: ConnectorCardView["action"]
): boolean {
  return action === "install" || action === "update";
}
