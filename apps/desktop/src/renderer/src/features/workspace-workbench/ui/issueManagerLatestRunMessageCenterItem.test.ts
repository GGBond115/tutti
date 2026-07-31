import assert from "node:assert/strict";
import { describe, it } from "node:test";
import type { WorkspaceAgentMessageCenterItem } from "@tutti-os/agent-gui/agent-message-center";
import type { IssueManagerLatestRunStatusRenderInput } from "@tutti-os/workspace-issue-manager/ui";
import { resolveIssueManagerLatestRunMessageCenterItem } from "./issueManagerLatestRunMessageCenterItem.ts";

describe("resolveIssueManagerLatestRunMessageCenterItem", () => {
  it("prefers the engine model item so a hidden delegate keeps its pending prompt", () => {
    const modelItem = messageCenterItem({
      agentSessionId: "delegate-1",
      pendingInteractionTarget: {
        agentSessionId: "delegate-1",
        requestId: "request-approval",
        turnId: "turn-1"
      },
      pendingPrompt: {
        kind: "approval",
        id: "approval:request-approval",
        turnId: "turn-1",
        requestId: "request-approval",
        callId: "call-1",
        title: "Run command",
        toolName: "Bash",
        status: "pending",
        input: { command: "pnpm test" },
        options: [{ id: "allow", label: "Allow", kind: "allow" }],
        output: null,
        occurredAtUnixMs: 21
      },
      status: "waiting"
    });

    const item = resolveIssueManagerLatestRunMessageCenterItem({
      agentSessionId: "delegate-1",
      input: renderInput(),
      itemCandidates: [modelItem],
      session: null
    });

    assert.equal(item, modelItem);
    assert.equal(item.pendingPrompt?.kind, "approval");
    assert.equal(item.pendingInteractionTarget?.requestId, "request-approval");
  });

  it("synthesizes a promptless run fallback only when the engine has no item", () => {
    const item = resolveIssueManagerLatestRunMessageCenterItem({
      agentSessionId: "delegate-1",
      input: renderInput(),
      itemCandidates: [messageCenterItem({ agentSessionId: "other-session" })],
      session: null
    });

    assert.equal(item.id, "issue-manager-run-run-1");
    assert.equal(item.status, "working");
    assert.equal(item.pendingPrompt, null);
    assert.equal(item.pendingInteractionTarget, null);
    assert.equal(item.needsAttentionKind, null);
  });
});

function renderInput(): IssueManagerLatestRunStatusRenderInput {
  return {
    canOpenAgentSession: true,
    copy: {} as IssueManagerLatestRunStatusRenderInput["copy"],
    latestRun: {
      runId: "run-1",
      issueId: "issue-1",
      workspaceId: "workspace-1",
      requesterUserId: "user-1",
      agentUserId: "agent-1",
      agentSessionId: "delegate-1",
      agentProvider: "codex",
      status: "running",
      updatedAtUnix: 1_700_000_000_000
    },
    title: "Delegated task"
  };
}

function messageCenterItem(
  overrides: Partial<WorkspaceAgentMessageCenterItem>
): WorkspaceAgentMessageCenterItem {
  return {
    agentSessionId: "session-1",
    cwd: "",
    id: "item-1",
    identity: null,
    lastAgentMessageAtUnixMs: null,
    lastAgentMessageSummary: "",
    digest: {
      primary: {
        kind: "summary",
        summary: "",
        occurredAtUnixMs: null
      }
    },
    needsAttentionKind: null,
    needsAttentionSummary: null,
    pendingInteractionTarget: null,
    pendingPrompt: null,
    provider: "codex",
    sortTimeUnixMs: 0,
    status: "idle",
    title: "",
    userId: null,
    ...overrides
  };
}
