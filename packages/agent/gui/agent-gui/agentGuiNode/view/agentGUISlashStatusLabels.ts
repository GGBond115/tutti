import type { TranslateFn } from "../../../i18n/index";
import {
  agentGUIProviderIdentityDisplayName,
  resolveAgentGUIProviderCatalogIdentity
} from "../../../providerIdentityCatalog";
import type { AgentGUIViewLabels } from "./AgentGUINodeView.types";
import { agentGUIUsageStatusLabels } from "./agentGUIUsageStatusLabels";

type AgentGUISlashStatusLabels = Pick<
  AgentGUIViewLabels,
  | "slashStatusTitle"
  | "slashStatusSession"
  | "slashStatusBaseUrl"
  | "slashStatusCwd"
  | "slashStatusExecutionLocation"
  | "slashStatusExecutionLocal"
  | "slashStatusExecutionCloud"
  | "slashStatusExecutionShared"
  | "slashStatusContext"
  | "slashStatusLimits"
  | "slashStatusAccount"
  | "slashStatusProviderAccount"
  | "slashStatusClose"
  | "slashStatusContextValue"
  | "slashStatusContextUnavailable"
  | "slashStatusLimitsUnavailable"
  | "slashStatusEmptyValue"
  | "slashStatusUsageJustUpdated"
  | "slashStatusUsageMinutesAgo"
  | "slashStatusUsageHoursAgo"
  | "slashStatusUsageUpdating"
  | "slashStatusUsageRefreshFailed"
  | "slashStatusUsageRefreshAria"
  | "slashStatusUsageAuthRequired"
  | "slashStatusUsageSessionExpired"
  | "slashStatusUsageSubscriptionRequired"
  | "slashStatusUsageQuotaExhausted"
  | "slashStatusUsageParseFailed"
  | "slashStatusUsageError"
>;

export function agentGUISlashStatusLabels(
  t: TranslateFn
): AgentGUISlashStatusLabels {
  return {
    slashStatusTitle: t("agentHost.agentGui.slashStatusTitle"),
    slashStatusSession: t("agentHost.agentGui.slashStatusSession"),
    slashStatusBaseUrl: t("agentHost.agentGui.slashStatusBaseUrl"),
    slashStatusCwd: t("agentHost.agentGui.slashStatusCwd"),
    slashStatusExecutionLocation: t(
      "agentHost.agentGui.slashStatusExecutionLocation"
    ),
    slashStatusExecutionLocal: t(
      "agentHost.agentGui.slashStatusExecutionLocal"
    ),
    slashStatusExecutionCloud: t(
      "agentHost.agentGui.slashStatusExecutionCloud"
    ),
    slashStatusExecutionShared: t(
      "agentHost.agentGui.slashStatusExecutionShared"
    ),
    slashStatusContext: t("agentHost.agentGui.slashStatusContext"),
    slashStatusLimits: t("agentHost.agentGui.slashStatusLimits"),
    slashStatusAccount: t("agentHost.agentGui.slashStatusAccount"),
    slashStatusProviderAccount: (provider: string) => {
      const identity = resolveAgentGUIProviderCatalogIdentity(provider);
      if (!identity) return null;
      return t("agentHost.agentGui.slashStatusProviderAccount", {
        provider: agentGUIProviderIdentityDisplayName(identity, t)
      });
    },
    slashStatusClose: t("agentHost.agentGui.slashStatusClose"),
    slashStatusContextValue: (input) =>
      t("agentHost.agentGui.slashStatusContextValue", input),
    slashStatusContextUnavailable: t(
      "agentHost.agentGui.slashStatusContextUnavailable"
    ),
    slashStatusLimitsUnavailable: t(
      "agentHost.agentGui.slashStatusLimitsUnavailable"
    ),
    slashStatusEmptyValue: t("agentHost.agentGui.slashStatusEmptyValue"),
    slashStatusUsageJustUpdated: t(
      "agentHost.agentGui.slashStatusUsageJustUpdated"
    ),
    slashStatusUsageMinutesAgo: (count: number) =>
      t("agentHost.agentGui.slashStatusUsageMinutesAgo", { count }),
    slashStatusUsageHoursAgo: (count: number) =>
      t("agentHost.agentGui.slashStatusUsageHoursAgo", { count }),
    slashStatusUsageUpdating: t("agentHost.agentGui.slashStatusUsageUpdating"),
    slashStatusUsageRefreshFailed: t(
      "agentHost.agentGui.slashStatusUsageRefreshFailed"
    ),
    slashStatusUsageRefreshAria: t(
      "agentHost.agentGui.slashStatusUsageRefreshAria"
    ),
    ...agentGUIUsageStatusLabels(t)
  };
}
