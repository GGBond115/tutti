export type ConnectorRendererEvent =
  | { readonly type: "catalog.requested" }
  | {
      readonly type: "connector.details.requested";
      readonly connectorKey: string;
    }
  | {
      readonly type: "authorization.requested";
      readonly connectorKey: string;
    }
  | {
      readonly type: "external-url.requested";
      readonly url: string;
    }
  | { readonly type: "account-admission.requested" }
  | {
      readonly type: "try-connector.requested";
      readonly connectorKey: string;
    };

export type ConnectorRendererEventSink = (
  event: ConnectorRendererEvent
) => void;
