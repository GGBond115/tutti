export interface CaptureSelectionFullscreenWindow {
  isFullScreen(): boolean;
  isSimpleFullScreen(): boolean;
  setFullScreen(fullscreen: boolean): void;
  setSimpleFullScreen(fullscreen: boolean): void;
}

export interface CaptureSelectionFullscreenState {
  fullScreen: boolean;
  simpleFullScreen: boolean;
}

export function resolveCaptureSelectionFullscreenOptions(
  platform: NodeJS.Platform
): { fullscreen: true; simpleFullscreen: boolean } {
  return {
    fullscreen: true,
    simpleFullscreen: platform === "darwin"
  };
}

export function enterCaptureSelectionFullscreen(
  window: CaptureSelectionFullscreenWindow,
  platform: NodeJS.Platform
): CaptureSelectionFullscreenState {
  if (platform === "darwin") {
    window.setSimpleFullScreen(true);
  } else {
    window.setFullScreen(true);
  }

  return {
    fullScreen: window.isFullScreen(),
    simpleFullScreen:
      platform === "darwin" ? window.isSimpleFullScreen() : false
  };
}
