import assert from "node:assert/strict";
import test from "node:test";
import { normalizeCapturePromptContent } from "./captureAgentPrompt.ts";

test("normalizeCapturePromptContent keeps editable text and image blocks", () => {
  assert.deepEqual(
    normalizeCapturePromptContent([
      { text: "  Inspect this  ", type: "text" },
      {
        data: "cG5n",
        mimeType: "image/png",
        name: " capture.png ",
        type: "image"
      }
    ]),
    [
      { text: "Inspect this", type: "text" },
      {
        data: "cG5n",
        mimeType: "image/png",
        name: "capture.png",
        type: "image"
      }
    ]
  );
});

test("normalizeCapturePromptContent rejects unsupported or malformed blocks", () => {
  assert.throws(
    () =>
      normalizeCapturePromptContent([{ path: "/tmp/file.txt", type: "file" }]),
    /unsupported content/u
  );
  assert.throws(
    () =>
      normalizeCapturePromptContent([
        { data: "not base64!", mimeType: "image/png", type: "image" }
      ]),
    /image is invalid/u
  );
  assert.throws(() => normalizeCapturePromptContent([]), /prompt is empty/u);
});
