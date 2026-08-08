import assert from "node:assert/strict";
import test from "node:test";
import {
  enterCaptureSelectionFullscreen,
  leaveCaptureSelectionFullscreen,
  resolveCaptureSelectionFullscreenOptions,
  type CaptureSelectionFullscreenTransitionWindow,
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

test("macOS waits for native simple fullscreen exit before continuing", async () => {
  const window = createTransitionWindow({ simpleFullScreen: true });
  let settled = false;

  const leaving = leaveCaptureSelectionFullscreen(window, "darwin").then(
    (result) => {
      settled = true;
      return result;
    }
  );

  assert.deepEqual(window.calls, ["simple:false"]);
  await Promise.resolve();
  assert.equal(settled, false);

  window.emitLeaveFullScreen();
  assert.equal(await leaving, "event");
  assert.equal(settled, true);
});

test("non-macOS waits for native fullscreen exit before continuing", async () => {
  const window = createTransitionWindow({ fullScreen: true });
  const leaving = leaveCaptureSelectionFullscreen(window, "win32");

  assert.deepEqual(window.calls, ["full:false"]);
  window.emitLeaveFullScreen();
  assert.equal(await leaving, "event");
});

test("fullscreen exit is a no-op after the native state already left", async () => {
  const window = createTransitionWindow({});

  assert.equal(
    await leaveCaptureSelectionFullscreen(window, "darwin"),
    "already-exited"
  );
  assert.deepEqual(window.calls, []);
});

function createTransitionWindow({
  fullScreen = false,
  simpleFullScreen = false
}: {
  fullScreen?: boolean;
  simpleFullScreen?: boolean;
}): CaptureSelectionFullscreenTransitionWindow & {
  calls: string[];
  emitLeaveFullScreen(): void;
} {
  const calls: string[] = [];
  let leaveListener: (() => void) | null = null;
  return {
    calls,
    emitLeaveFullScreen() {
      fullScreen = false;
      simpleFullScreen = false;
      leaveListener?.();
    },
    isFullScreen: () => fullScreen,
    isSimpleFullScreen: () => simpleFullScreen,
    once: (_event, listener) => {
      leaveListener = listener;
    },
    removeListener: (_event, listener) => {
      if (leaveListener === listener) {
        leaveListener = null;
      }
    },
    setFullScreen: (fullscreen) => {
      calls.push(`full:${fullscreen}`);
    },
    setSimpleFullScreen: (fullscreen) => {
      calls.push(`simple:${fullscreen}`);
    }
  };
}
