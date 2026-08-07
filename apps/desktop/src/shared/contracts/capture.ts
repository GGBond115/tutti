import type { DesktopLocale } from "../i18n/core/locale.ts";
import type { DesktopThemeAppearance } from "../theme/core.ts";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";

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
  displayPrompt?: string;
}

export interface DesktopCaptureSubmitResult {
  agentSessionId: string;
}

export interface DesktopCaptureApi {
  cancel(): Promise<void>;
  getState(): Promise<DesktopCaptureState>;
  select(
    input: DesktopCaptureSelectionInput
  ): Promise<DesktopCaptureSelectionResult>;
  submit(input: DesktopCaptureSubmitInput): Promise<DesktopCaptureSubmitResult>;
}
