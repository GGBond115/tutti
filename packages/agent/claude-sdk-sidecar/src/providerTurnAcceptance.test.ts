import assert from "node:assert/strict";
import test from "node:test";
import {
  ProviderTurnAcceptanceCoordinator,
  type ProviderTurnIdentityBindingDisposition
} from "./providerTurnAcceptance.ts";
import { ClaudeTurnBindingResolutionError } from "./sessionFork.ts";

test("provider acceptance retries transient transcript lag and binds once", async () => {
  let now = 0;
  let attempts = 0;
  const bindings: string[] = [];
  const checkpoints: string[] = [];
  const coordinator = new ProviderTurnAcceptanceCoordinator({
    cwd: "/repo",
    getProviderSessionId: () => "provider-session",
    resolveTarget: () => ({
      turnId: "turn-1",
      promptCorrelationId: "correlation-1",
      providerTurnId: bindings[0] ?? ""
    }),
    resolveBinding: async () => {
      attempts += 1;
      if (attempts < 3) {
        throw new ClaudeTurnBindingResolutionError("absent");
      }
      return {
        providerSessionId: "provider-session",
        providerTurnId: "provider-turn-1",
        providerCheckpointMessageId: "checkpoint-1"
      };
    },
    bindIdentity: (_turnId, providerTurnId) => {
      bindings.push(providerTurnId);
      return "bound";
    },
    emitCheckpoint: (_turnId, binding) =>
      checkpoints.push(binding.providerCheckpointMessageId),
    now: () => now,
    wait: async (delayMs) => {
      now += delayMs;
    }
  });

  await Promise.all([
    coordinator.ensure("streaming"),
    coordinator.ensure("waiting_approval"),
    coordinator.ensure("waiting_input")
  ]);

  assert.equal(attempts, 3);
  assert.deepEqual(bindings, ["provider-turn-1"]);
  assert.deepEqual(checkpoints, ["checkpoint-1"]);
  assert.equal(coordinator.phase("turn-1"), "waiting_input");
});

test("provider acceptance fails closed on ambiguous identity", async () => {
  const coordinator = createCoordinator({
    resolveBinding: async () => {
      throw new ClaudeTurnBindingResolutionError("ambiguous");
    }
  });

  await assert.rejects(
    coordinator.ensure("waiting_approval"),
    /proof is ambiguous/u
  );
});

test("provider acceptance cancellation aborts the shared identity flight", async () => {
  const coordinator = createCoordinator({
    resolveBinding: () => new Promise(() => {})
  });
  const pending = coordinator.ensure("waiting_input");

  coordinator.cancel("turn-1");

  await assert.rejects(pending, { name: "AbortError" });
});

function createCoordinator(options: {
  resolveBinding: ConstructorParameters<
    typeof ProviderTurnAcceptanceCoordinator
  >[0]["resolveBinding"];
}): ProviderTurnAcceptanceCoordinator {
  return new ProviderTurnAcceptanceCoordinator({
    cwd: "/repo",
    getProviderSessionId: () => "provider-session",
    resolveTarget: () => ({
      turnId: "turn-1",
      promptCorrelationId: "correlation-1",
      providerTurnId: ""
    }),
    resolveBinding: options.resolveBinding,
    bindIdentity: () =>
      "bound" satisfies ProviderTurnIdentityBindingDisposition,
    emitCheckpoint: () => {}
  });
}
