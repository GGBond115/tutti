import { numberValue, recordValue } from "./normalizer.ts";
import type { ClaudeSDKSidecarEventEmitter } from "./protocol.ts";
import { stringValue } from "./runtimeValues.ts";
import type { TurnLifecycle } from "./turnLifecycle.ts";

type GoalUpdateType =
  | "thread_goal_update"
  | "thread_goal_completed"
  | "thread_goal_cleared";

/** Projects Claude's live `active_goal` signal into the sidecar contract. */
export class ClaudeGoalProjection {
  private readonly turns: TurnLifecycle;
  private readonly emit: ClaudeSDKSidecarEventEmitter;

  constructor(turns: TurnLifecycle, emit: ClaudeSDKSidecarEventEmitter) {
    this.turns = turns;
    this.emit = emit;
  }

  handle(message: Record<string, unknown>): boolean {
    if (stringValue(message.type) !== "active_goal") {
      return false;
    }
    this.handleActiveGoal(message);
    return true;
  }

  private handleActiveGoal(message: Record<string, unknown>): void {
    const rawValue = message.value;
    // Claude's current native runtime yields `value: undefined` when a Goal is
    // met. JSON transport omits that property even though the SDK type says
    // `null`. Both wire shapes mean the provider cleared its active hook.
    if (rawValue === undefined || rawValue === null) {
      this.emitObservation(
        this.turns.activeTurn?.goalAction === "clear"
          ? "thread_goal_cleared"
          : "thread_goal_completed"
      );
      return;
    }
    const value = recordValue(rawValue);
    const condition = stringValue(value?.condition);
    if (!value || !condition || !isNonNegativeNumber(value.iterations)) {
      return;
    }
    const goal: Record<string, unknown> = {
      objective: condition,
      status: "active",
      iterations: numberValue(value.iterations)
    };
    const reason = stringValue(value.last_reason);
    if (reason) {
      goal.reason = reason;
    }
    this.emitObservation("thread_goal_update", goal);
  }

  private emitObservation(
    updateType: GoalUpdateType,
    goal?: Record<string, unknown>
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
        source: "active_goal",
        updateType,
        ...(goal ? { goal } : {})
      }
    });
  }
}

function isNonNegativeNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}
