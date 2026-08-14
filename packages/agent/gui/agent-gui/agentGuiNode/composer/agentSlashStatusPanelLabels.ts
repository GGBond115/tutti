import type { AgentSlashStatusPanelLabels } from "../AgentSlashStatusPanel";
import type { AgentComposerProps } from "./AgentComposer.types";

export function agentSlashStatusPanelLabels(
  labels: AgentComposerProps["labels"]
): AgentSlashStatusPanelLabels {
  return {
    slashStatusTitle: labels.slashStatusTitle,
    slashStatusSession: labels.slashStatusSession,
    slashStatusBaseUrl: labels.slashStatusBaseUrl,
    slashStatusCwd: labels.slashStatusCwd,
    slashStatusExecutionLocation: labels.slashStatusExecutionLocation,
    slashStatusExecutionLocal: labels.slashStatusExecutionLocal,
    slashStatusExecutionCloud: labels.slashStatusExecutionCloud,
    slashStatusExecutionShared: labels.slashStatusExecutionShared,
    slashStatusContext: labels.slashStatusContext,
    slashStatusLimits: labels.slashStatusLimits,
    slashStatusClose: labels.slashStatusClose,
    slashStatusContextValue: labels.slashStatusContextValue,
    slashStatusContextUnavailable: labels.slashStatusContextUnavailable,
    slashStatusLimitsUnavailable: labels.slashStatusLimitsUnavailable,
    slashStatusEmptyValue: labels.slashStatusEmptyValue,
    slashStatusUsageJustUpdated: labels.slashStatusUsageJustUpdated,
    slashStatusUsageMinutesAgo: labels.slashStatusUsageMinutesAgo,
    slashStatusUsageHoursAgo: labels.slashStatusUsageHoursAgo,
    slashStatusUsageUpdating: labels.slashStatusUsageUpdating,
    slashStatusUsageRefreshFailed: labels.slashStatusUsageRefreshFailed,
    slashStatusUsageRefreshAria: labels.slashStatusUsageRefreshAria
  };
}
