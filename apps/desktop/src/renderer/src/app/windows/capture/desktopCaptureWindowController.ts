import type {
  DesktopCaptureApi,
  DesktopCaptureAttachment,
  DesktopCaptureSelectionInput,
  DesktopCaptureState
} from "../../../../../shared/contracts/capture.ts";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import type { TuttiExternalAtRichTextBridge } from "@tutti-os/workspace-external-core/rich-text";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";
import type { DesktopCaptureAgentTargetPreference } from "./desktopCaptureAgentTargetPreference.ts";

export type DesktopCaptureStage = "loading" | "selecting" | "composing";

export interface DesktopCaptureWindowSnapshot {
  attachment: DesktopCaptureAttachment | null;
  agentTargetId: string;
  capture: DesktopCaptureState | null;
  content: AgentPromptContentBlock[];
  failed: boolean;
  selection: DesktopCaptureSelectionInput | null;
  stage: DesktopCaptureStage;
  submitting: boolean;
  trackWithTask: boolean;
}

const initialSnapshot: DesktopCaptureWindowSnapshot = {
  attachment: null,
  agentTargetId: "",
  capture: null,
  content: [],
  failed: false,
  selection: null,
  stage: "loading",
  submitting: false,
  trackWithTask: false
};

export class DesktopCaptureWindowController {
  private readonly api: DesktopCaptureApi;
  private readonly agentTargetPreference: DesktopCaptureAgentTargetPreference | null;
  private dragStart: { x: number; y: number } | null = null;
  private initializePromise: Promise<void> | null = null;
  private readonly listeners = new Set<() => void>();
  private snapshot = initialSnapshot;
  readonly mentionBridge: TuttiExternalAtRichTextBridge;

  constructor(
    api: DesktopCaptureApi,
    agentTargetPreference: DesktopCaptureAgentTargetPreference | null = null
  ) {
    this.api = api;
    this.agentTargetPreference = agentTargetPreference;
    this.mentionBridge = {
      at: {
        query: (input) => this.api.queryMentions(input),
        queryDirectory: (input) => this.api.queryMentionDirectory(input),
        resolve: (input) => this.api.resolveMention(input)
      }
    };
  }

  readonly getSnapshot = (): DesktopCaptureWindowSnapshot => this.snapshot;

  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  initialize(): Promise<void> {
    this.initializePromise ??= this.api
      .getState()
      .then((capture) => {
        this.update({
          agentTargetId:
            this.agentTargetPreference?.read(capture.workspaceId) ?? "",
          capture,
          failed: false,
          stage: "selecting"
        });
      })
      .catch(() => this.update({ failed: true }));
    return this.initializePromise;
  }

  beginSelection(point: { x: number; y: number }): void {
    if (this.snapshot.stage !== "selecting") {
      return;
    }
    this.dragStart = point;
    this.update({
      failed: false,
      selection: { ...point, height: 0, width: 0 }
    });
  }

  updateSelection(point: { x: number; y: number }): void {
    const start = this.dragStart;
    if (!start) {
      return;
    }
    this.update({
      selection: {
        x: Math.min(start.x, point.x),
        y: Math.min(start.y, point.y),
        width: Math.abs(point.x - start.x),
        height: Math.abs(point.y - start.y)
      }
    });
  }

  async finishSelection(): Promise<boolean> {
    const selection = this.snapshot.selection;
    this.dragStart = null;
    if (!selection || selection.width < 8 || selection.height < 8) {
      this.update({ selection: null });
      return false;
    }
    try {
      const result = await this.api.select(selection);
      const capture = this.snapshot.capture;
      if (!capture) {
        return false;
      }
      this.update({
        agentTargetId: resolveAvailableAgentTargetId(
          result.agents,
          this.snapshot.agentTargetId
        ),
        attachment: result.attachment,
        capture: {
          ...capture,
          agents: result.agents
        },
        content: [
          { text: "", type: "text" },
          {
            data: result.attachment.dataBase64,
            mimeType: result.attachment.mimeType,
            name: result.attachment.displayName,
            type: "image"
          }
        ],
        failed: false,
        stage: "composing"
      });
      return true;
    } catch {
      this.update({ failed: true });
      return false;
    }
  }

  cancelSelection(): void {
    if (!this.snapshot.submitting) {
      void this.api.cancel();
    }
  }

  setAgentTargetId(agentTargetId: string): void {
    const capture = this.snapshot.capture;
    if (!capture?.agents.some((agent) => agent.id === agentTargetId)) {
      return;
    }
    this.agentTargetPreference?.write(capture.workspaceId, agentTargetId);
    this.update({ agentTargetId });
  }

  setContent(content: AgentPromptContentBlock[]): void {
    this.update({ content });
  }

  setTrackWithTask(trackWithTask: boolean): void {
    this.update({ trackWithTask });
  }

  selectFiles(): Promise<readonly WorkspaceFileReference[]> {
    return this.api.selectFiles();
  }

  async submit(
    content: AgentPromptContentBlock[] = this.snapshot.content,
    displayPrompt?: string,
    taskInstruction?: string
  ): Promise<void> {
    const { agentTargetId, attachment, submitting, trackWithTask } =
      this.snapshot;
    if (!attachment || !agentTargetId || submitting || content.length === 0) {
      return;
    }
    this.update({ content, failed: false, submitting: true });
    try {
      const visiblePrompt =
        displayPrompt?.trim() || capturePromptText(content).trim();
      await this.api.submit({
        agentTargetId,
        content: trackWithTask
          ? prependCapturePromptInstruction(content, taskInstruction)
          : content,
        ...(visiblePrompt ? { displayPrompt: visiblePrompt } : {})
      });
    } catch {
      this.update({ failed: true, submitting: false });
    }
  }

  private update(patch: Partial<DesktopCaptureWindowSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) {
      listener();
    }
  }
}

function resolveAvailableAgentTargetId(
  agents: DesktopCaptureState["agents"],
  preferredAgentTargetId: string
): string {
  return agents.some((agent) => agent.id === preferredAgentTargetId)
    ? preferredAgentTargetId
    : (agents[0]?.id ?? "");
}

export function prependCapturePromptInstruction(
  content: readonly AgentPromptContentBlock[],
  instruction: string | null | undefined
): AgentPromptContentBlock[] {
  const normalizedInstruction = instruction?.trim() ?? "";
  if (!normalizedInstruction) {
    return [...content];
  }
  const textIndex = content.findIndex((block) => block.type === "text");
  if (textIndex < 0) {
    return [{ text: normalizedInstruction, type: "text" }, ...content];
  }
  return content.map((block, index) => {
    if (index !== textIndex || block.type !== "text") {
      return block;
    }
    const text = block.text ?? "";
    return {
      ...block,
      text: text.trim()
        ? `${normalizedInstruction}\n\n${text}`
        : normalizedInstruction
    };
  });
}

function capturePromptText(
  content: readonly AgentPromptContentBlock[]
): string {
  return content
    .filter((block) => block.type === "text")
    .map((block) => block.text?.trim() ?? "")
    .filter(Boolean)
    .join("\n");
}
