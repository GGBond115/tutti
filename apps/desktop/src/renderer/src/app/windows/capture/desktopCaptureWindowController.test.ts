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
    getComposerOptions: async () => ({
      agents: [
        {
          capabilities: {
            imageInput: true,
            workspaceReferences: true
          },
          iconUrl: "data:image/png;base64,aWNvbg==",
          id: "agent-1",
          name: "Agent",
          provider: "codex"
        }
      ]
    }),
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
          capabilities: {
            imageInput: true,
            workspaceReferences: true
          },
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
    },
    userProjects: createUserProjectsApi()
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
  await controller.setProjectPath(
    (await controller.userProjectApi.selectDirectory?.())?.path ?? null
  );

  await controller.submit(
    "agent-1",
    undefined,
    "Fix the selected bug",
    "Create a Task, start the work, and keep the Task updated"
  );
  assert.equal(controller.getSnapshot().failed, true);
  assert.equal(controller.getSnapshot().submitting, false);
  await controller.submit(
    "agent-stale",
    undefined,
    "Fix the selected bug",
    "Create a Task, start the work, and keep the Task updated"
  );
  assert.equal(submissions.length, 1);
  await controller.submit(
    "agent-1",
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
      capabilities: {
        imageInput: true,
        workspaceReferences: true
      },
      iconUrl: "data:image/png;base64,Y29kZXg=",
      id: "agent-codex",
      name: "Codex",
      provider: "codex"
    },
    {
      capabilities: {
        imageInput: true,
        workspaceReferences: true
      },
      iconUrl: "data:image/png;base64,dHV0dGk=",
      id: "agent-tutti",
      name: "Tutti Agent",
      provider: "tutti-agent"
    }
  ];
  const api: DesktopCaptureApi = {
    cancel: async () => undefined,
    getComposerOptions: async () => ({ agents }),
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
    submit: async () => ({ agentSessionId: "session-1" }),
    userProjects: createUserProjectsApi()
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

test("DesktopCaptureWindowController refreshes only the selected Agent capability for the selected project", async () => {
  const composerCwds: Array<string | null | undefined> = [];
  const composerAgentTargetIds: string[] = [];
  const submissions: DesktopCaptureSubmitInput[] = [];
  const agent = (imageInput: boolean) => ({
    capabilities: { imageInput, workspaceReferences: true },
    iconUrl: "data:image/png;base64,aWNvbg==",
    id: "agent-1",
    name: "Agent",
    provider: "codex"
  });
  const api: DesktopCaptureApi = {
    cancel: async () => undefined,
    getComposerOptions: async ({ agentTargetId, cwd }) => {
      composerAgentTargetIds.push(agentTargetId);
      composerCwds.push(cwd);
      return { agents: [agent(cwd === "/workspace/vision")] };
    },
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
      agents: [agent(true)],
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
    submit: async (input) => {
      submissions.push(input);
      return { agentSessionId: "session-1" };
    },
    userProjects: createUserProjectsApi()
  };
  const controller = new DesktopCaptureWindowController(api);
  await controller.initialize();
  controller.beginSelection({ x: 10, y: 20 });
  controller.updateSelection({ x: 110, y: 100 });
  assert.equal(await controller.finishSelection(), true);

  await controller.setProjectPath("/workspace/text-only");
  assert.equal(
    controller.getSnapshot().capture?.agents[0]?.capabilities.imageInput,
    false
  );
  await controller.submit("agent-1");
  assert.equal(submissions.length, 0);

  await controller.setProjectPath("/workspace/vision");
  assert.equal(
    controller.getSnapshot().capture?.agents[0]?.capabilities.imageInput,
    true
  );
  await controller.submit("agent-1");
  assert.equal(submissions.length, 1);
  assert.deepEqual(composerAgentTargetIds, ["agent-1", "agent-1", "agent-1"]);
  assert.deepEqual(composerCwds, [
    null,
    "/workspace/text-only",
    "/workspace/vision"
  ]);
});

test("DesktopCaptureWindowController discards an older project capability response", async () => {
  const deferred = new Map<string, (imageInput: boolean) => void>();
  const agent = (imageInput: boolean) => ({
    capabilities: { imageInput, workspaceReferences: true },
    iconUrl: "data:image/png;base64,aWNvbg==",
    id: "agent-1",
    name: "Agent",
    provider: "codex"
  });
  const api: DesktopCaptureApi = {
    cancel: async () => undefined,
    getComposerOptions: ({ cwd }) => {
      if (!cwd) {
        return Promise.resolve({ agents: [agent(true)] });
      }
      return new Promise((resolve) => {
        deferred.set(cwd, (imageInput) =>
          resolve({ agents: [agent(imageInput)] })
        );
      });
    },
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
      agents: [agent(true)],
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
    submit: async () => ({ agentSessionId: "session-1" }),
    userProjects: createUserProjectsApi()
  };
  const controller = new DesktopCaptureWindowController(api);
  await controller.initialize();
  controller.beginSelection({ x: 10, y: 20 });
  controller.updateSelection({ x: 110, y: 100 });
  assert.equal(await controller.finishSelection(), true);

  const older = controller.setProjectPath("/workspace/older");
  const newer = controller.setProjectPath("/workspace/newer");
  deferred.get("/workspace/newer")?.(true);
  await newer;
  deferred.get("/workspace/older")?.(false);
  await older;

  assert.equal(controller.getSnapshot().projectPath, "/workspace/newer");
  assert.equal(
    controller.getSnapshot().capture?.agents[0]?.capabilities.imageInput,
    true
  );
});

function createUserProjectsApi(): DesktopCaptureApi["userProjects"] {
  return {
    list: async () => ({ projects: [] }),
    prepareSelection: async () => ({
      isSelectedPathMissing: false,
      projects: [],
      selection: { kind: "none" }
    }),
    use: async ({ path }) => ({
      id: path,
      label: path,
      path,
      pinnedAtUnixMs: 0
    })
  };
}
