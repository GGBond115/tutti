import type { SessionStoreEntry } from "@anthropic-ai/claude-agent-sdk";
import { numberValue, recordValue } from "./normalizer.ts";
import type { ClaudeSDKSidecarEventEmitter } from "./protocol.ts";
import { stringValue } from "./runtimeValues.ts";
import type { TurnLifecycle } from "./turnLifecycle.ts";

type GoalUpdateType = "thread_goal_update" | "thread_goal_completed";

type GoalGeneration = {
  readonly operationId: string;
  readonly revision: number;
  readonly repairEpoch: number;
  readonly activatedAtUnixMs: number;
  readonly objective: string;
};

/** Projects Claude's durable goal-status records into the sidecar contract. */
export class ClaudeGoalProjection {
  private readonly turns: TurnLifecycle;
  private readonly emit: ClaudeSDKSidecarEventEmitter;
  private readonly observedEntryIds = new Set<string>();
  private currentGeneration: GoalGeneration | undefined;

  constructor(turns: TurnLifecycle, emit: ClaudeSDKSidecarEventEmitter) {
    this.turns = turns;
    this.emit = emit;
  }

  restoreGeneration(
    identity: Record<string, unknown> | undefined,
    goal: Record<string, unknown> | undefined
  ): void {
    const operationId = stringValue(identity?.operationId);
    const revision = numberValue(identity?.revision);
    const repairEpoch = numberValue(identity?.repairEpoch);
    const activatedAtUnixMs = numberValue(identity?.activatedAtUnixMs);
    const objective = stringValue(goal?.objective);
    if (
      !operationId ||
      !Number.isInteger(revision) ||
      revision <= 0 ||
      !Number.isInteger(repairEpoch) ||
      repairEpoch < 0 ||
      !objective ||
      stringValue(goal?.status) !== "active"
    ) {
      return;
    }
    this.currentGeneration = {
      operationId,
      revision,
      repairEpoch,
      activatedAtUnixMs:
        Number.isInteger(activatedAtUnixMs) && activatedAtUnixMs > 0
          ? activatedAtUnixMs
          : 0,
      objective
    };
  }

  observeTranscriptEntries(entries: readonly SessionStoreEntry[]): void {
    for (const entry of entries) {
      const attachment = recordValue(entry.attachment);
      if (
        stringValue(entry.type) !== "attachment" ||
        stringValue(attachment?.type) !== "goal_status"
      ) {
        continue;
      }
      const entryId = stringValue(entry.uuid);
      if (entryId && this.observedEntryIds.has(entryId)) {
        continue;
      }
      const objective = stringValue(attachment?.condition);
      if (!objective || typeof attachment?.met !== "boolean") {
        continue;
      }
      const occurredAtUnixMs = transcriptTimestampUnixMs(entry.timestamp);
      if (
        this.currentGeneration?.activatedAtUnixMs &&
        (occurredAtUnixMs === 0 ||
          occurredAtUnixMs < this.currentGeneration.activatedAtUnixMs)
      ) {
        continue;
      }
      if (entryId) {
        this.rememberEntry(entryId);
      }
      const activeTurn = this.turns.activeTurn;
      if (attachment.sentinel === true && attachment.met === false) {
        const operationId = activeTurn?.goalOperationId?.trim() ?? "";
        const revision = activeTurn?.goalRevision ?? 0;
        if (
          activeTurn?.goalAction !== "set" ||
          !operationId ||
          !Number.isInteger(revision) ||
          revision <= 0
        ) {
          continue;
        }
        this.currentGeneration = {
          operationId,
          revision,
          repairEpoch: activeTurn.goalRepairEpoch ?? 0,
          activatedAtUnixMs: occurredAtUnixMs,
          objective
        };
      }
      const generation = this.currentGeneration;
      if (!generation || generation.objective !== objective) {
        continue;
      }
      const failed = attachment.failed === true;
      const goal: Record<string, unknown> = {
        objective,
        status: attachment.met ? "complete" : failed ? "blocked" : "active"
      };
      copyNonNegativeInteger(attachment, goal, "iterations");
      copyNonNegativeInteger(attachment, goal, "durationMs");
      copyNonNegativeInteger(attachment, goal, "tokens");
      const reason = stringValue(attachment.reason);
      if (reason) {
        goal.reason = reason;
      }
      this.emitObservation(
        attachment.met ? "thread_goal_completed" : "thread_goal_update",
        generation,
        goal,
        occurredAtUnixMs
      );
      if (attachment.met || failed) {
        this.currentGeneration = undefined;
      }
    }
  }

  private rememberEntry(entryId: string): void {
    this.observedEntryIds.add(entryId);
    if (this.observedEntryIds.size <= 1024) {
      return;
    }
    const oldest = this.observedEntryIds.values().next().value;
    if (oldest) {
      this.observedEntryIds.delete(oldest);
    }
  }

  private emitObservation(
    updateType: GoalUpdateType,
    generation: GoalGeneration,
    goal: Record<string, unknown>,
    occurredAtUnixMs: number
  ): void {
    const activeTurn = this.turns.activeTurn;
    this.emit({
      type: "goal_observed",
      payload: {
        turnId: this.turns.activeId,
        ...(this.turns.lastProviderTurnId
          ? { providerTurnId: this.turns.lastProviderTurnId }
          : {}),
        ...(activeTurn?.goalAction ? { action: activeTurn.goalAction } : {}),
        goalOperationId: generation.operationId,
        goalRevision: generation.revision,
        goalRepairEpoch: generation.repairEpoch,
        ...(occurredAtUnixMs > 0 ? { occurredAtUnixMs } : {}),
        source: "transcript_mirror",
        updateType,
        goal
      }
    });
  }
}

function transcriptTimestampUnixMs(value: unknown): number {
  const timestamp = Date.parse(stringValue(value));
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : 0;
}

function copyNonNegativeInteger(
  source: Record<string, unknown>,
  target: Record<string, unknown>,
  key: string
): void {
  const raw = source[key];
  if (typeof raw !== "number" || !Number.isFinite(raw)) {
    return;
  }
  const value = numberValue(raw);
  if (Number.isInteger(value) && value >= 0) {
    target[key] = value;
  }
}
