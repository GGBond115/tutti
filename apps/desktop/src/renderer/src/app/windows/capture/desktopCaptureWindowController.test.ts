import assert from "node:assert/strict";
import test from "node:test";
import type {
  DesktopCaptureApi,
  DesktopCaptureSubmitInput
} from "../../../../../shared/contracts/capture.ts";
import {
  DesktopCaptureWindowController,
  prependCapturePromptInstruction
} from "./desktopCaptureWindowController.ts";

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
    queryMentionDirectory: async () => [],
    queryMentions: async () => [],
    resolveMention: async () => null,
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
    selectFiles: async () => [],
    selectProjectDirectory: async () => ({ path: "/workspace/alpha" }),
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
  controller.setContent([
    { text: "Fix the selected bug", type: "text" },
    ...controller.getSnapshot().content.filter((block) => block.type !== "text")
  ]);
  controller.setTrackWithTask(true);
  controller.setProjectPath(
    (await controller.selectProjectDirectory())?.path ?? null
  );

  await controller.submit(
    undefined,
    "Fix the selected bug",
    "Create a Task, start the work, and keep the Task updated"
  );
  assert.equal(controller.getSnapshot().failed, true);
  assert.equal(controller.getSnapshot().submitting, false);
  await controller.submit(
    undefined,
    "Fix the selected bug",
    "Create a Task, start the work, and keep the Task updated"
  );
  assert.equal(submissions.length, 2);
  assert.deepEqual(submissions[1], {
    agentTargetId: "agent-1",
    content: [
      {
        text: "Create a Task, start the work, and keep the Task updated\n\nFix the selected bug",
        type: "text"
      },
      {
        data: "cG5n",
        mimeType: "image/png",
        name: "capture.png",
        type: "image"
      }
    ],
    cwd: "/workspace/alpha",
    displayPrompt: "Fix the selected bug"
  });
  const visibleText = controller.getSnapshot().content[0];
  assert.equal(
    visibleText?.type === "text" ? visibleText.text : null,
    "Fix the selected bug"
  );
});

test("prependCapturePromptInstruction adds an instruction without mutating image-only content", () => {
  const content = [
    {
      data: "cG5n",
      mimeType: "image/png",
      name: "capture.png",
      type: "image" as const
    }
  ];
  assert.deepEqual(
    prependCapturePromptInstruction(content, "Create and track the Task"),
    [{ text: "Create and track the Task", type: "text" }, content[0]]
  );
  assert.deepEqual(content, [
    {
      data: "cG5n",
      mimeType: "image/png",
      name: "capture.png",
      type: "image"
    }
  ]);
});

test("DesktopCaptureWindowController restores and remembers an available Agent Target", async () => {
  const writes: Array<{ agentTargetId: string; workspaceId: string }> = [];
  const agents = [
    {
      iconUrl: "data:image/png;base64,Y29kZXg=",
      id: "agent-codex",
      name: "Codex",
      provider: "codex"
    },
    {
      iconUrl: "data:image/png;base64,dHV0dGk=",
      id: "agent-tutti",
      name: "Tutti Agent",
      provider: "tutti-agent"
    }
  ];
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
    queryMentionDirectory: async () => [],
    queryMentions: async () => [],
    resolveMention: async () => null,
    select: async () => ({
      agents,
      attachment: {
        dataBase64: "cG5n",
        dataUrl: "data:image/png;base64,cG5n",
        displayName: "capture.png",
        height: 80,
        mimeType: "image/png",
        width: 100
      }
    }),
    selectFiles: async () => [],
    selectProjectDirectory: async () => null,
    submit: async () => ({ agentSessionId: "session-1" })
  };
  const controller = new DesktopCaptureWindowController(api, {
    read: () => "agent-tutti",
    write: (workspaceId, agentTargetId) =>
      writes.push({ agentTargetId, workspaceId })
  });

  await controller.initialize();
  controller.beginSelection({ x: 10, y: 20 });
  controller.updateSelection({ x: 110, y: 100 });
  assert.equal(await controller.finishSelection(), true);
  assert.equal(controller.getSnapshot().agentTargetId, "agent-tutti");

  controller.setAgentTargetId("agent-codex");
  assert.equal(controller.getSnapshot().agentTargetId, "agent-codex");
  assert.deepEqual(writes, [
    { agentTargetId: "agent-codex", workspaceId: "workspace-1" }
  ]);
});
