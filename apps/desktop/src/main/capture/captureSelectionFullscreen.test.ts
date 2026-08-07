import assert from "node:assert/strict";
import test from "node:test";
import {
  enterCaptureSelectionFullscreen,
  resolveCaptureSelectionFullscreenOptions,
  type CaptureSelectionFullscreenWindow
} from "./captureSelectionFullscreen.ts";

test("macOS capture requests fullscreen and selects simple fullscreen mode", () => {
  assert.deepEqual(resolveCaptureSelectionFullscreenOptions("darwin"), {
    fullscreen: true,
    simpleFullscreen: true
  });
});

test("macOS capture explicitly enters simple fullscreen", () => {
  const calls: string[] = [];
  let simpleFullScreen = false;
  const window: CaptureSelectionFullscreenWindow = {
    isFullScreen: () => false,
    isSimpleFullScreen: () => simpleFullScreen,
    setFullScreen: () => calls.push("full"),
    setSimpleFullScreen: (fullscreen) => {
      calls.push("simple");
      simpleFullScreen = fullscreen;
    }
  };

  assert.deepEqual(enterCaptureSelectionFullscreen(window, "darwin"), {
    fullScreen: false,
    simpleFullScreen: true
  });
  assert.deepEqual(calls, ["simple"]);
});

test("non-macOS capture explicitly enters standard fullscreen", () => {
  const calls: string[] = [];
  let fullScreen = false;
  const window: CaptureSelectionFullscreenWindow = {
    isFullScreen: () => fullScreen,
    isSimpleFullScreen: () => false,
    setFullScreen: (fullscreen) => {
      calls.push("full");
      fullScreen = fullscreen;
    },
    setSimpleFullScreen: () => calls.push("simple")
  };

  assert.deepEqual(enterCaptureSelectionFullscreen(window, "win32"), {
    fullScreen: true,
    simpleFullScreen: false
  });
  assert.deepEqual(calls, ["full"]);
});
