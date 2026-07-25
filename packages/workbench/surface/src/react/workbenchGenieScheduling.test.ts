import assert from "node:assert/strict";
import test from "node:test";
import {
  resolveNativeFirstGenieTexture,
  startCachedWorkbenchGenieRestore
} from "./workbenchGenieScheduling.ts";

test("paints the cached genie texture before scheduling launch", () => {
  const events: string[] = [];
  const frames: (() => void)[] = [];
  const tasks: (() => void)[] = [];
  let cancelAnimation = () => {};

  startCachedWorkbenchGenieRestore({
    launch: () => events.push("launch"),
    requestFrame: (callback) => {
      frames.push(callback);
    },
    scheduleTask: (callback) => {
      tasks.push(callback);
    },
    startAnimation: () => {
      events.push("animation");
      cancelAnimation = () => events.push("cancel");
    }
  });

  assert.deepEqual(events, ["animation"]);
  cancelAnimation();
  frames.shift()?.();
  assert.deepEqual(events, ["animation", "cancel"]);
  tasks.shift()?.();
  assert.deepEqual(events, ["animation", "cancel", "launch"]);
});

test("does not prepare the DOM fallback when native capture renders", async () => {
  let domFallbackCalls = 0;
  const result = await resolveNativeFirstGenieTexture({
    nativeImageUrlPromise: Promise.resolve("data:image/png;base64,native"),
    renderDomFallback: () => {
      domFallbackCalls += 1;
      return "dom";
    },
    renderNativeImage: () => "native",
    timeoutMs: 120
  });

  assert.equal(result.texture, "native");
  assert.equal(result.nativeStatus, "resolved");
  assert.equal(domFallbackCalls, 0);
});

test("prepares the DOM fallback after native failure or timeout", async () => {
  let failureFallbackCalls = 0;
  const failed = await resolveNativeFirstGenieTexture({
    nativeImageUrlPromise: Promise.reject(new Error("capture failed")),
    renderDomFallback: () => {
      failureFallbackCalls += 1;
      return "dom-after-failure";
    },
    renderNativeImage: () => "native",
    timeoutMs: 120
  });

  let timeoutFallbackCalls = 0;
  const timedOut = await resolveNativeFirstGenieTexture({
    nativeImageUrlPromise: new Promise(() => {}),
    renderDomFallback: () => {
      timeoutFallbackCalls += 1;
      return "dom-after-timeout";
    },
    renderNativeImage: () => "native",
    timeoutMs: 0
  });

  assert.equal(failed.texture, "dom-after-failure");
  assert.equal(failed.nativeStatus, "resolved");
  assert.equal(failureFallbackCalls, 1);
  assert.equal(timedOut.texture, "dom-after-timeout");
  assert.equal(timedOut.nativeStatus, "pending");
  assert.equal(timeoutFallbackCalls, 1);
});
