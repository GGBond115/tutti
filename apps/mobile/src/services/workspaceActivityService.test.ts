import type {
  TuttidClient,
  WorkspaceAgentSession,
  WorkspaceAgentSessionMessage,
  WorkspaceSummary
} from "@tutti-os/client-tuttid-ts";
import { AgentDirectoryService } from "./agentDirectoryService";
import { ComposerDraftService } from "./composerDraftService";
import type { ClockPort } from "./servicePorts";
import { WorkspaceActivityService } from "./workspaceActivityService";
import { WorkspaceConversationRailService } from "./workspaceConversationRailService";
import { WorkspaceNavigationService } from "./workspaceNavigationService";

const workspace: WorkspaceSummary = {
  id: "workspace-1",
  lastOpenedAt: null,
  name: "Workspace"
};

describe("WorkspaceActivityService", () => {
  test("projects canonical session identity and authoritative message paging", async () => {
    const messageQueries: Array<Record<string, unknown>> = [];
    const client = createClient({
      listMessages: async (_workspaceId, agentSessionId, query) => {
        messageQueries.push(query);
        const older = "beforeVersion" in query;
        return {
          agentSessionId,
          hasMore: !older,
          latestVersion: 7,
          messages: [
            createMessage(
              older ? "message-older" : "message-latest",
              older ? 3 : 7
            )
          ]
        };
      }
    });
    const service = createService(client);

    await service.start();
    await flushAsyncWork();

    const initial = service.getSnapshot();
    expect(initial.selectedAgentSessionId).toBe("session-1");
    expect(initial.selectedSession?.userId).toBe("account-user-1");
    expect(
      initial.activity.sessionMessagesById["session-1"]?.map(
        (message) => message.messageId
      )
    ).toEqual(["message-latest"]);
    expect(messageQueries[0]).toEqual({ limit: 100, order: "desc" });

    await service.loadOlderMessages();
    await flushAsyncWork();

    expect(messageQueries[1]).toEqual({
      beforeVersion: 7,
      limit: 100,
      order: "desc"
    });
    expect(
      service
        .getSnapshot()
        .activity.sessionMessagesById["session-1"]?.map(
          (message) => message.messageId
        )
    ).toEqual(["message-older", "message-latest"]);

    service.dispose();
  });

  test("routes an existing-session submission through the engine command port", async () => {
    const sends: Array<{
      agentSessionId: string;
      input: Record<string, unknown>;
      workspaceId: string;
    }> = [];
    const client = createClient({
      listMessages: async (_workspaceId, agentSessionId) => ({
        agentSessionId,
        hasMore: false,
        latestVersion: 0,
        messages: []
      }),
      send: async (workspaceId, agentSessionId, input) => {
        sends.push({ agentSessionId, input, workspaceId });
        return new Promise<never>(() => undefined);
      }
    });
    const service = createService(client);

    await service.start();
    await flushAsyncWork();
    service.setDraft("continue");
    await service.send();
    await flushAsyncWork();

    expect(sends).toHaveLength(1);
    expect(sends[0]).toMatchObject({
      agentSessionId: "session-1",
      input: {
        content: [{ text: "continue", type: "text" }]
      },
      workspaceId: "workspace-1"
    });
    expect(service.getSnapshot().draft).toBe("");
    expect(service.getSnapshot().sending).toBe(true);

    service.dispose();
  });

  test("routes pin changes through the canonical session mutation command", async () => {
    let session = createSession();
    const pinRequests: boolean[] = [];
    const client = createClient({
      listMessages: async (_workspaceId, agentSessionId) => ({
        agentSessionId,
        hasMore: false,
        latestVersion: 0,
        messages: []
      }),
      pin: async (_workspaceId, _agentSessionId, input) => {
        pinRequests.push(input.pinned);
        session = { ...session, pinnedAtUnixMs: input.pinned ? 1_000 : null };
        return session;
      },
      session: () => session
    });
    const service = createService(client);

    await service.start();
    await flushAsyncWork();
    await service.toggleSessionPinned("session-1");

    expect(pinRequests).toEqual([true]);
    expect(service.getSnapshot().activity.sessions[0]?.pinnedAtUnixMs).toBe(
      1_000
    );

    service.dispose();
  });

  test("renames a session and reconciles the canonical rail snapshot", async () => {
    let session: WorkspaceAgentSession | null = createSession();
    const renameRequests: string[] = [];
    const client = createClient({
      listMessages: emptyMessagePage,
      rename: async (_workspaceId, _agentSessionId, input) => {
        renameRequests.push(input.title);
        session = { ...session!, title: input.title };
        return session;
      },
      session: () => session
    });
    const service = createService(client);

    await service.start();
    await flushAsyncWork();
    await service.renameSession("session-1", "  Renamed session  ");

    expect(renameRequests).toEqual(["Renamed session"]);
    expect(service.getSnapshot().selectedSession?.title).toBe(
      "Renamed session"
    );

    service.dispose();
  });

  test("deletes a session through the canonical mutation command", async () => {
    let session: WorkspaceAgentSession | null = createSession();
    const deleteRequests: string[][] = [];
    const client = createClient({
      deleteBatch: async (_workspaceId, input) => {
        deleteRequests.push(input.sessionIds);
        session = null;
        return {
          cleanupFailedSessionIds: [],
          removedMessages: 3,
          removedSessionIds: ["session-1"],
          removedSessions: 1
        };
      },
      listMessages: emptyMessagePage,
      session: () => session
    });
    const service = createService(client);

    await service.start();
    await flushAsyncWork();
    await service.deleteSession("session-1");

    expect(deleteRequests).toEqual([["session-1"]]);
    expect(service.getSnapshot().activity.sessions).toEqual([]);
    expect(service.getSnapshot().selectedAgentSessionId).toBeNull();

    service.dispose();
  });
});

function createService(client: TuttidClient): WorkspaceActivityService {
  const clock = new ManualClock();
  return new WorkspaceActivityService(
    workspace,
    client,
    new AgentDirectoryService(client),
    new WorkspaceNavigationService(),
    new ComposerDraftService(),
    new WorkspaceConversationRailService(workspace, client, clock),
    clock,
    "account-user-1"
  );
}

function createClient(options: {
  deleteBatch?(
    workspaceId: string,
    input: { sessionIds: string[] }
  ): Promise<{
    cleanupFailedSessionIds: string[];
    removedMessages: number;
    removedSessionIds: string[];
    removedSessions: number;
  }>;
  listMessages(
    workspaceId: string,
    agentSessionId: string,
    query: Record<string, unknown>
  ): Promise<{
    agentSessionId: string;
    hasMore: boolean;
    latestVersion: number;
    messages: WorkspaceAgentSessionMessage[];
  }>;
  send?(
    workspaceId: string,
    agentSessionId: string,
    input: Record<string, unknown>
  ): Promise<never>;
  pin?(
    workspaceId: string,
    agentSessionId: string,
    input: { pinned: boolean }
  ): Promise<WorkspaceAgentSession>;
  rename?(
    workspaceId: string,
    agentSessionId: string,
    input: { title: string }
  ): Promise<WorkspaceAgentSession>;
  session?(): WorkspaceAgentSession | null;
}): TuttidClient {
  return {
    deleteWorkspaceAgentSessionsBatch: options.deleteBatch,
    listAgentTargets: async () => ({ targets: [] }),
    listWorkspaceAgentSessionMessages: options.listMessages,
    listWorkspaceAgentSessionSections: async () => {
      const session =
        options.session === undefined ? createSession() : options.session();
      if (!session) {
        return {
          pinned: { hasMore: false, sessions: [], totalCount: 0 },
          sections: [],
          workspaceId: workspace.id
        };
      }
      const pinned = session.pinnedAtUnixMs
        ? { hasMore: false, sessions: [session], totalCount: 1 }
        : { hasMore: false, sessions: [], totalCount: 0 };
      return {
        pinned,
        sections: session.pinnedAtUnixMs
          ? []
          : [
              {
                hasMore: false,
                kind: "conversations" as const,
                sectionKey: "conversations",
                sessions: [session],
                totalCount: 1
              }
            ],
        workspaceId: workspace.id
      };
    },
    sendWorkspaceAgentSessionInput: options.send,
    updateWorkspaceAgentSessionPin: options.pin,
    updateWorkspaceAgentSessionTitle: options.rename
  } as unknown as TuttidClient;
}

async function emptyMessagePage(
  _workspaceId: string,
  agentSessionId: string
): Promise<{
  agentSessionId: string;
  hasMore: boolean;
  latestVersion: number;
  messages: WorkspaceAgentSessionMessage[];
}> {
  return {
    agentSessionId,
    hasMore: false,
    latestVersion: 0,
    messages: []
  };
}

function createSession(): WorkspaceAgentSession {
  return {
    activeTurn: null,
    activeTurnId: null,
    agentTargetId: null,
    capabilities: null,
    createdAtUnixMs: 1,
    cwd: "/",
    endedAtUnixMs: null,
    goal: null,
    id: "session-1",
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
    railSectionKey: "conversations",
    resumable: true,
    rootAgentSessionId: null,
    rootTurnId: null,
    settings: {},
    title: "Session",
    tuttiModeActivation: null,
    updatedAtUnixMs: 2,
    usage: null,
    visible: true
  };
}

function createMessage(
  messageId: string,
  version: number
): WorkspaceAgentSessionMessage {
  return {
    agentSessionId: "session-1",
    kind: "text",
    messageId,
    occurredAtUnixMs: version,
    payload: { text: messageId },
    role: "assistant",
    sequence: version,
    turnId: "turn-1",
    version
  };
}

async function flushAsyncWork(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

class ManualClock implements ClockPort {
  now(): number {
    return 1_000;
  }

  schedule(): { cancel(): void } {
    return { cancel: () => undefined };
  }
}
