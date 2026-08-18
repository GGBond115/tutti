import {
  Badge,
  Button,
  Card,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Spinner,
  StatusDot
} from "@tutti-os/ui-system/components";
import { MoreHorizontalIcon, UninstallIcon } from "@tutti-os/ui-system/icons";
import { useState } from "react";

import type { ConnectorMarketI18nRuntime } from "../../../i18n/connectorMarketI18n.ts";
import type { ConnectorRendererCardView as ConnectorCardView } from "../../connectorRendererSurface.ts";
import { useConnectorMarketServices } from "../ConnectorMarketServicesContext.tsx";
import {
  connectorCardActionStartsInstallation,
  connectorCardShowsInstallationProgress
} from "./connectorCardAction.ts";
import { ConnectorIcon } from "./ConnectorIcon.tsx";

export function ConnectorCard({ connectorKey }: { connectorKey: string }) {
  const { i18n, model, onError, snapshot } = useConnectorMarketServices();
  const [disconnecting, setDisconnecting] = useState(false);
  const [restartingRuntime, setRestartingRuntime] = useState(false);
  const card = snapshot.view.cardsByKey[connectorKey];
  if (!card) {
    return null;
  }

  const actionLabel = resolveActionLabel(card, i18n.t);
  const status = resolveStatus(card.status, i18n.t);
  const handleAction = () => {
    if (card.action === "disconnect") {
      if (disconnecting) {
        return;
      }
      setDisconnecting(true);
      void model.commands
        .disconnectAuthorization(connectorKey)
        .catch(() => onError?.(i18n.t("connectorDisconnectFailed")))
        .finally(() => setDisconnecting(false));
      return;
    }
    if (card.action === "cancel") {
      void model.commands
        .cancelAuthorization(connectorKey)
        .catch(() => onError?.(i18n.t("connectorAuthorizationFailed")));
      return;
    }
    if (card.action === "restart_runtime") {
      if (restartingRuntime) {
        return;
      }
      setRestartingRuntime(true);
      void model.commands
        .restartRuntime(connectorKey)
        .catch(() => {
          onError?.(i18n.t("connectorRuntimeRestartFailed"));
        })
        .finally(() => setRestartingRuntime(false));
      return;
    }
    if (connectorCardActionStartsInstallation(card.action)) {
      void model.commands.install(connectorKey).catch(() => {
        onError?.(
          i18n.t(
            card.action === "update"
              ? "connectorUpdateFailed"
              : "connectorInstallFailed"
          )
        );
      });
      return;
    }
    if (card.action !== "unavailable") {
      model.commands.openConnector(connectorKey);
    }
  };

  return (
    <Card
      className="min-h-[132px] justify-between gap-3 bg-[var(--background-panel)] p-4 py-4"
      data-testid={`connector-card-${connectorKey}`}
    >
      <div className="flex min-w-0 items-start gap-3">
        <ConnectorIcon displayName={card.displayName} iconUrl={card.iconUrl} />
        <div className="min-w-0 flex-1">
          <h4 className="m-0 truncate text-[14px] font-semibold leading-5 text-[var(--text-primary)]">
            {card.displayName}
          </h4>
          <p className="mt-0.5 line-clamp-2 text-[12px] leading-[1.45] text-[var(--text-secondary)]">
            {card.description}
          </p>
          {card.implementationTags.length > 0 ? (
            <div className="mt-2 flex flex-wrap gap-1">
              {card.implementationTags.map((tag) => (
                <Badge key={tag} size="sm" variant="outline">
                  {tag}
                </Badge>
              ))}
            </div>
          ) : null}
        </div>
        {card.canUninstall ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={i18n.t("actionMore")}
                disabled={card.action === "unavailable"}
                size="icon-xs"
                title={i18n.t("actionMore")}
                type="button"
                variant="ghost"
              >
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              className="min-w-[160px]"
              collisionPadding={12}
              style={{ zIndex: "var(--z-panel-popover)" }}
            >
              <DropdownMenuItem
                variant="destructive"
                onSelect={() => model.commands.requestUninstall(connectorKey)}
              >
                <UninstallIcon />
                {i18n.t("actionUninstall")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2 text-[12px] text-[var(--text-secondary)]">
          <StatusDot
            pulse={["connecting", "loading"].includes(card.status)}
            size="xs"
            tone={status.tone}
          />
          <span className="truncate">
            {connectorCardShowsInstallationProgress(card.reasonCode)
              ? i18n.t("actionInstalling")
              : card.operationStage && card.operationStage !== "completed"
                ? operationStageLabel(card.operationStage, i18n.t)
                : status.label}
          </span>
        </div>
        {card.action !== "unavailable" ? (
          <Button
            disabled={disconnecting || restartingRuntime}
            size="sm"
            type="button"
            variant={
              card.action === "disconnect"
                ? "destructive-secondary"
                : card.action === "install" || card.action === "update"
                  ? "outline"
                  : "secondary"
            }
            onClick={handleAction}
          >
            {disconnecting || restartingRuntime ? <Spinner size={14} /> : null}
            {disconnecting
              ? i18n.t("actionDisconnecting")
              : restartingRuntime
                ? i18n.t("actionRestartingRuntime")
                : actionLabel}
          </Button>
        ) : null}
      </div>
    </Card>
  );
}

function resolveActionLabel(
  card: Readonly<Pick<ConnectorCardView, "action" | "status">>,
  t: ConnectorMarketI18nRuntime["t"]
): string {
  switch (card.action) {
    case "install":
      return t(card.status === "failed" ? "actionReinstall" : "actionInstall");
    case "update":
      return t("actionUpdate");
    case "authorize":
      return t(
        card.status === "failed" ? "actionReauthorize" : "actionAuthorize"
      );
    case "restart_runtime":
      return t("actionRestartRuntime");
    case "cancel":
      return t("cancel");
    case "disconnect":
      return t("actionDisconnect");
    case "unavailable":
      return "";
  }
}

function resolveStatus(
  status: ConnectorCardView["status"],
  t: ConnectorMarketI18nRuntime["t"]
): {
  label: string;
  tone: "amber" | "blue" | "green" | "neutral" | "red";
} {
  switch (status) {
    case "connected":
      return { label: t("connectedStatus"), tone: "green" };
    case "authorization_required":
      return { label: t("statusAuthorizationRequired"), tone: "amber" };
    case "connecting":
      return { label: t("statusConnecting"), tone: "blue" };
    case "loading":
      return { label: t("loading"), tone: "neutral" };
    case "degraded":
      return { label: t("statusDegraded"), tone: "amber" };
    case "disabled":
      return { label: t("statusDisabled"), tone: "neutral" };
    case "unsupported":
      return { label: t("statusUnsupported"), tone: "neutral" };
    case "failed":
      return { label: t("operationFailed"), tone: "red" };
    case "unavailable":
      return { label: t("statusUnavailable"), tone: "red" };
    case "setup_required":
      return { label: t("statusNotInstalled"), tone: "neutral" };
    default:
      return { label: t("statusUnsupported"), tone: "neutral" };
  }
}

function operationStageLabel(
  stage: string,
  t: ConnectorMarketI18nRuntime["t"]
): string {
  const keys = {
    accepted: "operationAccepted",
    activating: "operationActivating",
    authorizing: "operationAuthorizing",
    completed: "operationCompleted",
    deactivating: "operationDeactivating",
    disconnecting: "operationDisconnecting",
    installing: "actionInstalling",
    installed: "operationPrepared",
    runtime_pending: "operationActivating",
    removing: "actionUninstalling",
    downloading: "operationDownloading",
    failed: "operationFailed",
    prepared: "operationPrepared",
    refreshing: "operationRefreshing"
  } as const;
  return t(keys[stage as keyof typeof keys] ?? "operationAccepted");
}
