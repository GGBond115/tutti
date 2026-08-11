import {
  ConfirmationDialog,
  Dialog,
  ToastProvider,
  ToastRoot,
  ToastTitle,
  ToastViewport
} from "@tutti-os/ui-system/components";
import { useEffect, useState } from "react";
import { useSnapshot } from "valtio";

import { useConnectorMarketServices } from "../ConnectorMarketServicesContext.tsx";
import { ConnectorAuthorizationDialog } from "./ConnectorAuthorizationDialog.tsx";
import { ConnectorBlockedDialog } from "./ConnectorBlockedDialog.tsx";
import { ConnectorManagementDialog } from "./ConnectorManagementDialog.tsx";
import { ConnectorInstallationDialog } from "./ConnectorInstallationDialog.tsx";

interface TrackedUninstall {
  connectorKey: string;
  displayName: string;
  operationId: string;
}

export function ConnectorMarketDialogs() {
  const { i18n, market, onError, onTryConnector, uiState, view } =
    useConnectorMarketServices();
  const dialog = useSnapshot(view.dataStore).dialog;
  const marketSnapshot = useSnapshot(market.dataStore);
  const [showSuccessToast, setShowSuccessToast] = useState<
    "authorize" | "install" | null
  >(null);
  const [trackedUninstalls, setTrackedUninstalls] = useState<
    TrackedUninstall[]
  >([]);
  const [uninstallSubmitting, setUninstallSubmitting] = useState(false);
  const [uninstallSuccessName, setUninstallSuccessName] = useState<
    string | null
  >(null);

  useEffect(() => {
    if (trackedUninstalls.length === 0) {
      return;
    }
    const completed: TrackedUninstall[] = [];
    const failed: TrackedUninstall[] = [];
    for (const tracked of trackedUninstalls) {
      const operation =
        marketSnapshot.operationsByConnectorKey[tracked.connectorKey];
      if (!operation || operation.operationId !== tracked.operationId) {
        continue;
      }
      if (operation.state === "completed") {
        completed.push(tracked);
      } else if (operation.state === "failed") {
        failed.push(tracked);
      }
    }
    const settledIds = new Set(
      [...completed, ...failed].map((tracked) => tracked.operationId)
    );
    if (settledIds.size === 0) {
      return;
    }
    setTrackedUninstalls((current) =>
      current.filter((tracked) => !settledIds.has(tracked.operationId))
    );
    if (completed.length > 0) {
      setUninstallSuccessName(completed.at(-1)?.displayName ?? null);
    }
    if (failed.length > 0) {
      onError?.(i18n.t("connectorUninstallFailed"));
    }
  }, [
    i18n,
    marketSnapshot.operationsByConnectorKey,
    onError,
    trackedUninstalls
  ]);

  if (!dialog && !showSuccessToast && !uninstallSuccessName) {
    return null;
  }

  // Hide management dialog when showing success toast
  const shouldHideDialog =
    dialog?.kind === "management" &&
    Boolean(showSuccessToast || uninstallSuccessName);

  return (
    <>
      {dialog?.kind === "uninstall_confirmation" ? (
        <ConfirmationDialog
          cancelLabel={i18n.t("cancel")}
          confirmBusy={uninstallSubmitting}
          confirmLabel={
            uninstallSubmitting
              ? i18n.t("actionUninstalling")
              : i18n.t("actionUninstall")
          }
          description={i18n.t("dialogUninstallDescription")}
          open
          title={i18n.t("dialogUninstallTitle", {
            name: dialog.displayName
          })}
          tone="destructive"
          onConfirm={() => {
            if (uninstallSubmitting) {
              return;
            }
            setUninstallSubmitting(true);
            void market
              .uninstall(dialog.connectorKey)
              .then(() => {
                const operation =
                  market.dataStore.operationsByConnectorKey[
                    dialog.connectorKey
                  ];
                if (operation?.kind === "uninstall") {
                  setTrackedUninstalls((current) =>
                    current.some(
                      (tracked) => tracked.operationId === operation.operationId
                    )
                      ? current
                      : [
                          ...current,
                          {
                            connectorKey: dialog.connectorKey,
                            displayName: dialog.displayName,
                            operationId: operation.operationId
                          }
                        ]
                  );
                }
                uiState.closeDialog();
              })
              .catch(() => {
                onError?.(i18n.t("connectorUninstallFailed"));
              })
              .finally(() => setUninstallSubmitting(false));
          }}
          onOpenChange={(open) => {
            if (!open && !uninstallSubmitting) {
              uiState.closeDialog();
            }
          }}
        />
      ) : dialog && !shouldHideDialog ? (
        <Dialog
          open
          onOpenChange={(open) =>
            !open &&
            !(dialog.kind === "installation" && dialog.installing) &&
            uiState.closeDialog()
          }
        >
          {dialog.kind === "installation" ? (
            <ConnectorInstallationDialog
              description={dialog.description}
              displayName={dialog.displayName}
              i18n={i18n}
              installing={dialog.installing}
              updating={dialog.updating}
              onClose={() => uiState.closeDialog()}
              onInstall={() => {
                setShowSuccessToast(null);
                void market
                  .install(dialog.connectorKey)
                  .then(() => {
                    setShowSuccessToast("install");
                    uiState.closeDialog();
                  })
                  .catch(() => {
                    onError?.(
                      i18n.t(
                        dialog.updating
                          ? "connectorUpdateFailed"
                          : "connectorInstallFailed"
                      )
                    );
                  });
              }}
            />
          ) : dialog.kind === "authorization" ? (
            <ConnectorAuthorizationDialog
              authorizationKind={dialog.authorizationKind}
              authorizing={dialog.authorizing}
              displayName={dialog.displayName}
              iconUrl={dialog.iconUrl}
              i18n={i18n}
              pending={dialog.pending}
              permissions={dialog.permissions}
              onAuthorize={(secret) => {
                setShowSuccessToast(null);
                return market
                  .beginAuthorization(dialog.connectorKey, secret)
                  .then(() => {
                    setShowSuccessToast("authorize");
                    uiState.closeDialog();
                  })
                  .catch(() => {
                    onError?.(i18n.t("connectorAuthorizationFailed"));
                  });
              }}
              onClose={() => uiState.closeDialog()}
            />
          ) : dialog.kind === "management" ? (
            <ConnectorManagementDialog
              canDisconnectAuthorization={dialog.canAuthorize}
              description={dialog.description}
              displayName={dialog.displayName}
              iconUrl={dialog.iconUrl}
              i18n={i18n}
              onDisconnect={() => {
                void market
                  .disconnectAuthorization(dialog.connectorKey)
                  .then(() => uiState.closeDialog())
                  .catch(() => {
                    onError?.(i18n.t("connectorDisconnectFailed"));
                  });
              }}
              onRequestUninstall={() =>
                uiState.requestUninstall(dialog.connectorKey)
              }
              onTry={() => {
                uiState.closeDialog();
                onTryConnector?.(dialog.connectorKey);
              }}
            />
          ) : (
            <ConnectorBlockedDialog
              displayName={dialog.displayName}
              iconUrl={dialog.iconUrl}
              i18n={i18n}
              reason={dialog.reason}
              onClose={() => uiState.closeDialog()}
            />
          )}
        </Dialog>
      ) : null}
      {showSuccessToast ? (
        <ToastProvider>
          <ToastRoot
            open
            variant="success"
            onOpenChange={(open) => !open && setShowSuccessToast(null)}
          >
            <ToastTitle>
              {i18n.t(
                showSuccessToast === "install"
                  ? "actionInstallSuccess"
                  : "actionAuthorizeSuccess"
              )}
            </ToastTitle>
          </ToastRoot>
          <ToastViewport />
        </ToastProvider>
      ) : null}
      {uninstallSuccessName ? (
        <ToastProvider>
          <ToastRoot
            open
            variant="success"
            onOpenChange={(open) => !open && setUninstallSuccessName(null)}
          >
            <ToastTitle>
              {i18n.t("connectorUninstallSuccess", {
                name: uninstallSuccessName
              })}
            </ToastTitle>
          </ToastRoot>
          <ToastViewport />
        </ToastProvider>
      ) : null}
    </>
  );
}
