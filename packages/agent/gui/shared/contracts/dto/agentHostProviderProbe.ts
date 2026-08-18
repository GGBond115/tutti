import type { AgentHostToolchainApplySummary } from "./agentHostWorkspace";

export interface AgentHostCapabilitiesResult {
  desktopMode: boolean;
  mockAuth: boolean;
  roomListMode: string;
  platforms: string[];
  /** Short hostname from desktopd for device-centric copy (e.g. Manage Agents). */
  hostDisplayName?: string;
}

export interface AgentHostManagedAgentsStateItem {
  toolId: string;
  toolClass: string;
  agentId?: string;
  hostDetected?: boolean;
  hostConfigDetected?: boolean;
  hostVersion?: string;
  targetVersion: string;
  recommendedVersion?: string;
  decisionReason: string;
  fallbackApplied: boolean;
  notes?: string;
}

export interface AgentHostToolchainConfigSyncedAgent {
  agentId: string;
  /** RFC3339 timestamp for when Tutti last synced this agent's host config. */
  syncedAt?: string;
}

export interface AgentHostManagedAgentsState {
  metadataSynced: boolean;
  toolCatalogRevision: string;
  agentProfileRevision: string;
  totalCount: number;
  items: AgentHostManagedAgentsStateItem[];
  /** Agent IDs ready for normal AgentGUI use (installed and authenticated/ready). */
  readyAgentIds: string[];
  /** Agent IDs whose host config has been synced to the VM through Manage Agents. */
  configSyncedAgentIds: string[];
  /** Agent config sync metadata, including when Tutti last copied host config. */
  configSyncedAgents?: AgentHostToolchainConfigSyncedAgent[];
}

export type AgentHostManageAgentActionKind = "sync" | "install" | "uninstall";

export interface AgentHostManageToolchainAgentInput {
  agentId: string;
  action: AgentHostManageAgentActionKind;
}

export interface AgentHostManageToolchainAgentResult {
  applied: boolean;
  alreadyUninstalled?: boolean;
  toolchainApply?: AgentHostToolchainApplySummary;
  /** Agent IDs ready for normal AgentGUI use after applying this action. */
  readyAgentIds?: string[];
  configSyncedAgentIds?: string[];
  configSyncedAgents?: AgentHostToolchainConfigSyncedAgent[];
}
