import type {
  DesktopCaptureApi,
  DesktopCaptureAttachment,
  DesktopCaptureComposerSettings,
  DesktopCaptureSelectionInput,
  DesktopCaptureState
} from "../../../../../shared/contracts/capture.ts";
import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import type { TuttiExternalAtRichTextBridge } from "@tutti-os/workspace-external-core/rich-text";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";
import type { WorkspaceUserProjectApi } from "@tutti-os/workspace-user-project/contracts";
import type { DesktopCaptureAgentTargetPreference } from "./desktopCaptureAgentTargetPreference.ts";
import type { DesktopCaptureProjectPreference } from "./desktopCaptureProjectPreference.ts";

export type DesktopCaptureStage = "loading" | "selecting" | "composing";

export interface DesktopCaptureWindowSnapshot {
  attachment: DesktopCaptureAttachment | null;
  agentTargetId: string;
  capture: DesktopCaptureState | null;
  composerSettings: DesktopCaptureComposerSettings;
  content: AgentPromptContentBlock[];
  failed: boolean;
  projectPath: string | null;
  refreshingAgentOptions: boolean;
  selection: DesktopCaptureSelectionInput | null;
  stage: DesktopCaptureStage;
  submitting: boolean;
  trackWithTask: boolean;
}

const initialSnapshot: DesktopCaptureWindowSnapshot = {
  attachment: null,
  agentTargetId: "",
  capture: null,
  composerSettings: {},
  content: [],
  failed: false,
  projectPath: null,
  refreshingAgentOptions: false,
  selection: null,
  stage: "loading",
  submitting: false,
  trackWithTask: false
};

export class DesktopCaptureWindowController {
  private readonly api: DesktopCaptureApi;
  private readonly agentTargetPreference: DesktopCaptureAgentTargetPreference | null;
  private readonly projectPreference: DesktopCaptureProjectPreference | null;
  private dragStart: { x: number; y: number } | null = null;
  private composerOptionsRequestRevision = 0;
  private initializePromise: Promise<void> | null = null;
  private readonly listeners = new Set<() => void>();
  private snapshot = initialSnapshot;
  readonly mentionBridge: TuttiExternalAtRichTextBridge;
  readonly userProjectApi: WorkspaceUserProjectApi;

  constructor(
    api: DesktopCaptureApi,
    agentTargetPreference: DesktopCaptureAgentTargetPreference | null = null,
    projectPreference: DesktopCaptureProjectPreference | null = null
  ) {
    this.api = api;
    this.agentTargetPreference = agentTargetPreference;
    this.projectPreference = projectPreference;
    this.mentionBridge = {
      at: {
        query: (input) => this.api.queryMentions(input),
        queryDirectory: (input) => this.api.queryMentionDirectory(input),
        resolve: (input) => this.api.resolveMention(input)
      }
    };
    this.userProjectApi = {
      list: () => this.api.userProjects.list(),
      prepareSelection: (input) =>
        this.api.userProjects.prepareSelection(input),
      selectDirectory: () => this.api.selectProjectDirectory(),
      use: (input) => this.api.userProjects.use(input)
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
          projectPath:
            this.projectPreference?.read(capture.workspaceId) ?? null,
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
        composerSettings: {},
        stage: "composing"
      });
      if (this.snapshot.agentTargetId) {
        await this.refreshComposerOptions(
          this.snapshot.agentTargetId,
          this.snapshot.projectPath
        );
      }
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
    this.update({ agentTargetId, composerSettings: {} });
    void this.refreshComposerOptions(
      agentTargetId,
      this.snapshot.projectPath,
      {}
    );
  }

  setContent(content: AgentPromptContentBlock[]): void {
    this.update({ content });
  }

  setTrackWithTask(trackWithTask: boolean): void {
    this.update({ trackWithTask });
  }

  setComposerSettings(patch: DesktopCaptureComposerSettings): void {
    if (this.snapshot.submitting) {
      return;
    }
    const composerSettings = { ...this.snapshot.composerSettings, ...patch };
    this.update({ composerSettings });
    if (this.snapshot.agentTargetId) {
      void this.refreshComposerOptions(
        this.snapshot.agentTargetId,
        this.snapshot.projectPath,
        composerSettings
      );
    }
  }

  selectFiles(): Promise<readonly WorkspaceFileReference[]> {
    return this.api.selectFiles();
  }

  readonly setProjectPath = async (
    projectPath: string | null
  ): Promise<void> => {
    const capture = this.snapshot.capture;
    const normalizedProjectPath = projectPath?.trim() || null;
    if (!capture) {
      return;
    }
    this.projectPreference?.write(capture.workspaceId, normalizedProjectPath);
    this.update({ projectPath: normalizedProjectPath });
    if (this.snapshot.agentTargetId) {
      await this.refreshComposerOptions(
        this.snapshot.agentTargetId,
        normalizedProjectPath,
        this.snapshot.composerSettings
      );
    }
  };

  async submit(
    agentTargetId: string,
    content: AgentPromptContentBlock[] = this.snapshot.content,
    displayPrompt?: string,
    taskInstruction?: string
  ): Promise<void> {
    const {
      agentTargetId: selectedAgentTargetId,
      attachment,
      composerSettings,
      projectPath,
      refreshingAgentOptions,
      submitting,
      trackWithTask
    } = this.snapshot;
    if (
      !attachment ||
      !agentTargetId ||
      agentTargetId !== selectedAgentTargetId ||
      !this.snapshot.capture?.agents.some(
        (agent) => agent.id === agentTargetId && agent.capabilities.imageInput
      ) ||
      refreshingAgentOptions ||
      submitting ||
      content.length === 0
    ) {
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
        ...(projectPath ? { cwd: projectPath } : {}),
        ...(visiblePrompt ? { displayPrompt: visiblePrompt } : {}),
        ...(Object.keys(composerSettings).length > 0
          ? { settings: composerSettings }
          : {})
      });
    } catch {
      this.update({ failed: true, submitting: false });
    }
  }

  private async refreshComposerOptions(
    agentTargetId: string,
    projectPath: string | null,
    settings: DesktopCaptureComposerSettings = this.snapshot.composerSettings
  ): Promise<void> {
    const revision = ++this.composerOptionsRequestRevision;
    this.update({ failed: false, refreshingAgentOptions: true });
    try {
      const options = await this.api.getComposerOptions({
        agentTargetId,
        cwd: projectPath,
        settings: Object.keys(settings).length > 0 ? settings : null
      });
      if (revision !== this.composerOptionsRequestRevision) {
        return;
      }
      const capture = this.snapshot.capture;
      if (!capture) {
        return;
      }
      const refreshedAgent = options.agents.find(
        (agent) => agent.id === agentTargetId
      );
      if (!refreshedAgent) {
        throw new Error("Selected screenshot Agent target is unavailable");
      }
      this.update({
        capture: {
          ...capture,
          agents: capture.agents.map((agent) =>
            agent.id === agentTargetId ? refreshedAgent : agent
          )
        },
        failed: false,
        refreshingAgentOptions: false
      });
    } catch {
      if (revision !== this.composerOptionsRequestRevision) {
        return;
      }
      const capture = this.snapshot.capture;
      this.update({
        capture: capture
          ? {
              ...capture,
              agents: capture.agents.map((agent) =>
                agent.id === agentTargetId
                  ? {
                      ...agent,
                      capabilities: {
                        ...agent.capabilities,
                        imageInput: false
                      },
                      composerOptions: null
                    }
                  : agent
              )
            }
          : capture,
        failed: true,
        refreshingAgentOptions: false
      });
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
