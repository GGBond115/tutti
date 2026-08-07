import type { DesktopLocale } from "../i18n/core/locale.ts";
import type { DesktopThemeAppearance } from "../theme/core.ts";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import type {
  TuttiExternalAtQueryDirectoryInput,
  TuttiExternalAtQueryInput,
  TuttiExternalAtQueryResult,
  TuttiExternalAtResolveInput,
  TuttiExternalAtResolveResult
} from "@tutti-os/workspace-external-core/contracts";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";

export interface DesktopCaptureAgentOption {
  description?: string | null;
  id: string;
  iconUrl: string;
  name: string;
  provider: string;
}

export interface DesktopCaptureState {
  agents: DesktopCaptureAgentOption[];
  displayHeight: number;
  displayWidth: number;
  locale: DesktopLocale;
  screenshotDataUrl: string;
  themeAppearance: DesktopThemeAppearance;
  workspaceId: string;
}

export interface DesktopCaptureComposerOptions {
  agents: DesktopCaptureAgentOption[];
}

export interface DesktopCaptureSelectionInput {
  height: number;
  width: number;
  x: number;
  y: number;
}

export interface DesktopCaptureAttachment {
  dataBase64: string;
  dataUrl: string;
  displayName: string;
  height: number;
  mimeType: "image/png";
  width: number;
}

export interface DesktopCaptureSelectionResult extends DesktopCaptureComposerOptions {
  attachment: DesktopCaptureAttachment;
}

export interface DesktopCaptureSubmitInput {
  agentTargetId: string;
  content: AgentPromptContentBlock[];
  cwd?: string;
  displayPrompt?: string;
}

export interface DesktopCaptureSubmitResult {
  agentSessionId: string;
}

export interface DesktopCaptureApi {
  cancel(): Promise<void>;
  getState(): Promise<DesktopCaptureState>;
  queryMentions(
    input: TuttiExternalAtQueryInput
  ): Promise<TuttiExternalAtQueryResult[]>;
  queryMentionDirectory(
    input: TuttiExternalAtQueryDirectoryInput
  ): Promise<TuttiExternalAtQueryResult[]>;
  resolveMention(
    input: TuttiExternalAtResolveInput
  ): Promise<TuttiExternalAtResolveResult | null>;
  select(
    input: DesktopCaptureSelectionInput
  ): Promise<DesktopCaptureSelectionResult>;
  selectFiles(): Promise<WorkspaceFileReference[]>;
  selectProjectDirectory(): Promise<{ path: string } | null>;
  submit(input: DesktopCaptureSubmitInput): Promise<DesktopCaptureSubmitResult>;
}
