import { useMemo } from "react";
import type {
  AgentComposerSlashStatus,
  AgentComposerSlashStatusLimit
} from "../AgentComposer";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";
import {
  resolveAgentGUISlashStatusExecutionLocation,
  resolveSlashStatus,
  useStableSlashStatus
} from "./agentGUIDetailModelHelpers";

export function useAgentGUIProjectedSlashStatus(input: {
  slashStatusLimits: readonly AgentComposerSlashStatusLimit[];
  slashStatusLimitsLoading: boolean;
  slashStatusLimitsUnavailable: boolean;
  slashStatusOverride?: AgentComposerSlashStatus | null;
  viewModel: AgentGUINodeViewModel;
}): AgentComposerSlashStatus {
  const rawSlashStatus = useMemo(() => {
    const baseSlashStatus =
      input.slashStatusOverride ??
      resolveSlashStatus({
        rawState: input.viewModel.interaction.sessionChrome.rawState,
        limits: input.slashStatusLimits,
        limitsLoading: input.slashStatusLimitsLoading,
        limitsUnavailable: input.slashStatusLimitsUnavailable,
        usage: input.viewModel.detail.usage
      });
    const activeConversation = input.viewModel.rail.activeConversation;
    if (!activeConversation) return baseSlashStatus;
    return {
      ...baseSlashStatus,
      cwd: activeConversation.cwd.trim() || null,
      executionLocation: resolveAgentGUISlashStatusExecutionLocation(
        input.viewModel.rail.selectedAgentTarget
      )
    };
  }, [
    input.slashStatusLimits,
    input.slashStatusLimitsLoading,
    input.slashStatusLimitsUnavailable,
    input.slashStatusOverride,
    input.viewModel.detail.usage,
    input.viewModel.interaction.sessionChrome.rawState,
    input.viewModel.rail.activeConversation,
    input.viewModel.rail.selectedAgentTarget
  ]);
  return useStableSlashStatus(rawSlashStatus);
}
