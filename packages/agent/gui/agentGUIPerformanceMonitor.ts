import {
  selectEngineSession,
  selectEngineTurn,
  selectPendingActivations,
  selectPendingSubmits,
  type AgentSessionEngine,
  type AgentSessionEngineState
} from "@tutti-os/agent-activity-core";

const MAX_RETAINED_PERFORMANCE_RECORDS = 512;

export type AgentGUIPerformanceDurationBucket =
  | "lt_1s"
  | "1s_to_3s"
  | "3s_to_10s"
  | "10s_to_30s"
  | "30s_to_60s"
  | "gte_60s";

export type AgentGUIFirstTokenKind = "other" | "plan" | "reasoning" | "text";

interface AgentGUIPerformanceEventBase {
  agentSessionId: string;
  durationBucket: AgentGUIPerformanceDurationBucket;
  durationMs: number;
  observedAtUnixMs: number;
  operationId: string;
  provider: string;
  startedAtUnixMs: number;
  workspaceId: string;
}

export type AgentGUIPerformanceEvent =
  | (AgentGUIPerformanceEventBase & {
      errorCategory?: string;
      hasInitialPrompt: boolean;
      mode: "existing" | "new";
      outcome: "confirmed" | "failed";
      type: "session_activation_settled";
    })
  | (AgentGUIPerformanceEventBase & {
      errorCategory?: string;
      outcome: "accepted" | "failed";
      queued: boolean;
      source: "activation" | "submit";
      turnId: string | null;
      type: "prompt_admission_settled";
    })
  | (AgentGUIPerformanceEventBase & {
      firstTokenKind: AgentGUIFirstTokenKind;
      queued: boolean;
      source: "activation" | "submit";
      turnId: string;
      type: "prompt_first_token_received";
    })
  | (AgentGUIPerformanceEventBase & {
      errorCategory?: string;
      outcome: "canceled" | "completed" | "failed" | "interrupted";
      source: "activation" | "submit";
      turnId: string;
      type: "turn_settled";
    });

export interface AgentGUIPerformanceMonitor {
  dispose(): void;
}

interface PromptAttempt {
  agentSessionId: string;
  allowUnboundTurnMatch: boolean;
  operationId: string;
  queued: boolean;
  source: "activation" | "submit";
  startedAtUnixMs: number;
  turnId: string | null;
}

interface FirstTokenObservation {
  agentSessionId: string;
  firstTokenKind: AgentGUIFirstTokenKind;
  observedAtUnixMs: number;
  turnId: string;
}

interface TurnContext extends Omit<
  PromptAttempt,
  "allowUnboundTurnMatch" | "turnId"
> {
  turnId: string;
}

export function createAgentGUIPerformanceMonitor(input: {
  engine: AgentSessionEngine;
  nowUnixMs?: () => number;
  onEvent: (event: AgentGUIPerformanceEvent) => void;
  subscribeSessionEvents: (listener: (event: unknown) => void) => () => void;
}): AgentGUIPerformanceMonitor {
  const workspaceId = input.engine.identity.workspaceId.trim();
  const nowUnixMs = input.nowUnixMs ?? Date.now;
  const attemptsByOperationId = new Map<string, PromptAttempt>();
  const attemptsByTurnKey = new Map<string, PromptAttempt>();
  const observationsByTurnKey = new Map<string, FirstTokenObservation>();
  const turnContexts = new Map<string, TurnContext>();
  const observedActivationRecords = new WeakSet<object>();
  const observedSubmitRecords = new WeakSet<object>();
  const seenPromptAttempts = new Set<string>();
  const reportedActivationSettlements = new Set<string>();
  const reportedPromptSettlements = new Set<string>();
  const reportedTurnSettlements = new Set<string>();
  let disposed = false;

  const emit = (event: AgentGUIPerformanceEvent): void => {
    try {
      input.onEvent(event);
    } catch (error) {
      // Performance reporting must never affect the Agent runtime.
      console.error("[agent-gui] performance event sink failed", error);
    }
  };

  const providerFor = (
    state: AgentSessionEngineState,
    agentSessionId: string
  ): string =>
    selectEngineSession(state, agentSessionId)?.provider?.trim() || "unknown";

  const clearOrphanedObservations = (agentSessionId: string): void => {
    const hasUnboundAttempt = [...attemptsByOperationId.values()].some(
      (attempt) =>
        attempt.agentSessionId === agentSessionId && attempt.turnId === null
    );
    if (hasUnboundAttempt) return;
    for (const [key, observation] of observationsByTurnKey) {
      if (observation.agentSessionId === agentSessionId) {
        observationsByTurnKey.delete(key);
      }
    }
  };

  const removeAttempt = (attempt: PromptAttempt): void => {
    attemptsByOperationId.delete(attempt.operationId);
    if (attempt.turnId) {
      attemptsByTurnKey.delete(
        performanceTurnKey(attempt.agentSessionId, attempt.turnId)
      );
    }
    clearOrphanedObservations(attempt.agentSessionId);
  };

  const reportFirstToken = (
    attempt: PromptAttempt,
    observation: FirstTokenObservation,
    state: AgentSessionEngineState
  ): void => {
    const key = performanceTurnKey(
      observation.agentSessionId,
      observation.turnId
    );
    turnContexts.set(key, turnContext(attempt, observation.turnId));
    trimMap(turnContexts);
    removeAttempt(attempt);
    observationsByTurnKey.delete(key);
    const duration = agentGUIPerformanceDuration(
      observation.observedAtUnixMs - attempt.startedAtUnixMs
    );
    emit({
      agentSessionId: attempt.agentSessionId,
      ...duration,
      firstTokenKind: observation.firstTokenKind,
      observedAtUnixMs: observation.observedAtUnixMs,
      operationId: attempt.operationId,
      provider: providerFor(state, attempt.agentSessionId),
      queued: attempt.queued,
      source: attempt.source,
      startedAtUnixMs: attempt.startedAtUnixMs,
      turnId: observation.turnId,
      type: "prompt_first_token_received",
      workspaceId
    });
  };

  const bindTurn = (
    attempt: PromptAttempt,
    turnId: string,
    state: AgentSessionEngineState
  ): void => {
    const normalizedTurnId = turnId.trim();
    if (!normalizedTurnId) return;
    attempt.turnId = normalizedTurnId;
    const key = performanceTurnKey(attempt.agentSessionId, normalizedTurnId);
    attemptsByTurnKey.set(key, attempt);
    turnContexts.set(key, turnContext(attempt, normalizedTurnId));
    trimMap(attemptsByTurnKey);
    trimMap(turnContexts);
    const observation = observationsByTurnKey.get(key);
    if (observation) {
      reportFirstToken(attempt, observation, state);
    } else {
      clearOrphanedObservations(attempt.agentSessionId);
    }
  };

  const trackPromptAttempt = (attempt: PromptAttempt): PromptAttempt | null => {
    if (seenPromptAttempts.has(attempt.operationId)) {
      return attemptsByOperationId.get(attempt.operationId) ?? null;
    }
    remember(seenPromptAttempts, attempt.operationId);
    attemptsByOperationId.set(attempt.operationId, attempt);
    trimMap(attemptsByOperationId);
    return attempt;
  };

  const reportState = (): void => {
    if (disposed) return;
    const state = input.engine.getSnapshot();
    const observedAtUnixMs = nowUnixMs();

    for (const activation of selectPendingActivations(state)) {
      if (observedActivationRecords.has(activation)) continue;
      observedActivationRecords.add(activation);
      const startedAtUnixMs = performanceStartedAt(
        activation.submitDiagnostics?.submittedAtUnixMs,
        activation.requestedAtUnixMs
      );
      const hasInitialPrompt = (activation.content?.length ?? 0) > 0;
      const operationId =
        activation.mode === "new" && activation.clientSubmitId
          ? activation.clientSubmitId
          : activation.requestId;
      const attempt = hasInitialPrompt
        ? trackPromptAttempt({
            agentSessionId: activation.agentSessionId,
            allowUnboundTurnMatch: activation.mode === "new",
            operationId,
            queued: activation.submitDiagnostics?.queued === true,
            source: "activation",
            startedAtUnixMs,
            turnId: null
          })
        : null;
      if (activation.status !== "confirmed" && activation.status !== "failed") {
        continue;
      }
      const duration = agentGUIPerformanceDuration(
        observedAtUnixMs - startedAtUnixMs
      );
      if (!reportedActivationSettlements.has(activation.requestId)) {
        remember(reportedActivationSettlements, activation.requestId);
        emit({
          agentSessionId: activation.agentSessionId,
          ...duration,
          ...(activation.status === "failed"
            ? { errorCategory: activation.errorCode ?? "runtime" }
            : {}),
          hasInitialPrompt,
          mode: activation.mode,
          observedAtUnixMs,
          operationId: activation.requestId,
          outcome: activation.status,
          provider: providerFor(state, activation.agentSessionId),
          startedAtUnixMs,
          type: "session_activation_settled",
          workspaceId
        });
      }
      if (hasInitialPrompt && !reportedPromptSettlements.has(operationId)) {
        remember(reportedPromptSettlements, operationId);
        emit({
          agentSessionId: activation.agentSessionId,
          ...duration,
          ...(activation.status === "failed"
            ? { errorCategory: activation.errorCode ?? "runtime" }
            : {}),
          observedAtUnixMs,
          operationId,
          outcome: activation.status === "confirmed" ? "accepted" : "failed",
          provider: providerFor(state, activation.agentSessionId),
          queued: activation.submitDiagnostics?.queued === true,
          source: "activation",
          startedAtUnixMs,
          turnId: null,
          type: "prompt_admission_settled",
          workspaceId
        });
      }
      if (activation.status === "failed" && attempt) {
        removeAttempt(attempt);
      }
    }

    for (const submit of selectPendingSubmits(state)) {
      if (observedSubmitRecords.has(submit)) continue;
      observedSubmitRecords.add(submit);
      const startedAtUnixMs = performanceStartedAt(
        submit.submitDiagnostics?.submittedAtUnixMs,
        submit.requestedAtUnixMs
      );
      const attempt = trackPromptAttempt({
        agentSessionId: submit.agentSessionId,
        allowUnboundTurnMatch: false,
        operationId: submit.clientSubmitId,
        queued: submit.submitDiagnostics?.queued === true,
        source: "submit",
        startedAtUnixMs,
        turnId: null
      });
      if (attempt && submit.turnId && attempt.turnId !== submit.turnId) {
        bindTurn(attempt, submit.turnId, state);
      }
      if (
        submit.status !== "accepted" &&
        submit.status !== "confirmed" &&
        submit.status !== "failed"
      ) {
        continue;
      }
      if (!reportedPromptSettlements.has(submit.clientSubmitId)) {
        remember(reportedPromptSettlements, submit.clientSubmitId);
        const duration = agentGUIPerformanceDuration(
          observedAtUnixMs - startedAtUnixMs
        );
        emit({
          agentSessionId: submit.agentSessionId,
          ...duration,
          ...(submit.status === "failed"
            ? { errorCategory: submit.errorCode ?? "runtime" }
            : {}),
          observedAtUnixMs,
          operationId: submit.clientSubmitId,
          outcome: submit.status === "failed" ? "failed" : "accepted",
          provider: providerFor(state, submit.agentSessionId),
          queued: submit.submitDiagnostics?.queued === true,
          source: "submit",
          startedAtUnixMs,
          turnId: submit.turnId,
          type: "prompt_admission_settled",
          workspaceId
        });
      }
      if (submit.status === "failed" && attempt) {
        removeAttempt(attempt);
      }
    }

    for (const context of turnContexts.values()) {
      const turn = selectEngineTurn(
        state,
        context.agentSessionId,
        context.turnId
      );
      if (!turn) continue;
      const key = performanceTurnKey(turn.agentSessionId, turn.turnId);
      if (
        turn.phase !== "settled" ||
        !turn.outcome ||
        reportedTurnSettlements.has(key)
      ) {
        continue;
      }
      remember(reportedTurnSettlements, key);
      const settledAtUnixMs = turn.settledAtUnixMs ?? turn.updatedAtUnixMs;
      const duration = agentGUIPerformanceDuration(
        settledAtUnixMs - turn.startedAtUnixMs
      );
      emit({
        agentSessionId: turn.agentSessionId,
        ...duration,
        ...(turn.outcome === "failed"
          ? { errorCategory: turn.error?.code ?? "runtime" }
          : {}),
        observedAtUnixMs,
        operationId: context.operationId,
        outcome: turn.outcome,
        provider: providerFor(state, turn.agentSessionId),
        source: context.source,
        startedAtUnixMs: turn.startedAtUnixMs,
        turnId: turn.turnId,
        type: "turn_settled",
        workspaceId
      });
      const attempt = attemptsByTurnKey.get(key);
      if (attempt) removeAttempt(attempt);
      observationsByTurnKey.delete(key);
      turnContexts.delete(key);
    }
  };

  const observeSessionEvent = (event: unknown): void => {
    if (disposed) return;
    const observation = firstTokenObservation(event, workspaceId, nowUnixMs());
    if (!observation) return;
    const key = performanceTurnKey(
      observation.agentSessionId,
      observation.turnId
    );
    const state = input.engine.getSnapshot();
    const exactAttempt = attemptsByTurnKey.get(key);
    if (exactAttempt) {
      reportFirstToken(exactAttempt, observation, state);
      return;
    }
    const unboundActivationAttempts = [
      ...attemptsByOperationId.values()
    ].filter(
      (attempt) =>
        attempt.allowUnboundTurnMatch &&
        attempt.agentSessionId === observation.agentSessionId &&
        attempt.turnId === null
    );
    if (unboundActivationAttempts.length === 1) {
      reportFirstToken(unboundActivationAttempts[0]!, observation, state);
      return;
    }
    const hasUnboundSubmit = [...attemptsByOperationId.values()].some(
      (attempt) =>
        !attempt.allowUnboundTurnMatch &&
        attempt.agentSessionId === observation.agentSessionId &&
        attempt.turnId === null
    );
    if (hasUnboundSubmit && !observationsByTurnKey.has(key)) {
      observationsByTurnKey.set(key, observation);
      trimMap(observationsByTurnKey);
    }
  };

  reportState();
  const releaseEngine = input.engine.subscribe(reportState);
  const releaseSessionEvents =
    input.subscribeSessionEvents(observeSessionEvent);

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      releaseEngine();
      releaseSessionEvents();
      attemptsByOperationId.clear();
      attemptsByTurnKey.clear();
      observationsByTurnKey.clear();
      turnContexts.clear();
      seenPromptAttempts.clear();
      reportedActivationSettlements.clear();
      reportedPromptSettlements.clear();
      reportedTurnSettlements.clear();
    }
  };
}

export function agentGUIPerformanceDuration(durationMs: number): {
  durationBucket: AgentGUIPerformanceDurationBucket;
  durationMs: number;
} {
  const normalizedDurationMs = Number.isFinite(durationMs)
    ? Math.max(0, durationMs)
    : 0;
  const durationBucket =
    normalizedDurationMs < 1_000
      ? "lt_1s"
      : normalizedDurationMs < 3_000
        ? "1s_to_3s"
        : normalizedDurationMs < 10_000
          ? "3s_to_10s"
          : normalizedDurationMs < 30_000
            ? "10s_to_30s"
            : normalizedDurationMs < 60_000
              ? "30s_to_60s"
              : "gte_60s";
  return { durationBucket, durationMs: normalizedDurationMs };
}

function firstTokenObservation(
  event: unknown,
  workspaceId: string,
  observedAtUnixMs: number
): FirstTokenObservation | null {
  const record = asRecord(event);
  if (stringField(record, "eventType") !== "message_delta") return null;
  const data = asRecord(record?.data);
  const eventWorkspaceId =
    stringField(record, "workspaceId") ?? stringField(data, "workspaceId");
  if (eventWorkspaceId !== workspaceId) return null;
  const role = stringField(data, "role")?.toLowerCase();
  if (role !== "assistant" && role !== "agent") return null;
  const content = asRecord(data?.content);
  if (!content || !deltaContentHasText(content)) return null;
  const agentSessionId =
    stringField(record, "agentSessionId") ??
    stringField(data, "agentSessionId");
  const turnId = stringField(data, "turnId") ?? stringField(record, "turnId");
  if (!agentSessionId || !turnId) return null;
  return {
    agentSessionId,
    firstTokenKind: normalizeFirstTokenKind(stringField(data, "kind")),
    observedAtUnixMs,
    turnId
  };
}

function deltaContentHasText(content: Record<string, unknown>): boolean {
  if (content.operation === "append_text") {
    return typeof content.text === "string" && content.text.trim().length > 0;
  }
  if (content.operation !== "set") return false;
  const pending: unknown[] = [content.value];
  let inspected = 0;
  while (pending.length > 0 && inspected < 100) {
    const value = pending.pop();
    inspected += 1;
    if (typeof value === "string" && value.trim()) return true;
    if (Array.isArray(value)) {
      pending.push(...value);
      continue;
    }
    const record = asRecord(value);
    if (!record) continue;
    for (const key of ["text", "content", "value", "parts", "blocks"]) {
      if (Object.hasOwn(record, key)) pending.push(record[key]);
    }
  }
  return false;
}

function normalizeFirstTokenKind(
  kind: string | undefined
): AgentGUIFirstTokenKind {
  const normalized = kind?.trim().toLowerCase().replaceAll("-", "_");
  if (
    normalized === "text" ||
    normalized === "reasoning" ||
    normalized === "plan"
  ) {
    return normalized;
  }
  return "other";
}

function performanceStartedAt(
  submittedAtUnixMs: number | undefined,
  requestedAtUnixMs: number
): number {
  return typeof submittedAtUnixMs === "number" &&
    Number.isFinite(submittedAtUnixMs)
    ? submittedAtUnixMs
    : requestedAtUnixMs;
}

function turnContext(attempt: PromptAttempt, turnId: string): TurnContext {
  return {
    agentSessionId: attempt.agentSessionId,
    operationId: attempt.operationId,
    queued: attempt.queued,
    source: attempt.source,
    startedAtUnixMs: attempt.startedAtUnixMs,
    turnId
  };
}

function performanceTurnKey(agentSessionId: string, turnId: string): string {
  return `${agentSessionId}\u0000${turnId}`;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : null;
}

function stringField(
  value: Record<string, unknown> | null,
  key: string
): string | undefined {
  const field = value?.[key];
  return typeof field === "string" && field.trim() ? field.trim() : undefined;
}

function remember(set: Set<string>, key: string): void {
  set.add(key);
  while (set.size > MAX_RETAINED_PERFORMANCE_RECORDS) {
    const oldest = set.values().next().value;
    if (typeof oldest !== "string") break;
    set.delete(oldest);
  }
}

function trimMap<TKey, TValue>(map: Map<TKey, TValue>): void {
  while (map.size > MAX_RETAINED_PERFORMANCE_RECORDS) {
    const oldest = map.keys().next().value;
    if (oldest === undefined) break;
    map.delete(oldest);
  }
}
