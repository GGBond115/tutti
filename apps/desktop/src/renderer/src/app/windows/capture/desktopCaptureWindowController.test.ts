import assert from "node:assert/strict";
import test from "node:test";
import type {
  DesktopCaptureApi,
  DesktopCaptureSubmitInput
} from "../../../../../shared/contracts/capture.ts";
import { DesktopCaptureWindowController } from "./desktopCaptureWindowController.ts";

test("DesktopCaptureWindowController owns selection and submission retry state", async () => {
  const submissions: DesktopCaptureSubmitInput[] = [];
  let failSubmission = true;
  const api: DesktopCaptureApi = {
    cancel: async () => undefined,
    getState: async () => ({
      agents: [],
      displayHeight: 800,
      displayWidth: 1200,
      locale: "en",
      screenshotDataUrl: "data:image/png;base64,c2NyZWVu",
      themeAppearance: "light",
      workspaceId: "workspace-1"
    }),
    select: async () => ({
      agents: [
        {
          iconUrl: "data:image/png;base64,aWNvbg==",
          id: "agent-1",
          name: "Agent",
          provider: "codex"
        }
      ],
      attachment: {
        dataBase64: "cG5n",
        dataUrl: "data:image/png;base64,cG5n",
        displayName: "capture.png",
        height: 80,
        mimeType: "image/png",
        width: 100
      }
    }),
    submit: async (input) => {
      submissions.push(input);
      if (failSubmission) {
        failSubmission = false;
        throw new Error("run unavailable");
      }
      return { agentSessionId: "session-1" };
    }
  };
  const controller = new DesktopCaptureWindowController(api);
  await controller.initialize();
  assert.equal(controller.getSnapshot().stage, "selecting");

  controller.beginSelection({ x: 10, y: 20 });
  controller.updateSelection({ x: 110, y: 100 });
  assert.equal(await controller.finishSelection(), true);
  assert.equal(controller.getSnapshot().stage, "composing");
  controller.insertPrompt("Create and manage the task");
  controller.insertPrompt("Create and manage the task");

  await controller.submit();
  assert.equal(controller.getSnapshot().failed, true);
  assert.equal(controller.getSnapshot().submitting, false);
  await controller.submit();
  assert.equal(submissions.length, 2);
  assert.deepEqual(submissions[1], {
    agentTargetId: "agent-1",
    content: [
      { text: "Create and manage the task", type: "text" },
      {
        data: "cG5n",
        mimeType: "image/png",
        name: "capture.png",
        type: "image"
      }
    ]
  });
});
