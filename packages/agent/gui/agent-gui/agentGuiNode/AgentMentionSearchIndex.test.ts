import { describe, expect, it, vi } from "vitest";
import { AGENT_CONTEXT_MENTION_PROVIDER_IDS } from "./agentContextMentionProvider";
import {
  fetchAgentMentionFilterResult,
  type AgentMentionProviderQueryInput
} from "./AgentMentionSearchIndex";

describe("fetchAgentMentionFilterResult provenance filtering", () => {
  it("keeps ordinary files ready when the optional generated-file provider fails", async () => {
    const queryProviderMentionItemsById = vi.fn(
      async (input: AgentMentionProviderQueryInput) => {
        if (
          input.providerId ===
          AGENT_CONTEXT_MENTION_PROVIDER_IDS.agentGeneratedFile
        ) {
          throw new Error("generated files unavailable");
        }
        return [
          {
            directoryPath: "/workspace",
            entryKind: "file" as const,
            href: "/workspace/readme.md",
            kind: "file" as const,
            name: "readme.md",
            path: "/workspace/readme.md"
          }
        ];
      }
    );

    const result = await fetchAgentMentionFilterResult({
      workspaceId: "workspace-1",
      currentUserId: "user-1",
      query: "",
      filter: "file",
      sessionCwd: "/workspace",
      sectionKey: "project:/workspace",
      includeAgentGeneratedFiles: true,
      fileLimit: 30,
      currentFileSearchLimit: 30,
      currentIssueSearchLimit: 30,
      provenanceFilter: null,
      queryProviderMentionItemsById
    });

    expect(result.rawGroups.opened_files).toEqual([
      expect.objectContaining({ path: "/workspace/readme.md" })
    ]);
    expect(result.rawGroups.agent_generated_files).toEqual([]);
  });

  it("does not swallow an aborted optional generated-file request", async () => {
    const error = new Error("aborted");
    error.name = "AbortError";
    const queryProviderMentionItemsById = vi.fn(
      async (input: AgentMentionProviderQueryInput) => {
        if (
          input.providerId ===
          AGENT_CONTEXT_MENTION_PROVIDER_IDS.agentGeneratedFile
        ) {
          throw error;
        }
        return [];
      }
    );

    await expect(
      fetchAgentMentionFilterResult({
        workspaceId: "workspace-1",
        currentUserId: "user-1",
        query: "",
        filter: "file",
        sessionCwd: "/workspace",
        sectionKey: "project:/workspace",
        includeAgentGeneratedFiles: true,
        fileLimit: 30,
        currentFileSearchLimit: 30,
        currentIssueSearchLimit: 30,
        provenanceFilter: null,
        queryProviderMentionItemsById
      })
    ).rejects.toBe(error);
  });

  it("fails closed when generated files are required by a provenance filter", async () => {
    const error = new Error("generated files unavailable");
    const queryProviderMentionItemsById = vi.fn(
      async (input: AgentMentionProviderQueryInput) => {
        if (
          input.providerId ===
          AGENT_CONTEXT_MENTION_PROVIDER_IDS.agentGeneratedFile
        ) {
          throw error;
        }
        return [];
      }
    );

    await expect(
      fetchAgentMentionFilterResult({
        workspaceId: "workspace-1",
        currentUserId: "user-1",
        query: "",
        filter: "file",
        sessionCwd: "/workspace",
        sectionKey: "project:/workspace",
        includeAgentGeneratedFiles: true,
        fileLimit: 30,
        currentFileSearchLimit: 30,
        currentIssueSearchLimit: 30,
        provenanceFilter: {
          agentTargetIds: ["agent-1"],
          memberIds: null
        },
        queryProviderMentionItemsById
      })
    ).rejects.toBe(error);
  });

  it("queries only the provenance-aware file provider for a member-only constraint", async () => {
    const queryProviderMentionItemsById = vi.fn(
      async (input: AgentMentionProviderQueryInput) =>
        input.providerId ===
        AGENT_CONTEXT_MENTION_PROVIDER_IDS.agentGeneratedFile
          ? [
              {
                directoryPath: "/workspace",
                entryKind: "file" as const,
                href: "/workspace/report.md",
                kind: "file" as const,
                name: "report.md",
                path: "/workspace/report.md"
              }
            ]
          : [
              {
                directoryPath: "/workspace",
                entryKind: "file" as const,
                href: "/workspace/unfiltered.md",
                kind: "file" as const,
                name: "unfiltered.md",
                path: "/workspace/unfiltered.md"
              }
            ]
    );

    const result = await fetchAgentMentionFilterResult({
      workspaceId: "workspace-1",
      currentUserId: "user-1",
      query: "report",
      filter: "file",
      sessionCwd: "/workspace",
      sectionKey: "project:/workspace",
      includeAgentGeneratedFiles: false,
      fileLimit: 30,
      currentFileSearchLimit: 30,
      currentIssueSearchLimit: 30,
      provenanceFilter: {
        agentTargetIds: null,
        memberIds: ["member-1"]
      },
      queryProviderMentionItemsById
    });

    expect(queryProviderMentionItemsById).toHaveBeenCalledTimes(1);
    expect(queryProviderMentionItemsById).toHaveBeenCalledWith(
      expect.objectContaining({
        providerId: AGENT_CONTEXT_MENTION_PROVIDER_IDS.agentGeneratedFile,
        provenanceFilter: {
          agentTargetIds: null,
          memberIds: ["member-1"]
        }
      })
    );
    expect(result.rawGroups.opened_files).toEqual([]);
    expect(result.rawGroups.agent_generated_files).toEqual([
      expect.objectContaining({ path: "/workspace/report.md" })
    ]);
  });

  it("fails closed for issue summaries under a member-only constraint", async () => {
    const queryProviderMentionItemsById = vi.fn();

    const result = await fetchAgentMentionFilterResult({
      workspaceId: "workspace-1",
      currentUserId: "user-1",
      query: "task",
      filter: "issue",
      sessionCwd: "/workspace",
      sectionKey: "project:/workspace",
      includeAgentGeneratedFiles: false,
      fileLimit: 30,
      currentFileSearchLimit: 30,
      currentIssueSearchLimit: 30,
      provenanceFilter: {
        agentTargetIds: null,
        memberIds: ["member-1"]
      },
      queryProviderMentionItemsById
    });

    expect(queryProviderMentionItemsById).not.toHaveBeenCalled();
    expect(result.rawGroups.issues).toEqual([]);
  });
});
