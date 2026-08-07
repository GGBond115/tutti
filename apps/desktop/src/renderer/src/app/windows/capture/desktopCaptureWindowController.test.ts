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
      agents: [{ id: "agent-1", name: "Agent" }],
      defaultTopicId: "topic-1",
      displayHeight: 800,
      displayWidth: 1200,
      locale: "en",
      screenshotDataUrl: "data:image/png;base64,c2NyZWVu",
      themeAppearance: "light",
      topics: [{ id: "topic-1", isDefault: true, title: "Inbox" }],
      workspaceId: "workspace-1"
    }),
    select: async () => ({
      dataBase64: "cG5n",
      dataUrl: "data:image/png;base64,cG5n",
      displayName: "capture.png",
      height: 80,
      mimeType: "image/png",
      width: 100
    }),
    submit: async (input) => {
      submissions.push(input);
      if (failSubmission) {
        failSubmission = false;
        throw new Error("run unavailable");
      }
      return { issueId: "issue-1", runStarted: true };
    }
  };
  const controller = new DesktopCaptureWindowController(api);
  await controller.initialize();
  assert.equal(controller.getSnapshot().stage, "selecting");
  assert.equal(controller.getSnapshot().topicId, "topic-1");

  controller.beginSelection({ x: 10, y: 20 });
  controller.updateSelection({ x: 110, y: 100 });
  assert.equal(await controller.finishSelection(), true);
  assert.equal(controller.getSnapshot().stage, "composing");
  controller.setNote("Inspect this");

  await controller.submit("create-and-run");
  assert.equal(controller.getSnapshot().failed, true);
  assert.equal(controller.getSnapshot().submitting, false);
  await controller.submit("create-and-run");
  assert.equal(submissions.length, 2);
  assert.deepEqual(submissions[1], {
    action: "create-and-run",
    agentTargetId: "agent-1",
    note: "Inspect this",
    topicId: "topic-1"
  });
});
