import type {
  TuttidClient,
  UserProject,
  WorkspaceAgentSession,
  WorkspaceAgentSessionSection,
  WorkspaceSummary
} from "@tutti-os/client-tuttid-ts";
import { ObservableService } from "./observableService";
import type { ClockPort } from "./servicePorts";

const SESSION_POLL_MS = 2_000;
const SESSION_PAGE_SIZE = 30;
const SESSION_SECTION_LIMIT_MAX = 100;

export type WorkspaceConversationRailSectionKind =
  | "pinned"
  | "project"
  | "conversations";

export interface WorkspaceConversationRailMembership {
  hasMore: boolean;
  id: string;
  kind: WorkspaceConversationRailSectionKind;
  nextCursor: string | null;
  project: UserProject | null;
  sectionKey: string | null;
  sessionIds: readonly string[];
  totalCount: number;
}

export interface WorkspaceConversationRailSnapshot {
  errorCode: "request_failed" | null;
  loadingMoreSectionId: string | null;
  sections: readonly WorkspaceConversationRailMembership[];
  sessions: readonly WorkspaceAgentSession[];
  status: "idle" | "loading" | "ready";
}

export class WorkspaceConversationRailService extends ObservableService<WorkspaceConversationRailSnapshot> {
  readonly _serviceBrand: undefined;
  private disposed = false;
  private paused = false;
  private loadPromise: Promise<void> | null = null;
  private pollTask: { cancel(): void } | null = null;
  private snapshot: WorkspaceConversationRailSnapshot = {
    errorCode: null,
    loadingMoreSectionId: null,
    sections: [],
    sessions: [],
    status: "idle"
  };

  constructor(
    readonly workspace: WorkspaceSummary,
    private readonly client: TuttidClient,
    private readonly clock: ClockPort
  ) {
    super();
  }

  getSnapshot = (): WorkspaceConversationRailSnapshot => this.snapshot;

  start(): Promise<void> {
    return this.refresh();
  }

  refresh(): Promise<void> {
    if (
      this.loadPromise ||
      this.snapshot.loadingMoreSectionId ||
      this.paused ||
      this.disposed
    ) {
      return this.loadPromise ?? Promise.resolve();
    }
    const loadedPerSection = Math.min(
      SESSION_SECTION_LIMIT_MAX,
      this.snapshot.sections.reduce(
        (maximum, section) => Math.max(maximum, section.sessionIds.length),
        SESSION_PAGE_SIZE
      )
    );
    if (this.snapshot.status === "idle") {
      this.snapshot = { ...this.snapshot, status: "loading" };
      this.emitChange();
    }
    this.loadPromise = this.client
      .listWorkspaceAgentSessionSections(this.workspace.id, {
        limitPerSection: loadedPerSection
      })
      .then((response) => {
        if (this.disposed || this.paused) return;
        const freshSections = [
          ...(response.pinned.totalCount > 0
            ? [
                membershipFromPage({
                  hasMore: response.pinned.hasMore,
                  id: "pinned",
                  kind: "pinned" as const,
                  nextCursor: response.pinned.nextCursor,
                  project: null,
                  sectionKey: null,
                  sessions: response.pinned.sessions,
                  totalCount: response.pinned.totalCount
                })
              ]
            : []),
          ...response.sections.map(membershipFromSection)
        ];
        const sections = reconcileRefreshedMemberships(
          freshSections,
          this.snapshot.sections
        );
        const freshSessions = uniqueSessions(freshSections, response);
        this.snapshot = {
          errorCode: null,
          loadingMoreSectionId: null,
          sections,
          sessions: sessionsForMemberships(
            sections,
            mergeSessions(this.snapshot.sessions, freshSessions)
          ),
          status: "ready"
        };
        this.emitChange();
      })
      .catch(() => {
        if (this.disposed) return;
        this.snapshot = {
          ...this.snapshot,
          errorCode: "request_failed",
          status: "ready"
        };
        this.emitChange();
      })
      .finally(() => {
        this.loadPromise = null;
        this.schedulePoll();
      });
    return this.loadPromise;
  }

  async reconcile(): Promise<WorkspaceConversationRailSnapshot> {
    if (this.loadPromise) {
      await this.loadPromise;
    } else {
      await this.refresh();
    }
    if (this.disposed || this.paused) {
      throw new Error("mobile conversation rail is unavailable");
    }
    return this.snapshot;
  }

  async loadMore(sectionId: string): Promise<void> {
    await this.loadPromise;
    const section = this.snapshot.sections.find(
      (candidate) => candidate.id === sectionId
    );
    if (
      !section ||
      !section.hasMore ||
      !section.nextCursor ||
      this.snapshot.loadingMoreSectionId ||
      this.paused ||
      this.disposed
    ) {
      return;
    }
    this.pollTask?.cancel();
    this.pollTask = null;
    this.snapshot = {
      ...this.snapshot,
      errorCode: null,
      loadingMoreSectionId: sectionId
    };
    this.emitChange();
    try {
      const page =
        section.kind === "pinned"
          ? (
              await this.client.listWorkspaceAgentPinnedSessionPage(
                this.workspace.id,
                {
                  cursor: section.nextCursor,
                  limit: SESSION_PAGE_SIZE
                }
              )
            ).page
          : (
              await this.client.listWorkspaceAgentSessionSectionPage(
                this.workspace.id,
                {
                  cursor: section.nextCursor,
                  limit: SESSION_PAGE_SIZE,
                  sectionKey: section.sectionKey!
                }
              )
            ).section;
      if (this.disposed || this.paused) return;
      const sessions = mergeSessions(this.snapshot.sessions, page.sessions);
      const sections = this.snapshot.sections.map((candidate) =>
        candidate.id === sectionId
          ? {
              ...candidate,
              hasMore: page.hasMore,
              nextCursor: page.nextCursor ?? null,
              sessionIds: uniqueIds([
                ...candidate.sessionIds,
                ...page.sessions.map((session) => session.id)
              ]),
              totalCount: page.totalCount
            }
          : candidate
      );
      this.snapshot = {
        errorCode: null,
        loadingMoreSectionId: null,
        sections,
        sessions,
        status: "ready"
      };
    } catch {
      if (this.disposed) return;
      this.snapshot = {
        ...this.snapshot,
        errorCode: "request_failed",
        loadingMoreSectionId: null
      };
    } finally {
      if (!this.disposed && this.snapshot.loadingMoreSectionId === sectionId) {
        this.snapshot = {
          ...this.snapshot,
          loadingMoreSectionId: null
        };
      }
      if (!this.disposed) this.emitChange();
      this.schedulePoll();
    }
  }

  pause(): void {
    if (this.paused || this.disposed) return;
    this.paused = true;
    this.pollTask?.cancel();
    this.pollTask = null;
  }

  resume(): void {
    if (!this.paused || this.disposed) return;
    this.paused = false;
    void this.refresh();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.pollTask?.cancel();
    this.pollTask = null;
    this.loadPromise = null;
    this.clearListeners();
  }

  private schedulePoll(): void {
    this.pollTask?.cancel();
    if (this.disposed || this.paused) return;
    this.pollTask = this.clock.schedule(SESSION_POLL_MS, () => {
      this.pollTask = null;
      void this.refresh();
    });
  }
}

function membershipFromSection(
  section: WorkspaceAgentSessionSection
): WorkspaceConversationRailMembership {
  return membershipFromPage({
    ...section,
    id: `section:${section.sectionKey}`,
    kind: section.kind,
    nextCursor: section.nextCursor,
    project: section.userProject ?? null,
    sectionKey: section.sectionKey
  });
}

function membershipFromPage(input: {
  hasMore: boolean;
  id: string;
  kind: WorkspaceConversationRailSectionKind;
  nextCursor?: string;
  project: UserProject | null;
  sectionKey: string | null;
  sessions: readonly WorkspaceAgentSession[];
  totalCount: number;
}): WorkspaceConversationRailMembership {
  return {
    hasMore: input.hasMore,
    id: input.id,
    kind: input.kind,
    nextCursor: input.nextCursor ?? null,
    project: input.project,
    sectionKey: input.sectionKey,
    sessionIds: input.sessions.map((session) => session.id),
    totalCount: input.totalCount
  };
}

function uniqueSessions(
  sections: readonly WorkspaceConversationRailMembership[],
  response: {
    pinned: { sessions: readonly WorkspaceAgentSession[] };
    sections: readonly WorkspaceAgentSessionSection[];
  }
): WorkspaceAgentSession[] {
  const sessionsById = new Map<string, WorkspaceAgentSession>();
  for (const session of response.pinned.sessions) {
    sessionsById.set(session.id, session);
  }
  for (const section of response.sections) {
    for (const session of section.sessions) {
      sessionsById.set(session.id, session);
    }
  }
  const orderedIds = sections.flatMap((section) => section.sessionIds);
  return orderedIds.flatMap((id) => {
    const session = sessionsById.get(id);
    return session ? [session] : [];
  });
}

function reconcileRefreshedMemberships(
  fresh: readonly WorkspaceConversationRailMembership[],
  current: readonly WorkspaceConversationRailMembership[]
): WorkspaceConversationRailMembership[] {
  const freshOwnerBySessionId = new Map<string, string>();
  for (const section of fresh) {
    for (const sessionId of section.sessionIds) {
      freshOwnerBySessionId.set(sessionId, section.id);
    }
  }
  return fresh.map((section) => {
    const previous = current.find((candidate) => candidate.id === section.id);
    if (!previous || previous.sessionIds.length <= section.sessionIds.length) {
      return section;
    }
    const freshIds = new Set(section.sessionIds);
    const preservedTail = previous.sessionIds.filter(
      (sessionId) =>
        !freshIds.has(sessionId) && !freshOwnerBySessionId.has(sessionId)
    );
    const sessionIds = [...section.sessionIds, ...preservedTail].slice(
      0,
      section.totalCount
    );
    const hasMore = sessionIds.length < section.totalCount;
    return {
      ...section,
      hasMore,
      nextCursor: hasMore ? (previous.nextCursor ?? section.nextCursor) : null,
      sessionIds
    };
  });
}

function sessionsForMemberships(
  sections: readonly WorkspaceConversationRailMembership[],
  sessions: readonly WorkspaceAgentSession[]
): WorkspaceAgentSession[] {
  const sessionsById = new Map(
    sessions.map((session) => [session.id, session])
  );
  return sections.flatMap((section) =>
    section.sessionIds.flatMap((sessionId) => {
      const session = sessionsById.get(sessionId);
      return session ? [session] : [];
    })
  );
}

function mergeSessions(
  current: readonly WorkspaceAgentSession[],
  next: readonly WorkspaceAgentSession[]
): WorkspaceAgentSession[] {
  const byId = new Map(current.map((session) => [session.id, session]));
  for (const session of next) byId.set(session.id, session);
  return [...byId.values()];
}

function uniqueIds(ids: readonly string[]): string[] {
  return [...new Set(ids)];
}
