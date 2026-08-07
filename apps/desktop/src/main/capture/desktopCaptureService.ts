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
import type {
  TuttiExternalAtQueryResult,
  TuttiExternalAtResolveResult,
  TuttiExternalAgentActivityActivateSessionResult,
  TuttiExternalAgentTargetCatalog,
  TuttiExternalReferenceSelectResult
} from "@tutti-os/workspace-external-core/contracts";
import {
  normalizeTuttiExternalAtQueryDirectoryInput,
  normalizeTuttiExternalAtQueryInput,
  normalizeTuttiExternalAtResolveInput
} from "@tutti-os/workspace-external-core/core";
import type { DesktopHostPreferencesState } from "../desktopHostPreferences.ts";
import type { DesktopLogger } from "../logging.ts";
import { getDesktopThemeState } from "../desktopTheme.ts";
import {
  findWorkspaceWindow,
  getWorkspaceWindowWorkspaceID
} from "../windows/workspaceWindow.ts";
import {
  desktopIpcChannels,
  type DesktopWorkspaceAppExternalRendererRequest,
  type DesktopWorkspaceAppExternalRendererResult
} from "../../shared/contracts/ipc.ts";
import type {
  DesktopCaptureAttachment,
  DesktopCaptureComposerOptions,
  DesktopCaptureSelectionInput,
  DesktopCaptureState,
  DesktopCaptureSubmitInput,
  DesktopCaptureSubmitResult
} from "../../shared/contracts/capture.ts";
import { registerDesktopIpcHandler } from "../ipc/handle.ts";
import { requestWorkspaceOwnerRenderer } from "../ipc/workspaceAppRendererBridge.ts";
import {
  normalizeCaptureSelection,
  resolveCaptureComposerBounds,
  resolveCaptureTitle
} from "./captureGeometry.ts";
import { normalizeCapturePromptContent } from "./captureAgentPrompt.ts";
import { desktopCaptureAccelerator } from "./captureShortcut.ts";

const captureComposerWidth = 620;
const captureComposerHeight = 260;
const captureRendererAppId = "desktop-capture";

type DesktopCaptureComposerOptionsLoad =
  | { error: null; options: DesktopCaptureComposerOptions }
  | { error: Error; options: null };

interface ActiveCapture {
  composerOptions: Promise<DesktopCaptureComposerOptionsLoad>;
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
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.queryMentions,
    async (event, payload) => {
      const capture = trustedActiveCapture(event.sender);
      const query = normalizeTuttiExternalAtQueryInput(payload);
      return requestCaptureWorkspaceOwner<TuttiExternalAtQueryResult[]>(
        capture,
        {
          appId: captureRendererAppId,
          input: query,
          operation: "at.query",
          requestId: randomUUID(),
          workspaceId: capture.state.workspaceId
        }
      );
    }
  );
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.queryMentionDirectory,
    async (event, payload) => {
      const capture = trustedActiveCapture(event.sender);
      const query = normalizeTuttiExternalAtQueryDirectoryInput(payload);
      return requestCaptureWorkspaceOwner<TuttiExternalAtQueryResult[]>(
        capture,
        {
          appId: captureRendererAppId,
          input: query,
          operation: "at.queryDirectory",
          requestId: randomUUID(),
          workspaceId: capture.state.workspaceId
        }
      );
    }
  );
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.resolveMention,
    async (event, payload) => {
      const capture = trustedActiveCapture(event.sender);
      const mention = normalizeTuttiExternalAtResolveInput(payload);
      return requestCaptureWorkspaceOwner<TuttiExternalAtResolveResult | null>(
        capture,
        {
          appId: captureRendererAppId,
          input: mention,
          operation: "at.resolve",
          requestId: randomUUID(),
          workspaceId: capture.state.workspaceId
        }
      );
    }
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
    desktopIpcChannels.capture.selectReferences,
    async (event) => {
      const capture = trustedActiveCapture(event.sender);
      return requestCaptureReferenceSelection(capture);
    }
  );
  registerDesktopIpcHandler(
    desktopIpcChannels.capture.submit,
    async (event, submission) => {
      const capture = trustedActiveCapture(event.sender);
      capture.submission ??= submitCapture(capture, submission);
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
  const workspaceWindow = resolveWorkspaceWindow(workspaceId);
  if (!workspaceWindow) {
    throw new Error("Workspace renderer is unavailable");
  }
  const composerOptions = loadCaptureComposerOptions(
    workspaceWindow,
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
    backgroundColor: "#00000000",
    frame: false,
    fullscreenable: false,
    hasShadow: false,
    movable: true,
    resizable: false,
    show: false,
    skipTaskbar: true,
    transparent: true,
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
    displayHeight: display.bounds.height,
    displayWidth: display.bounds.width,
    locale: input.preferences.getLocale(),
    screenshotDataUrl: source.thumbnail.toDataURL(),
    themeAppearance: theme.appearance,
    workspaceId
  };
  const capture: ActiveCapture = {
    composerOptions,
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
  workspaceWindow: BrowserWindow,
  workspaceId: string
): Promise<DesktopCaptureComposerOptions> {
  const catalog =
    await requestWorkspaceOwnerRenderer<TuttiExternalAgentTargetCatalog>(
      workspaceWindow,
      {
        appId: captureRendererAppId,
        operation: "agentActivity.listTargets",
        requestId: randomUUID(),
        workspaceId
      }
    );
  const agents = catalog.agents
    .filter((target) => target.availability.status === "ready")
    .map((target) => ({
      description: target.description,
      iconUrl: target.iconUrl,
      id: target.agentTargetId,
      name: target.name,
      provider: target.provider
    }));
  if (agents.length === 0) {
    throw new Error("Screenshot capture requires an available Agent");
  }
  return { agents };
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
  capture: ActiveCapture,
  input: DesktopCaptureSubmitInput
): Promise<DesktopCaptureSubmitResult> {
  const attachment = capture.selected;
  if (!attachment) {
    throw new Error("Screenshot selection is unavailable");
  }
  const agentTargetId = input.agentTargetId.trim();
  if (!capture.state.agents.some((agent) => agent.id === agentTargetId)) {
    throw new Error("Screenshot Agent target is invalid");
  }
  const content = normalizeCapturePromptContent(input.content);
  const displayPrompt = input.displayPrompt?.trim() ?? "";
  const workspaceWindow = resolveWorkspaceWindow(capture.state.workspaceId);
  if (!workspaceWindow) {
    throw new Error("Workspace renderer is unavailable");
  }
  const agentSessionId = randomUUID();
  await requestWorkspaceOwnerRenderer<TuttiExternalAgentActivityActivateSessionResult>(
    workspaceWindow,
    {
      appId: captureRendererAppId,
      input: {
        agentSessionId,
        agentTargetId,
        clientSubmitId: randomUUID(),
        initialContent: content,
        ...(displayPrompt ? { initialDisplayPrompt: displayPrompt } : {}),
        title: resolveCaptureTitle(displayPrompt, attachment.displayName),
        visible: true
      },
      operation: "agentActivity.activateSession",
      requestId: randomUUID(),
      workspaceId: capture.state.workspaceId
    }
  );
  return { agentSessionId };
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

function resolveWorkspaceWindow(workspaceId: string): BrowserWindow | null {
  return (
    findWorkspaceWindow(workspaceId, "workspace") ??
    findWorkspaceWindow(workspaceId, "agent")
  );
}

function focusWorkspace(workspaceId: string): void {
  const window = resolveWorkspaceWindow(workspaceId);
  if (!window || window.isDestroyed()) {
    return;
  }
  if (window.isMinimized()) {
    window.restore();
  }
  window.show();
  window.focus();
}

function requestCaptureWorkspaceOwner<
  Result extends DesktopWorkspaceAppExternalRendererResult
>(
  capture: ActiveCapture,
  request: DesktopWorkspaceAppExternalRendererRequest
): Promise<Result> {
  const workspaceWindow = resolveWorkspaceWindow(capture.state.workspaceId);
  if (!workspaceWindow) {
    throw new Error("Workspace renderer is unavailable");
  }
  return requestWorkspaceOwnerRenderer<Result>(workspaceWindow, request);
}

async function requestCaptureReferenceSelection(
  capture: ActiveCapture
): Promise<TuttiExternalReferenceSelectResult> {
  const workspaceWindow = resolveWorkspaceWindow(capture.state.workspaceId);
  if (!workspaceWindow) {
    throw new Error("Workspace renderer is unavailable");
  }
  capture.window.hide();
  if (workspaceWindow.isMinimized()) {
    workspaceWindow.restore();
  }
  workspaceWindow.show();
  workspaceWindow.focus();
  try {
    return await requestWorkspaceOwnerRenderer<TuttiExternalReferenceSelectResult>(
      workspaceWindow,
      {
        appId: captureRendererAppId,
        operation: "references.select",
        requestId: randomUUID(),
        workspaceId: capture.state.workspaceId
      }
    );
  } finally {
    if (!capture.window.isDestroyed()) {
      capture.window.setAlwaysOnTop(true, "screen-saver");
      capture.window.show();
      capture.window.focus();
    }
  }
}
