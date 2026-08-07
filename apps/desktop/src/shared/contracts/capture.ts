import type { DesktopLocale } from "../i18n/core/locale.ts";
import type { DesktopThemeAppearance } from "../theme/core.ts";

export interface DesktopCaptureTopicOption {
  id: string;
  isDefault: boolean;
  title: string;
}

export interface DesktopCaptureAgentOption {
  id: string;
  name: string;
}

export interface DesktopCaptureState {
  agents: DesktopCaptureAgentOption[];
  defaultTopicId: string;
  displayHeight: number;
  displayWidth: number;
  locale: DesktopLocale;
  screenshotDataUrl: string;
  themeAppearance: DesktopThemeAppearance;
  topics: DesktopCaptureTopicOption[];
  workspaceId: string;
}

export interface DesktopCaptureComposerOptions {
  agents: DesktopCaptureAgentOption[];
  defaultTopicId: string;
  topics: DesktopCaptureTopicOption[];
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
  action: "create" | "create-and-run";
  agentTargetId?: string;
  note: string;
  topicId: string;
}

export interface DesktopCaptureSubmitResult {
  issueId: string;
  runStarted: boolean;
}

export interface DesktopCaptureApi {
  cancel(): Promise<void>;
  getState(): Promise<DesktopCaptureState>;
  select(
    input: DesktopCaptureSelectionInput
  ): Promise<DesktopCaptureSelectionResult>;
  submit(input: DesktopCaptureSubmitInput): Promise<DesktopCaptureSubmitResult>;
}
