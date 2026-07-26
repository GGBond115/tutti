import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus,
  AgentActivitySession,
  AgentActivitySessionSettings,
  AgentActivitySnapshot
} from "@tutti-os/agent-activity-core";
import type { AgentConversationVM } from "@tutti-os/agent-gui/conversation-projection";
import type { AgentComposerSettingsSupport } from "@tutti-os/agent-gui/composer-projection";
import type { AgentTarget } from "@tutti-os/client-tuttid-ts";
import type { WorkspaceConversationRailSection } from "./workspaceConversationRailProjection";

export interface WorkspaceActivitySnapshot {
  activity: AgentActivitySnapshot;
  ambiguousSubmission: boolean;
  composerOptions: AgentActivityComposerOptions | null;
  composerOptionsLoadStatus: AgentActivityComposerOptionsLoadStatus | null;
  composerSettings: AgentActivitySessionSettings;
  composerSettingsSupport: AgentComposerSettingsSupport;
  conversation: AgentConversationVM | null;
  creating: boolean;
  draft: string;
  errorCode: "request_failed" | null;
  loading: boolean;
  pinningSessionIds: readonly string[];
  railErrorCode: "request_failed" | null;
  railSections: readonly WorkspaceConversationRailSection[];
  railStatus: "idle" | "loading" | "ready";
  selectedAgentSessionId: string | null;
  selectedAgentTargetId: string | null;
  selectedSession: AgentActivitySession | null;
  sending: boolean;
  targets: readonly AgentTarget[];
}
