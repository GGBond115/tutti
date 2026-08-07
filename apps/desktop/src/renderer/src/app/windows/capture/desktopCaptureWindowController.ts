import type {
  DesktopCaptureApi,
  DesktopCaptureAttachment,
  DesktopCaptureSelectionInput,
  DesktopCaptureState
} from "../../../../../shared/contracts/capture.ts";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";

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
}

const initialSnapshot: DesktopCaptureWindowSnapshot = {
  attachment: null,
  agentTargetId: "",
  capture: null,
  content: [],
  failed: false,
  selection: null,
  stage: "loading",
  submitting: false
};

export class DesktopCaptureWindowController {
  private readonly api: DesktopCaptureApi;
  private dragStart: { x: number; y: number } | null = null;
  private initializePromise: Promise<void> | null = null;
  private readonly listeners = new Set<() => void>();
  private snapshot = initialSnapshot;

  constructor(api: DesktopCaptureApi) {
    this.api = api;
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
          agentTargetId: capture.agents[0]?.id ?? "",
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
        agentTargetId: result.agents[0]?.id ?? "",
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
    this.update({ agentTargetId });
  }

  setContent(content: AgentPromptContentBlock[]): void {
    this.update({ content });
  }

  insertPrompt(prompt: string): void {
    const normalizedPrompt = prompt.trim();
    if (!normalizedPrompt) {
      return;
    }
    const content = this.snapshot.content;
    const currentText = content
      .filter((block) => block.type === "text")
      .map((block) => block.text?.trim() ?? "")
      .filter(Boolean)
      .join("\n");
    if (currentText.includes(normalizedPrompt)) {
      return;
    }
    const nextText = currentText
      ? `${currentText}\n\n${normalizedPrompt}`
      : normalizedPrompt;
    this.update({
      content: [
        { text: nextText, type: "text" },
        ...content.filter((block) => block.type !== "text")
      ]
    });
  }

  async submit(
    content: AgentPromptContentBlock[] = this.snapshot.content,
    displayPrompt?: string
  ): Promise<void> {
    const { agentTargetId, attachment, submitting } = this.snapshot;
    if (!attachment || !agentTargetId || submitting || content.length === 0) {
      return;
    }
    this.update({ content, failed: false, submitting: true });
    try {
      await this.api.submit({
        agentTargetId,
        content,
        ...(displayPrompt?.trim() ? { displayPrompt } : {})
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
