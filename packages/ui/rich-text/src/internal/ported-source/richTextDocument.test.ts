import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  appendWorkspaceFileLinksToContent,
  extractWorkspaceFileLinksFromContent,
  extractPlainTextFromContent,
  normalizeWorkspaceFileLinkHref
} from "./richTextDocument.ts";

describe("richTextDocument", () => {
  it("appends workspaceFileLink nodes and extracts them back out", () => {
    const content = appendWorkspaceFileLinksToContent("", [
      { name: "demo.md", path: "workspace/tasks/task-1/attachments/demo.md" }
    ]);

    assert.equal(
      content,
      "[demo.md](workspace/tasks/task-1/attachments/demo.md)"
    );
    assert.deepEqual(extractWorkspaceFileLinksFromContent(content), [
      {
        name: "demo.md",
        path: "workspace/tasks/task-1/attachments/demo.md",
        href: "workspace/tasks/task-1/attachments/demo.md",
        kind: "file"
      }
    ]);
    assert.ok(extractPlainTextFromContent(content).includes("demo.md"));
  });

  it("preserves folder references with folder protocol and kind metadata", () => {
    const content = appendWorkspaceFileLinksToContent("", [
      {
        name: "specs",
        path: "workspace/tasks/task-1/attachments/specs",
        kind: "folder"
      }
    ]);

    assert.equal(content, "[specs](workspace/tasks/task-1/attachments/specs/)");
    assert.deepEqual(extractWorkspaceFileLinksFromContent(content), [
      {
        name: "specs",
        path: "workspace/tasks/task-1/attachments/specs/",
        href: "workspace/tasks/task-1/attachments/specs/",
        kind: "folder"
      }
    ]);
    assert.equal(
      normalizeWorkspaceFileLinkHref(
        "workspace/tasks/task-1/attachments/specs",
        "folder"
      ),
      "workspace/tasks/task-1/attachments/specs/"
    );
  });

  it("extracts plain text from markdown content while keeping link labels", () => {
    assert.equal(
      extractPlainTextFromContent(
        "目标：产出方案\n\n参考文件：[demo.md](/workspace/output/demo.md)\n- 第一项\n- 第二项"
      ),
      "目标：产出方案 参考文件： demo.md 第一项 第二项"
    );
  });
});
