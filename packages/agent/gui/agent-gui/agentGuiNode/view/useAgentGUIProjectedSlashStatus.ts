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

export function projectAgentGUISlashStatusExecutionContext(input: {
  activeConversation: AgentGUINodeViewModel["rail"]["activeConversation"];
  baseSlashStatus: AgentComposerSlashStatus;
  enabled: boolean;
  selectedAgentTarget: AgentGUINodeViewModel["rail"]["selectedAgentTarget"];
}): AgentComposerSlashStatus {
  if (!input.enabled || !input.activeConversation) {
    return input.baseSlashStatus;
  }
  return {
    ...input.baseSlashStatus,
    cwd: input.activeConversation.cwd.trim() || null,
    executionLocation: resolveAgentGUISlashStatusExecutionLocation(
      input.selectedAgentTarget
    )
  };
}

export function useAgentGUIProjectedSlashStatus(input: {
  executionContextEnabled: boolean;
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
    return projectAgentGUISlashStatusExecutionContext({
      activeConversation: input.viewModel.rail.activeConversation,
      baseSlashStatus,
      enabled: input.executionContextEnabled,
      selectedAgentTarget: input.viewModel.rail.selectedAgentTarget
    });
  }, [
    input.executionContextEnabled,
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
