import {
  AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
  agentActivitySessionMessageWindowFromDescendingPage,
  createAgentActivitySnapshotProjector,
  createAgentSessionEngine,
  dispatchSessionMutation,
  selectRootAgentActivitySessions,
  type AgentActivitySessionSettings,
  type AgentActivityInteraction,
  type AgentActivitySession,
  type AgentSessionEngine,
  type EngineExternalCommand
} from "@tutti-os/agent-activity-core";
import {
  agentActivityMessageFromTuttidMessage,
  agentActivitySessionFromTuttidSession,
  agentActivityTurnFromTuttidTurn
} from "@tutti-os/agent-activity-tuttid-adapter";
import type {
  TuttidClient,
  WorkspaceSummary
} from "@tutti-os/client-tuttid-ts";
import type { AgentDirectoryService } from "./agentDirectoryService";
import type { ComposerDraftService } from "./composerDraftService";
import { ObservableService } from "./observableService";
import {
  resolvePendingSubmission,
  type PendingSubmission
} from "./pendingSubmission";
import type { ClockPort, DeviceLinkPort } from "./servicePorts";
import { executeWorkspaceActivityCommand } from "./workspaceActivityCommandAdapter";
import {
  projectWorkspaceActivitySnapshot,
  resolveWorkspaceComposerTarget
} from "./workspaceActivityProjection";
import type {
  WorkspaceConversationRailService,
  WorkspaceConversationRailSnapshot
} from "./workspaceConversationRailService";
import { createMobileActivityCommandId } from "./workspaceActivityCommandSupport";
import type { WorkspaceActivitySnapshot } from "./workspaceActivityTypes";
import { WorkspaceAgentLiveLane } from "./workspaceAgentLiveLane";
import type { WorkspaceNavigationService } from "./workspaceNavigationService";
import { WorkspaceMediaService } from "./workspaceMediaService";

export type { WorkspaceActivitySnapshot } from "./workspaceActivityTypes";

const MESSAGE_POLL_MS = 1_000;
const COMMAND_TIMEOUT_MS = 30_000;
const PENDING_EXPIRY_MS = 60_000;
const MESSAGE_PAGE_SIZE = 100;

export class WorkspaceActivityService extends ObservableService<WorkspaceActivitySnapshot> {
  readonly _serviceBrand: undefined;
  readonly media: WorkspaceMediaService;
  private readonly engine: AgentSessionEngine;
  private readonly liveLane: WorkspaceAgentLiveLane;
  private readonly projectActivity: (
    state: ReturnType<AgentSessionEngine["getSnapshot"]>
  ) => WorkspaceActivitySnapshot["activity"];
  private disposed = false;
  private paused = false;
  private initializePromise: Promise<void> | null = null;
  private messagesInFlight: Promise<void> | null = null;
  private messagePollTask: { cancel(): void } | null = null;
  private errorCode: "request_failed" | null = null;
  private loading = true;
  private observedSelectedSessionId: string | null = null;
  private lastRailSessions: WorkspaceConversationRailSnapshot["sessions"] = [];
  private previousConversation: WorkspaceActivitySnapshot["conversation"] =
    null;
  private snapshotCache: WorkspaceActivitySnapshot | null = null;
  private readonly disposables: Array<() => void> = [];
  private readonly pendingSubmissionsByDraftKey = new Map<
    string,
    PendingSubmission
  >();
  private readonly ambiguousDraftKeys = new Set<string>();

  constructor(
    readonly workspace: WorkspaceSummary,
    private readonly client: TuttidClient,
    private readonly directory: AgentDirectoryService,
    private readonly navigation: WorkspaceNavigationService,
    private readonly drafts: ComposerDraftService,
    private readonly rail: WorkspaceConversationRailService,
    private readonly clock: ClockPort,
    private readonly currentUserId: string,
    deviceLink?: DeviceLinkPort
  ) {
    super();
    this.media = new WorkspaceMediaService(workspace.id, client);
    this.projectActivity = createAgentActivitySnapshotProjector(workspace.id);
    this.engine = createAgentSessionEngine({
      clock: { nowUnixMs: () => this.clock.now() },
      commandPort: {
        execute: (command, options) =>
          this.executeCommand(command, options?.signal)
      },
      identity: {
        origin: AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
        workspaceId: workspace.id
      },
      scheduler: {
        schedule: (delayMs, task) => this.clock.schedule(delayMs, task)
      }
    });
    this.liveLane = new WorkspaceAgentLiveLane({
      clock: this.clock,
      deviceLink,
      engine: this.engine,
      isAvailable: () => !this.disposed && !this.paused,
      loadSelectedMessages: (authoritative) =>
        this.loadSelectedMessages(authoritative),
      navigation: this.navigation,
      onActivityChanged: () => this.onDependencyChanged(),
      onConnectionChanged: (connected) => {
        if (connected) {
          this.messagePollTask?.cancel();
          this.messagePollTask = null;
        } else {
          this.scheduleMessagesPoll();
        }
      },
      rail: this.rail,
      readCanonicalActivity: () =>
        this.projectActivity(this.engine.getSnapshot()),
      reconcileWorkspace: () => this.reconcileWorkspace(),
      workspaceId: this.workspace.id
    });
    this.disposables.push(
      this.engine.subscribe(() => this.onDependencyChanged()),
      this.navigation.subscribe(() => {
        const selectedSessionId =
          this.navigation.getSnapshot().selectedAgentSessionId;
        const selectionChanged =
          selectedSessionId !== this.observedSelectedSessionId;
        this.observedSelectedSessionId = selectedSessionId;
        this.onDependencyChanged();
        if (selectionChanged) void this.loadSelectedMessages(true);
        this.loadComposerOptions();
      }),
      this.drafts.subscribe(() => this.onDependencyChanged()),
      this.rail.subscribe(() => {
        const rail = this.rail.getSnapshot();
        if (
          rail.status === "ready" &&
          rail.sessions !== this.lastRailSessions &&
          !this.disposed &&
          !this.paused
        ) {
          this.lastRailSessions = rail.sessions;
          this.applySessionSnapshot(
            rail.sessions.map((session) => this.mapSession(session))
          );
        }
        this.onDependencyChanged();
      }),
      this.directory.subscribe(() => {
        this.navigation.reconcileTargetIds(
          this.directory.getSnapshot().targets.map((target) => target.id)
        );
        this.onDependencyChanged();
        this.loadComposerOptions();
      })
    );
  }

  getSnapshot = (): WorkspaceActivitySnapshot => {
    if (this.snapshotCache) return this.snapshotCache;
    const state = this.engine.getSnapshot();
    const activity = this.liveLane.project(this.projectActivity(state));
    const railSnapshot = this.rail.getSnapshot();
    const navigation = this.navigation.getSnapshot();
    const draftKey = navigation.creating
      ? "new"
      : (navigation.selectedAgentSessionId ?? "none");
    const draftSettings = navigation.creating
      ? this.drafts.getSettings(navigation.selectedAgentTargetId ?? "")
      : {};
    this.snapshotCache = projectWorkspaceActivitySnapshot({
      activity,
      ambiguousSubmission: this.ambiguousDraftKeys.has(draftKey),
      draftSettings,
      draft: this.drafts.get(draftKey),
      errorCode: this.errorCode,
      loading: this.loading,
      navigation,
      previousConversation: this.previousConversation,
      rail: railSnapshot,
      state,
      targets: this.directory.getSnapshot().targets,
      workspaceId: this.workspace.id
    });
    this.previousConversation = this.snapshotCache.conversation;
    return this.snapshotCache;
  };

  start(): Promise<void> {
    if (this.initializePromise) return this.initializePromise;
    if (this.disposed) return Promise.resolve();
    this.engine.dispatch({
      status: "connected",
      type: "engine/connectionChanged",
      workspaceId: this.workspace.id
    });
    for (const session of this.getSnapshot().activity.sessions) {
      this.engine.dispatch({
        agentSessionId: session.agentSessionId,
        availability: { state: "available" },
        type: "session/runtimeAvailabilityChanged"
      });
    }
    this.initializePromise = Promise.all([
      this.directory.load(),
      this.rail.start()
    ])
      .then(() => undefined)
      .finally(() => {
        if (this.disposed) return;
        this.loading = false;
        this.loadComposerOptions();
        this.liveLane.start();
        this.onDependencyChanged();
      });
    return this.initializePromise;
  }

  setDraft(value: string): void {
    const navigation = this.navigation.getSnapshot();
    this.drafts.set(
      navigation.creating
        ? "new"
        : (navigation.selectedAgentSessionId ?? "none"),
      value
    );
  }

  selectSession(agentSessionId: string): void {
    this.navigation.selectSession(agentSessionId);
  }

  selectTarget(agentTargetId: string): void {
    this.navigation.selectTarget(agentTargetId);
  }

  updateComposerSettings(settings: AgentActivitySessionSettings): void {
    const target = this.currentComposerTarget();
    if (!target || Object.keys(settings).length === 0) return;
    const navigation = this.navigation.getSnapshot();
    if (navigation.creating) {
      this.drafts.setSettings(target.agentTargetId, settings);
      this.loadComposerOptions({ force: true });
      return;
    }
    if (!target.agentSessionId) return;
    this.engine.dispatch({
      agentSessionId: target.agentSessionId,
      commandId: createMobileActivityCommandId(),
      settings,
      timeoutMs: COMMAND_TIMEOUT_MS,
      type: "session/settingsUpdateRequested",
      workspaceId: this.workspace.id
    });
  }

  startCreating(): void {
    const targets = this.directory.getSnapshot().targets;
    this.navigation.startCreating(targets.length === 1 ? targets[0]!.id : null);
  }

  loadMoreSessions(sectionId: string): Promise<void> {
    return this.rail.loadMore(sectionId);
  }

  refreshSessions(): Promise<void> {
    return this.rail.refresh();
  }

  async toggleSessionPinned(agentSessionId: string): Promise<void> {
    const session = this.getSnapshot().activity.sessions.find(
      (candidate) => candidate.agentSessionId === agentSessionId
    );
    if (!session) return;
    try {
      await dispatchSessionMutation(this.engine, {
        agentSessionId,
        mutationId: createMobileActivityCommandId(),
        pinned: session.pinnedAtUnixMs == null,
        timeoutMs: COMMAND_TIMEOUT_MS,
        type: "session/pinRequested",
        workspaceId: this.workspace.id
      });
      await this.rail.reconcile();
      this.errorCode = null;
    } catch {
      if (!this.disposed) this.errorCode = "request_failed";
    }
    this.onDependencyChanged();
  }

  async renameSession(agentSessionId: string, title: string): Promise<void> {
    const normalizedTitle = title.trim();
    if (!normalizedTitle) return;
    try {
      const session = await this.client.updateWorkspaceAgentSessionTitle(
        this.workspace.id,
        agentSessionId,
        { title: normalizedTitle }
      );
      this.engine.dispatch({
        session: this.mapSession(session),
        type: "session/upserted"
      });
      await this.rail.reconcile();
      this.errorCode = null;
    } catch {
      if (!this.disposed) this.errorCode = "request_failed";
    }
    this.onDependencyChanged();
  }

  async deleteSession(agentSessionId: string): Promise<void> {
    try {
      await dispatchSessionMutation(this.engine, {
        agentSessionIds: [agentSessionId],
        mutationId: createMobileActivityCommandId(),
        timeoutMs: COMMAND_TIMEOUT_MS,
        type: "sessions/deleteRequested",
        workspaceId: this.workspace.id
      });
      await this.rail.reconcile();
      this.errorCode = null;
    } catch {
      if (!this.disposed) this.errorCode = "request_failed";
    }
    this.onDependencyChanged();
  }

  async send(): Promise<void> {
    const snapshot = this.getSnapshot();
    const text = snapshot.draft.trim();
    if (!text || snapshot.sending) return;
    if (snapshot.creating && !snapshot.selectedAgentTargetId) return;
    const draftKey = snapshot.creating
      ? "new"
      : (snapshot.selectedAgentSessionId ?? "none");
    const submission = resolvePendingSubmission(
      this.pendingSubmissionsByDraftKey.get(draftKey) ?? null,
      {
        agentSessionId: snapshot.selectedAgentSessionId,
        agentTargetId: snapshot.selectedAgentTargetId,
        creating: snapshot.creating,
        text
      }
    );
    if (this.ambiguousDraftKeys.has(draftKey)) {
      await this.reconcileWorkspace().catch(() => undefined);
      this.reconcilePendingSubmissions();
      if (!this.pendingSubmissionsByDraftKey.has(draftKey)) return;
      this.dismissPendingSubmission(submission);
      this.ambiguousDraftKeys.delete(draftKey);
      this.errorCode = null;
    }
    this.pendingSubmissionsByDraftKey.set(draftKey, submission);
    const now = this.clock.now();
    const content = [{ text, type: "text" as const }];
    const submitDiagnostics = {
      blockCount: 1,
      promptLength: text.length,
      source: "mobile",
      submittedAtUnixMs: now
    };
    if (snapshot.creating) {
      this.engine.dispatch({
        agentSessionId: submission.agentSessionId,
        agentTargetId: submission.agentTargetId!,
        clientSubmitId: submission.clientSubmitId,
        content,
        expiresAtUnixMs: now + PENDING_EXPIRY_MS,
        initialTurnExpected: true,
        mode: "new",
        requestId: submission.clientSubmitId,
        requestedAtUnixMs: now,
        runtimeContent: content,
        settings: snapshot.composerSettings,
        submitDiagnostics,
        type: "activation/requested",
        visible: true,
        workspaceId: this.workspace.id
      });
      this.drafts.clear("new");
      return;
    }
    if (!snapshot.selectedAgentSessionId) return;
    this.engine.dispatch({
      agentSessionId: snapshot.selectedAgentSessionId,
      clientSubmitId: submission.clientSubmitId,
      content,
      expiresAtUnixMs: now + PENDING_EXPIRY_MS,
      requestedAtUnixMs: now,
      routing: "auto",
      runtimeContent: content,
      submitDiagnostics,
      type: "submit/requested",
      workspaceId: this.workspace.id
    });
    this.drafts.clear(snapshot.selectedAgentSessionId);
  }

  stop(): void {
    const selected = this.getSnapshot().selectedSession;
    if (!selected) return;
    const now = this.clock.now();
    this.engine.dispatch({
      agentSessionId: selected.agentSessionId,
      awaitingTurnExpiresAtUnixMs: now + PENDING_EXPIRY_MS,
      commandId: createMobileActivityCommandId(),
      timeoutMs: COMMAND_TIMEOUT_MS,
      type: "session/stopRequested",
      workspaceId: this.workspace.id
    });
  }

  respondToInteraction(
    interaction: AgentActivityInteraction,
    input: {
      action?: string;
      optionId?: string;
      payload?: Readonly<Record<string, unknown>>;
    }
  ): void {
    this.engine.dispatch({
      ...input,
      agentSessionId: interaction.agentSessionId,
      commandId: createMobileActivityCommandId(),
      requestId: interaction.requestId,
      retry: false,
      timeoutMs: COMMAND_TIMEOUT_MS,
      turnId: interaction.turnId,
      type: "interaction/responseRequested",
      workspaceId: this.workspace.id
    });
  }

  async loadOlderMessages(): Promise<void> {
    const selected = this.navigation.getSnapshot().selectedAgentSessionId;
    if (!selected || this.messagesInFlight || this.paused || this.disposed)
      return;
    const window = this.projectActivity(this.engine.getSnapshot())
      .sessionMessageWindowsById?.[selected];
    if (!window?.hasOlderMessages || window.oldestLoadedVersion === null)
      return;
    await this.loadMessagePage(selected, {
      beforeVersion: window.oldestLoadedVersion,
      limit: MESSAGE_PAGE_SIZE,
      order: "desc"
    });
  }

  pause(): void {
    if (this.paused || this.disposed) return;
    this.paused = true;
    this.liveLane.stop();
    this.cancelPolls();
    this.rail.pause();
    this.engine.dispatch({
      status: "disconnected",
      type: "engine/connectionChanged",
      workspaceId: this.workspace.id
    });
    for (const session of this.getSnapshot().activity.sessions) {
      this.engine.dispatch({
        agentSessionId: session.agentSessionId,
        availability: {
          reason: "transport_unavailable",
          state: "blocked"
        },
        type: "session/runtimeAvailabilityChanged"
      });
    }
  }

  resume(): void {
    if (!this.paused || this.disposed) return;
    this.paused = false;
    this.rail.resume();
    this.liveLane.start();
    this.engine.dispatch({
      status: "connected",
      type: "engine/connectionChanged",
      workspaceId: this.workspace.id
    });
    this.engine.dispatch({
      type: "workspace/reconcileRequested",
      workspaceId: this.workspace.id
    });
    void this.loadSelectedMessages(true);
    this.scheduleMessagesPoll();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.liveLane.stop();
    this.cancelPolls();
    for (const dispose of this.disposables.splice(0)) dispose();
    this.pendingSubmissionsByDraftKey.clear();
    this.ambiguousDraftKeys.clear();
    this.engine.dispose();
    this.media.dispose();
    this.clearListeners();
  }

  private async loadSelectedMessages(authoritative: boolean): Promise<void> {
    const agentSessionId = this.navigation.getSnapshot().selectedAgentSessionId;
    if (this.messagesInFlight) return this.messagesInFlight;
    if (!agentSessionId || this.paused || this.disposed) return;
    const activity = this.projectActivity(this.engine.getSnapshot());
    const messages = activity.sessionMessagesById[agentSessionId] ?? [];
    const latestVersion = messages.reduce(
      (latest, message) => Math.max(latest, message.version),
      0
    );
    if (authoritative || latestVersion === 0) {
      await this.loadMessagePage(agentSessionId, {
        limit: MESSAGE_PAGE_SIZE,
        order: "desc"
      });
      return;
    }
    await this.loadMessagePage(agentSessionId, {
      afterVersion: latestVersion,
      order: "asc"
    });
  }

  private async loadMessagePage(
    agentSessionId: string,
    query: {
      afterVersion?: number;
      beforeVersion?: number;
      limit?: number;
      order: "asc" | "desc";
    }
  ): Promise<void> {
    if (this.messagesInFlight || this.paused || this.disposed) return;
    this.messagesInFlight = this.client
      .listWorkspaceAgentSessionMessages(
        this.workspace.id,
        agentSessionId,
        query
      )
      .then((page) => {
        if (
          this.disposed ||
          this.paused ||
          this.navigation.getSnapshot().selectedAgentSessionId !==
            agentSessionId
        )
          return;
        const messages = page.messages.map((message) =>
          agentActivityMessageFromTuttidMessage(this.workspace.id, message)
        );
        this.engine.dispatch({
          messages,
          ...(query.order === "desc"
            ? {
                sessionMessageWindows: [
                  {
                    agentSessionId,
                    ...agentActivitySessionMessageWindowFromDescendingPage({
                      ...page,
                      messages
                    })
                  }
                ]
              }
            : {}),
          type: "message/snapshotReceived",
          workspaceId: this.workspace.id
        });
        this.liveLane.reconcileMessages(agentSessionId);
        this.errorCode = null;
      })
      .catch(() => {
        if (!this.disposed) this.errorCode = "request_failed";
      })
      .finally(() => {
        this.messagesInFlight = null;
        this.onDependencyChanged();
        this.scheduleMessagesPoll();
      });
    return this.messagesInFlight;
  }

  private executeCommand(
    command: EngineExternalCommand,
    signal?: AbortSignal
  ): Promise<unknown> {
    return executeWorkspaceActivityCommand(
      {
        client: this.client,
        engine: this.engine,
        loadComposerOptions: (options) => this.loadComposerOptions(options),
        mapSession: (session) => this.mapSession(session),
        reconcileSession: (agentSessionId) =>
          this.reconcileSession(agentSessionId),
        reconcileWorkspace: () => this.reconcileWorkspace()
      },
      command,
      signal
    );
  }

  private async reconcileWorkspace(): Promise<unknown> {
    const rail = await this.rail.reconcile();
    const sessions = rail.sessions.map((session) => this.mapSession(session));
    if (this.disposed || this.paused) {
      throw new Error("mobile workspace activity is unavailable");
    }
    this.applySessionSnapshot(sessions);
    this.errorCode = null;
    return { sessions };
  }

  private async reconcileSession(agentSessionId: string): Promise<unknown> {
    const detail = await this.client.getWorkspaceAgentSession(
      this.workspace.id,
      agentSessionId
    );
    const session = this.mapSession(detail.session);
    this.engine.dispatch({ session, type: "session/upserted" });
    for (const turn of detail.turns) {
      this.engine.dispatch({
        turn: agentActivityTurnFromTuttidTurn(turn),
        type: "turn/upserted"
      });
    }
    await this.loadSelectedMessages(true);
    return { session };
  }

  private loadComposerOptions(options?: { force?: boolean }): void {
    if (this.disposed || this.paused) return;
    const target = this.currentComposerTarget();
    if (!target) return;
    this.engine.dispatch({
      commandId: createMobileActivityCommandId(),
      ...(target.cwd ? { cwd: target.cwd } : {}),
      ...(options?.force ? { force: true } : {}),
      provider: target.provider,
      settings: target.settings,
      targetKey: target.agentTargetId,
      type: "composerOptions/loadRequested",
      workspaceId: this.workspace.id
    });
  }

  private currentComposerTarget() {
    return resolveWorkspaceComposerTarget({
      activity: this.projectActivity(this.engine.getSnapshot()),
      getDraftSettings: (agentTargetId) =>
        this.drafts.getSettings(agentTargetId),
      navigation: this.navigation.getSnapshot(),
      targets: this.directory.getSnapshot().targets
    });
  }

  private scheduleMessagesPoll(): void {
    this.messagePollTask?.cancel();
    if (this.disposed || this.paused || this.liveLane.isConnected()) return;
    this.messagePollTask = this.clock.schedule(MESSAGE_POLL_MS, () => {
      this.messagePollTask = null;
      void this.loadSelectedMessages(false);
    });
  }

  private cancelPolls(): void {
    this.messagePollTask?.cancel();
    this.messagePollTask = null;
  }

  private applySessionSnapshot(sessions: AgentActivitySession[]): void {
    this.engine.dispatch({
      sessions,
      type: "session/snapshotReceived"
    });
    if (!this.paused) {
      for (const session of sessions) {
        this.engine.dispatch({
          agentSessionId: session.agentSessionId,
          availability: { state: "available" },
          type: "session/runtimeAvailabilityChanged"
        });
      }
    }
    const roots = selectRootAgentActivitySessions({ sessions }).filter(
      (session) => session.visible
    );
    this.navigation.reconcileSessionIds(
      roots.map((session) => session.agentSessionId)
    );
  }

  private onDependencyChanged(): void {
    this.reconcilePendingSubmissions();
    this.snapshotCache = null;
    this.media.sync(this.getSnapshot().conversation);
    this.emitChange();
  }

  private reconcilePendingSubmissions(): void {
    const state = this.engine.getSnapshot();
    for (const [draftKey, submission] of this.pendingSubmissionsByDraftKey) {
      if (submission.creating) {
        const session =
          state.sessionLifecycle.sessionsById[submission.agentSessionId];
        if (session) {
          this.pendingSubmissionsByDraftKey.delete(draftKey);
          this.ambiguousDraftKeys.delete(draftKey);
          this.errorCode = null;
          this.drafts.clear(draftKey);
          this.navigation.selectSession(submission.agentSessionId);
          continue;
        }
        const record =
          state.pendingIntents.activationsByRequestId[
            submission.clientSubmitId
          ];
        if (record?.status === "failed" || record?.status === "uncertain") {
          this.markSubmissionAmbiguous(draftKey, submission.text);
        }
        continue;
      }
      const record =
        state.pendingIntents.submitsByClientSubmitId[submission.clientSubmitId];
      if (record?.status === "accepted" || record?.status === "confirmed") {
        this.pendingSubmissionsByDraftKey.delete(draftKey);
        this.ambiguousDraftKeys.delete(draftKey);
        this.errorCode = null;
      } else if (
        record?.status === "failed" ||
        record?.status === "uncertain"
      ) {
        this.markSubmissionAmbiguous(draftKey, submission.text);
      }
    }
  }

  private markSubmissionAmbiguous(draftKey: string, text: string): void {
    this.ambiguousDraftKeys.add(draftKey);
    this.errorCode = "request_failed";
    if (!this.drafts.get(draftKey)) {
      this.drafts.set(draftKey, text);
    }
  }

  private dismissPendingSubmission(submission: PendingSubmission): void {
    if (submission.creating) {
      this.engine.dispatch({
        requestId: submission.clientSubmitId,
        type: "activation/dismissed"
      });
      return;
    }
    this.engine.dispatch({
      clientSubmitId: submission.clientSubmitId,
      type: "submit/dismissed"
    });
  }

  private mapSession(
    session: Parameters<typeof agentActivitySessionFromTuttidSession>[1]
  ): AgentActivitySession {
    return agentActivitySessionFromTuttidSession(this.workspace.id, session, {
      currentUserId: this.currentUserId
    });
  }
}
