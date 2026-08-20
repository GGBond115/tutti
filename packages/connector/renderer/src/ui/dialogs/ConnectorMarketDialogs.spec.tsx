import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { proxy } from "valtio";

import type { IConnectorMarketRoot } from "../../application/services/core/connectorMarketRoot.interface.ts";
import type {
  ConnectorAuthorizationDialogView,
  ConnectorManagementDialogView,
  ConnectorMarketViewState
} from "../../application/services/view/connectorMarketViewTypes.ts";
import { ConnectorMarketRootProvider } from "../ConnectorMarketServicesContext.tsx";
import type { ConnectorMarketI18nRuntime } from "../i18n/connectorMarketI18n.ts";
import { ConnectorMarketDialogs } from "./ConnectorMarketDialogs.tsx";

afterEach(cleanup);

const i18n = {
  has: () => true,
  t: (key: string) => key,
  tFirst: (keys: readonly string[]) => keys[0] ?? ""
} as ConnectorMarketI18nRuntime;

const authorizationDialog: ConnectorAuthorizationDialogView = {
  authorizationKind: "oauth2",
  authorizing: false,
  brokeredAuthorization: false,
  connectorKey: "gmail",
  description: "Gmail tools",
  displayName: "Gmail",
  iconUrl: "/gmail.png",
  kind: "authorization",
  pending: false,
  permissions: []
};

const managementDialog: ConnectorManagementDialogView = {
  canAuthorize: true,
  canUninstall: true,
  connectorKey: "gmail",
  description: "Gmail tools",
  details: [],
  displayName: "Gmail",
  iconUrl: "/gmail.png",
  kind: "management",
  permissions: []
};

function emptyView(
  dialog: ConnectorMarketViewState["dialog"]
): ConnectorMarketViewState {
  return {
    cardsByKey: {},
    catalogError: null,
    dialog,
    refreshing: false,
    sections: [],
    status: "ready"
  };
}

describe("ConnectorMarketDialogs", () => {
  it("keeps the dialog open after authorization so disconnect and try stay available", async () => {
    const viewState = proxy(emptyView(authorizationDialog));
    const closeDialog = vi.fn();
    const onTryConnector = vi.fn();
    const root = {
      market: {
        beginAuthorization: vi.fn(async () => {
          viewState.dialog = managementDialog;
        }),
        dataStore: proxy({
          pendingUninstallNotificationsByOperationId: {}
        })
      },
      uiState: {
        closeDialog,
        dataStore: proxy({
          dialog: { connectorKey: "gmail", kind: "connector" },
          query: "",
          scope: {},
          started: true
        })
      },
      view: { dataStore: viewState }
    } as unknown as IConnectorMarketRoot;

    render(
      <ConnectorMarketRootProvider
        i18n={i18n}
        onTryConnector={onTryConnector}
        root={root}
      >
        <ConnectorMarketDialogs />
      </ConnectorMarketRootProvider>
    );

    fireEvent.click(screen.getByRole("button", { name: "actionAuthorize" }));

    expect(
      await screen.findByRole("button", { name: "actionTry" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "actionDisconnect" })
    ).toBeInTheDocument();
    expect(closeDialog).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "actionTry" }));
    expect(closeDialog).toHaveBeenCalledTimes(1);
    expect(onTryConnector).toHaveBeenCalledWith("gmail");
  });
});
