import type {
  DesktopCaptureApi,
  DesktopCaptureAttachment,
  DesktopCaptureSelectionInput,
  DesktopCaptureState
} from "../../../../../shared/contracts/capture.ts";

export type DesktopCaptureStage = "loading" | "selecting" | "composing";

export interface DesktopCaptureWindowSnapshot {
  attachment: DesktopCaptureAttachment | null;
  agentTargetId: string;
  capture: DesktopCaptureState | null;
  failed: boolean;
  note: string;
  selection: DesktopCaptureSelectionInput | null;
  stage: DesktopCaptureStage;
  submitting: boolean;
  topicId: string;
}

const initialSnapshot: DesktopCaptureWindowSnapshot = {
  attachment: null,
  agentTargetId: "",
  capture: null,
  failed: false,
  note: "",
  selection: null,
  stage: "loading",
  submitting: false,
  topicId: ""
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
          stage: "selecting",
          topicId: capture.defaultTopicId
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
          agents: result.agents,
          defaultTopicId: result.defaultTopicId,
          topics: result.topics
        },
        failed: false,
        stage: "composing",
        topicId: result.defaultTopicId
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

  setNote(note: string): void {
    this.update({ note });
  }

  setTopicId(topicId: string): void {
    this.update({ topicId });
  }

  async submit(action: "create" | "create-and-run"): Promise<void> {
    const { agentTargetId, attachment, note, submitting, topicId } =
      this.snapshot;
    if (
      !attachment ||
      !topicId ||
      submitting ||
      (action === "create-and-run" && !agentTargetId)
    ) {
      return;
    }
    this.update({ failed: false, submitting: true });
    try {
      await this.api.submit({
        action,
        ...(action === "create-and-run" ? { agentTargetId } : {}),
        note,
        topicId
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
