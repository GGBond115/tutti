import type {
  TuttidClient,
  WorkspaceAgentSession,
  WorkspaceSummary
} from "@tutti-os/client-tuttid-ts";
import type { ClockPort } from "./servicePorts";
import { WorkspaceConversationRailService } from "./workspaceConversationRailService";

const workspace: WorkspaceSummary = {
  id: "workspace-1",
  lastOpenedAt: null,
  name: "Workspace"
};

describe("WorkspaceConversationRailService", () => {
  test("coalesces an engine reconcile with the initial section read", async () => {
    let listCalls = 0;
    const client = {
      listWorkspaceAgentSessionSections: async () => {
        listCalls += 1;
        await Promise.resolve();
        return {
          pinned: { hasMore: false, sessions: [], totalCount: 0 },
          sections: [],
          workspaceId: workspace.id
        };
      }
    } as unknown as TuttidClient;
    const service = new WorkspaceConversationRailService(
      workspace,
      client,
      new ManualClock()
    );

    await Promise.all([service.start(), service.reconcile()]);

    expect(listCalls).toBe(1);
    service.dispose();
  });

  test("keeps server section identity and loads the next exact page", async () => {
    const sectionQueries: Array<Record<string, unknown>> = [];
    const client = {
      listWorkspaceAgentPinnedSessionPage: async () => ({
        page: { hasMore: false, sessions: [], totalCount: 0 },
        workspaceId: workspace.id
      }),
      listWorkspaceAgentSessionSectionPage: async (
        _workspaceId: string,
        query: Record<string, unknown>
      ) => {
        sectionQueries.push(query);
        return {
          section: {
            hasMore: false,
            kind: "project" as const,
            sectionKey: "project:/repo",
            sessions: [createSession("session-2", 2)],
            totalCount: 2
          },
          workspaceId: workspace.id
        };
      },
      listWorkspaceAgentSessionSections: async () => ({
        pinned: { hasMore: false, sessions: [], totalCount: 0 },
        sections: [
          {
            hasMore: true,
            kind: "project" as const,
            nextCursor: "cursor-1",
            sectionKey: "project:/repo",
            sessions: [createSession("session-1", 1)],
            totalCount: 2,
            userProject: {
              createdAtUnixMs: 1,
              id: "project-1",
              label: "Repo",
              lastUsedAtUnixMs: 1,
              path: "/repo",
              pinnedAtUnixMs: 0,
              sectionKey: "project:/repo",
              updatedAtUnixMs: 1
            }
          }
        ],
        workspaceId: workspace.id
      })
    } as unknown as TuttidClient;
    const service = new WorkspaceConversationRailService(
      workspace,
      client,
      new ManualClock()
    );

    await service.start();
    await service.loadMore("section:project:/repo");

    expect(sectionQueries).toEqual([
      {
        cursor: "cursor-1",
        limit: 30,
        sectionKey: "project:/repo"
      }
    ]);
    expect(service.getSnapshot().sections[0]).toMatchObject({
      hasMore: false,
      id: "section:project:/repo",
      sessionIds: ["session-1", "session-2"],
      totalCount: 2
    });
    expect(service.getSnapshot().sessions.map((session) => session.id)).toEqual(
      ["session-1", "session-2"]
    );

    service.dispose();
  });

  test("caps refresh reads and preserves pages already loaded past the cap", async () => {
    const listQueries: Array<{ limitPerSection?: number }> = [];
    const firstPage = Array.from({ length: 100 }, (_, index) =>
      createSession(`session-${index + 1}`, 200 - index)
    );
    const nextPage = Array.from({ length: 30 }, (_, index) =>
      createSession(`session-${index + 101}`, 100 - index)
    );
    const client = {
      listWorkspaceAgentSessionSectionPage: async () => ({
        section: {
          hasMore: true,
          kind: "conversations" as const,
          nextCursor: "cursor-2",
          sectionKey: "conversations",
          sessions: nextPage,
          totalCount: 140
        },
        workspaceId: workspace.id
      }),
      listWorkspaceAgentSessionSections: async (
        _workspaceId: string,
        query: { limitPerSection?: number }
      ) => {
        listQueries.push(query);
        return {
          pinned: { hasMore: false, sessions: [], totalCount: 0 },
          sections: [
            {
              hasMore: true,
              kind: "conversations" as const,
              nextCursor: "cursor-1",
              sectionKey: "conversations",
              sessions: firstPage,
              totalCount: 140
            }
          ],
          workspaceId: workspace.id
        };
      }
    } as unknown as TuttidClient;
    const service = new WorkspaceConversationRailService(
      workspace,
      client,
      new ManualClock()
    );

    await service.start();
    await service.loadMore("section:conversations");
    await service.refresh();

    expect(listQueries).toEqual([
      { limitPerSection: 30 },
      { limitPerSection: 100 }
    ]);
    expect(service.getSnapshot().sections[0]?.sessionIds).toHaveLength(130);
    expect(service.getSnapshot().sections[0]).toMatchObject({
      hasMore: true,
      nextCursor: "cursor-2",
      totalCount: 140
    });

    service.dispose();
  });
});

function createSession(
  id: string,
  updatedAtUnixMs: number
): WorkspaceAgentSession {
  return {
    activeTurn: null,
    activeTurnId: null,
    agentTargetId: null,
    capabilities: null,
    createdAtUnixMs: 1,
    cwd: "/repo",
    endedAtUnixMs: null,
    goal: null,
    id,
    imported: false,
    kind: "root",
    latestTurn: null,
    latestTurnInteractions: [],
    parentAgentSessionId: null,
    parentToolCallId: null,
    parentTurnId: null,
    pendingInteractions: [],
    permissionConfig: { configurable: false, modes: [] },
    pinnedAtUnixMs: null,
    provider: "codex",
    providerSessionId: null,
    railSectionKey: "project:/repo",
    resumable: true,
    rootAgentSessionId: null,
    rootTurnId: null,
    settings: {},
    title: id,
    tuttiModeActivation: null,
    updatedAtUnixMs,
    usage: null,
    visible: true
  };
}

class ManualClock implements ClockPort {
  now(): number {
    return 1_000;
  }

  schedule(): { cancel(): void } {
    return { cancel: () => undefined };
  }
}
