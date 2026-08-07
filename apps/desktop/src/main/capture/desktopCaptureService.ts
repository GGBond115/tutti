import { randomUUID } from "node:crypto";
import {
  BrowserWindow,
  app,
  desktopCapturer,
  globalShortcut,
  ipcMain,
  screen,
  type Display,
  type NativeImage
} from "electron";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopHostPreferencesState } from "../desktopHostPreferences.ts";
import type { DesktopLogger } from "../logging.ts";
import { getDesktopThemeState } from "../desktopTheme.ts";
import {
  findWorkspaceWindow,
  getWorkspaceWindowWorkspaceID
} from "../windows/workspaceWindow.ts";
import { desktopIpcChannels } from "../../shared/contracts/ipc.ts";
import type {
  DesktopCaptureAttachment,
  DesktopCaptureComposerOptions,
  DesktopCaptureSelectionInput,
  DesktopCaptureState,
  DesktopCaptureSubmitInput,
  DesktopCaptureSubmitResult
} from "../../shared/contracts/capture.ts";
import { registerDesktopIpcHandler } from "../ipc/handle.ts";
import {
  normalizeCaptureSelection,
  resolveCaptureComposerBounds,
  resolveCaptureTitle
} from "./captureGeometry.ts";
import { desktopCaptureAccelerator } from "./captureShortcut.ts";

const captureComposerWidth = 480;
const captureComposerHeight = 500;

type DesktopCaptureComposerOptionsLoad =
  | { error: null; options: DesktopCaptureComposerOptions }
  | { error: Error; options: null };

interface ActiveCapture {
  composerOptions: Promise<DesktopCaptureComposerOptionsLoad>;
  createdIssueId: string | null;
  display: Display;
  image: NativeImage;
  selected: DesktopCaptureAttachment | null;
  state: DesktopCaptureState;
  submission: Promise<DesktopCaptureSubmitResult> | null;
  window: BrowserWindow;
}

export interface DesktopCaptureService {
  dispose(): void;
}

export function createDesktopCaptureService(input: {
  logger: DesktopLogger;
  preferences: Pick<
    DesktopHostPreferencesState,
    "getLocale" | "getThemeSource"
  >;
  preloadPath: string;
  rendererFilePath: string;
  rendererUrl?: string;
  tuttidClient: Pick<
    TuttidClient,
    | "createWorkspaceIssue"
    | "startWorkspaceIssueRun"
    | "listAgentTargets"
    | "listWorkspaceIssueTopics"
  >;
}): DesktopCaptureService {
  let activeCapture: ActiveCapture | null = null;
  let openingCapture = false;
  let lastWorkspaceId = resolveFocusedWorkspaceId();

  const onBrowserWindowFocus = (
    _event: Electron.Event,
    window: BrowserWindow
  ) => {
    const workspaceId = getWorkspaceWindowWorkspaceID(window);
    if (workspaceId) {
      lastWorkspaceId = workspaceId;
    }
  };
  app.on("browser-window-focus", onBrowserWindowFocus);

  const trustedActiveCapture = (
    sender: Electron.WebContents
  ): ActiveCapture => {
    if (
      !activeCapture ||
      activeCapture.window.isDestroyed() ||
      activeCapture.window.webContents !== sender
    ) {
      throw new Error("Screenshot capture window is unavailable");
    }
    return activeCapture;
  };

  registerDesktopIpcHandler(
    desktopIpcChannels.capture.getState,
    (event) => trustedActiveCapture(event.sender).state
  );
  registerDesktopIpcHandler(desktopIpcChannels.capture.cancel, (event) => {
    const capture = trustedActiveCapture(event.sender);
    if (!capture.submission) {
      capture.window.close();
    }
  });
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.select,
    async (event, selection) => {
      const capture = trustedActiveCapture(event.sender);
      const normalized = normalizeCaptureSelection(selection, {
        height: capture.state.displayHeight,
        width: capture.state.displayWidth
      });
      const selected = cropSelection(capture, normalized);
      const loaded = await capture.composerOptions;
      if (loaded.error) {
        input.logger.warn("screenshot composer metadata unavailable", {
          error: loaded.error.message,
          workspaceId: capture.state.workspaceId
        });
        throw loaded.error;
      }
      capture.state = {
        ...capture.state,
        ...loaded.options
      };
      capture.selected = selected;
      presentComposer(capture, normalized);
      return { attachment: selected, ...loaded.options };
    }
  );
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.submit,
    async (event, submission) => {
      const capture = trustedActiveCapture(event.sender);
      capture.submission ??= submitCapture(
        input.tuttidClient,
        capture,
        submission
      );
      try {
        const result = await capture.submission;
        const workspaceId = capture.state.workspaceId;
        if (!capture.window.isDestroyed()) {
          capture.window.close();
        }
        focusWorkspace(workspaceId);
        return result;
      } finally {
        capture.submission = null;
      }
    }
  );

  const openCapture = async (): Promise<void> => {
    if (activeCapture && !activeCapture.window.isDestroyed()) {
      activeCapture.window.focus();
      return;
    }
    if (openingCapture) {
      return;
    }
    const workspaceId = lastWorkspaceId ?? resolveFocusedWorkspaceId();
    if (!workspaceId) {
      input.logger.warn("screenshot capture skipped", {
        reason: "workspace_unavailable"
      });
      return;
    }
    openingCapture = true;
    try {
      activeCapture = await createCaptureWindow(
        input,
        workspaceId,
        (capture) => {
          activeCapture = capture;
          capture.window.once("closed", () => {
            if (activeCapture?.window === capture.window) {
              activeCapture = null;
            }
          });
        }
      );
    } catch (error) {
      if (activeCapture && !activeCapture.window.isDestroyed()) {
        activeCapture.window.close();
      }
      activeCapture = null;
      input.logger.warn("screenshot capture failed", {
        error: error instanceof Error ? error.message : String(error)
      });
    } finally {
      openingCapture = false;
    }
  };

  const registered = globalShortcut.register(desktopCaptureAccelerator, () => {
    input.logger.info("screenshot shortcut activated", {
      accelerator: desktopCaptureAccelerator
    });
    void openCapture();
  });
  if (!registered || !globalShortcut.isRegistered(desktopCaptureAccelerator)) {
    input.logger.warn("screenshot shortcut registration failed", {
      accelerator: desktopCaptureAccelerator
    });
  } else {
    input.logger.info("screenshot shortcut registered", {
      accelerator: desktopCaptureAccelerator
    });
  }

  return {
    dispose() {
      app.removeListener("browser-window-focus", onBrowserWindowFocus);
      globalShortcut.unregister(desktopCaptureAccelerator);
      for (const channel of Object.values(desktopIpcChannels.capture)) {
        ipcMain.removeHandler(channel);
      }
      if (activeCapture && !activeCapture.window.isDestroyed()) {
        activeCapture.window.close();
      }
      activeCapture = null;
    }
  };
}

async function createCaptureWindow(
  input: Parameters<typeof createDesktopCaptureService>[0],
  workspaceId: string,
  activate: (capture: ActiveCapture) => void
): Promise<ActiveCapture> {
  const display = screen.getDisplayNearestPoint(screen.getCursorScreenPoint());
  const thumbnailSize = {
    width: Math.max(1, Math.round(display.size.width * display.scaleFactor)),
    height: Math.max(1, Math.round(display.size.height * display.scaleFactor))
  };
  const composerOptions = loadCaptureComposerOptions(
    input.tuttidClient,
    workspaceId
  ).then<DesktopCaptureComposerOptionsLoad, DesktopCaptureComposerOptionsLoad>(
    (options) => ({ error: null, options }),
    (cause) => ({
      error: cause instanceof Error ? cause : new Error(String(cause)),
      options: null
    })
  );
  const sources = await desktopCapturer.getSources({
    types: ["screen"],
    thumbnailSize
  });
  const source =
    sources.find((candidate) => candidate.display_id === String(display.id)) ??
    sources[0];
  if (!source || source.thumbnail.isEmpty()) {
    throw new Error("Screen capture permission is unavailable");
  }
  const theme = getDesktopThemeState(input.preferences.getThemeSource());
  const captureWindow = new BrowserWindow({
    ...display.bounds,
    alwaysOnTop: true,
    backgroundColor: "#000000",
    frame: false,
    fullscreenable: false,
    hasShadow: false,
    movable: false,
    resizable: false,
    show: false,
    skipTaskbar: true,
    webPreferences: {
      backgroundThrottling: false,
      contextIsolation: true,
      nodeIntegration: false,
      preload: input.preloadPath,
      sandbox: false
    }
  });
  captureWindow.setAlwaysOnTop(true, "screen-saver");
  const state: DesktopCaptureState = {
    agents: [],
    defaultTopicId: "",
    displayHeight: display.bounds.height,
    displayWidth: display.bounds.width,
    locale: input.preferences.getLocale(),
    screenshotDataUrl: source.thumbnail.toDataURL(),
    themeAppearance: theme.appearance,
    topics: [],
    workspaceId
  };
  const capture: ActiveCapture = {
    composerOptions,
    createdIssueId: null,
    display,
    image: source.thumbnail,
    selected: null,
    state,
    submission: null,
    window: captureWindow
  };
  activate(capture);
  if (input.rendererUrl) {
    await captureWindow.loadURL(`${input.rendererUrl}/capture.html`);
  } else {
    await captureWindow.loadFile(input.rendererFilePath);
  }
  captureWindow.show();
  captureWindow.focus();
  return capture;
}

function cropSelection(
  capture: ActiveCapture,
  raw: DesktopCaptureSelectionInput
): DesktopCaptureAttachment {
  const imageSize = capture.image.getSize();
  const scaleX = imageSize.width / capture.state.displayWidth;
  const scaleY = imageSize.height / capture.state.displayHeight;
  const cropped = capture.image.crop({
    x: Math.round(raw.x * scaleX),
    y: Math.round(raw.y * scaleY),
    width: Math.max(1, Math.round(raw.width * scaleX)),
    height: Math.max(1, Math.round(raw.height * scaleY))
  });
  const dataBase64 = cropped.toPNG().toString("base64");
  return {
    dataBase64,
    dataUrl: `data:image/png;base64,${dataBase64}`,
    displayName: `Screenshot-${new Date().toISOString().replaceAll(":", "-")}.png`,
    height: cropped.getSize().height,
    mimeType: "image/png",
    width: cropped.getSize().width
  };
}

async function loadCaptureComposerOptions(
  client: Pick<TuttidClient, "listAgentTargets" | "listWorkspaceIssueTopics">,
  workspaceId: string
): Promise<DesktopCaptureComposerOptions> {
  const [topicResponse, targetResponse] = await Promise.all([
    client.listWorkspaceIssueTopics(workspaceId),
    client.listAgentTargets()
  ]);
  const topics = topicResponse.topics.map((topic) => ({
    id: topic.topicId,
    isDefault: topic.isDefault,
    title: topic.title
  }));
  const defaultTopicId =
    topics.find((topic) => topic.isDefault)?.id ?? topics[0]?.id ?? "";
  if (!defaultTopicId) {
    throw new Error("Screenshot capture requires an Issue topic");
  }
  const agents = targetResponse.targets
    .filter(
      (target) =>
        target.enabled &&
        !["auth_required", "not_installed", "unsupported"].includes(
          target.availability?.status ?? "ready"
        )
    )
    .map((target) => ({ id: target.id, name: target.name }));
  return { agents, defaultTopicId, topics };
}

function presentComposer(
  capture: ActiveCapture,
  selection: DesktopCaptureSelectionInput
): void {
  const bounds = resolveCaptureComposerBounds({
    composerHeight: captureComposerHeight,
    composerWidth: captureComposerWidth,
    displayBounds: capture.display.bounds,
    selection,
    workArea: capture.display.workArea
  });
  capture.window.setOpacity(0);
  capture.window.setResizable(true);
  capture.window.setBounds(bounds);
  capture.window.setResizable(false);
  capture.window.setBackgroundColor("#00000000");
  capture.window.setOpacity(1);
}

async function submitCapture(
  client: Pick<TuttidClient, "createWorkspaceIssue" | "startWorkspaceIssueRun">,
  capture: ActiveCapture,
  input: DesktopCaptureSubmitInput
): Promise<DesktopCaptureSubmitResult> {
  const attachment = capture.selected;
  if (!attachment) {
    throw new Error("Screenshot selection is unavailable");
  }
  const topicId = input.topicId.trim();
  if (!capture.state.topics.some((topic) => topic.id === topicId)) {
    throw new Error("Screenshot topic is invalid");
  }
  if (input.action !== "create" && input.action !== "create-and-run") {
    throw new Error("Screenshot action is invalid");
  }
  const agentTargetId = input.agentTargetId?.trim() ?? "";
  if (
    input.action === "create-and-run" &&
    !capture.state.agents.some((agent) => agent.id === agentTargetId)
  ) {
    throw new Error("Screenshot Agent target is invalid");
  }
  const note = input.note.trim();
  const title = resolveCaptureTitle(note, attachment.displayName);
  let issueId = capture.createdIssueId;
  if (!issueId) {
    const issue = await client.createWorkspaceIssue(capture.state.workspaceId, {
      attachments: [
        {
          attachmentId: randomUUID(),
          dataBase64: attachment.dataBase64,
          displayName: attachment.displayName,
          mimeType: attachment.mimeType
        }
      ],
      content: note,
      topicId,
      title
    });
    issueId = issue.issueId;
    // A run request can fail after Issue creation. Keep the durable Issue ID so
    // retrying from the composer starts the run instead of duplicating the task.
    capture.createdIssueId = issueId;
  }
  if (input.action === "create-and-run") {
    await client.startWorkspaceIssueRun(capture.state.workspaceId, issueId, {
      agentTargetId
    });
    return { issueId, runStarted: true };
  }
  return { issueId, runStarted: false };
}

function resolveFocusedWorkspaceId(): string | null {
  const focused = BrowserWindow.getFocusedWindow();
  const focusedWorkspace = focused
    ? getWorkspaceWindowWorkspaceID(focused)
    : null;
  if (focusedWorkspace) {
    return focusedWorkspace;
  }
  for (const window of BrowserWindow.getAllWindows()) {
    const workspaceId = getWorkspaceWindowWorkspaceID(window);
    if (workspaceId) {
      return workspaceId;
    }
  }
  return null;
}

function focusWorkspace(workspaceId: string): void {
  const window =
    findWorkspaceWindow(workspaceId, "workspace") ??
    findWorkspaceWindow(workspaceId, "agent");
  if (!window || window.isDestroyed()) {
    return;
  }
  if (window.isMinimized()) {
    window.restore();
  }
  window.show();
  window.focus();
}
