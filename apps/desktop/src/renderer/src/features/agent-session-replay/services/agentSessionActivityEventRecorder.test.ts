import assert from "node:assert/strict";
import test from "node:test";
import type {
  EngineExternalCommand,
  EngineIntent
} from "@tutti-os/agent-activity-core";
import {
  AgentSessionActivityEventRecorder,
  createTuttidAgentSessionActivityEventAppender,
  type AgentSessionActivityEvent
} from "./agentSessionActivityEventRecorder.ts";

test("records replayable intents and their command effects in one sequence", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  let now = 100;
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
        assert.equal(input.recordingId, "recording-1");
      }
    },
    nowUnixMs: () => now++
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  const submit = {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "keep going", type: "text" }],
    expiresAtUnixMs: 5_000,
    requestedAtUnixMs: 90,
    routing: "auto",
    type: "submit/requested",
    workspaceId: "workspace-1"
  } satisfies EngineIntent;
  recorder.observeIntent(submit);

  const command = {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    commandId: "submit:send:submit-1",
    content: submit.content,
    correlationId: "submit-1",
    guidance: true,
    promptId: "submit-1",
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  } satisfies EngineExternalCommand;
  recorder.observeCommand(command);
  recorder.observeIntent({
    commandId: command.commandId,
    commandType: command.type,
    correlationId: "submit-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { turnId: "turn-1" }
  });

  await recorder.seal();

  assert.deepEqual(appended, [
    {
      agentSessionId: "session-1",
      correlationId: "submit-1",
      eventId: "recording-1:activity:1",
      kind: "intent",
      occurredAtUnixMs: 100,
      payload: {
        clientSubmitId: "submit-1",
        content: [{ text: "keep going", type: "text" }],
        expiresAtUnixMs: 5_000,
        requestedAtUnixMs: 90,
        routing: "auto"
      },
      schemaVersion: 3,
      scopeId: "workspace-1",
      sequence: 1,
      type: "submit/requested"
    },
    {
      agentSessionId: "session-1",
      causedByEventId: "recording-1:activity:1",
      correlationId: "submit-1",
      eventId: "recording-1:activity:2",
      kind: "effect",
      occurredAtUnixMs: 101,
      payload: {
        clientSubmitId: "submit-1",
        content: [{ text: "keep going", type: "text" }],
        guidance: true,
        outcome: "succeeded",
        promptId: "submit-1",
        result: { turnId: "turn-1" }
      },
      schemaVersion: 3,
      scopeId: "workspace-1",
      sequence: 2,
      type: "queue/sendPrompt"
    }
  ]);
});

test("effect correlation follows the causing intent when command ids differ", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    },
    nowUnixMs: () => 100
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });
  recorder.observeIntent({
    agentSessionId: "session-1",
    agentTargetId: "local:codex",
    clientSubmitId: "submit-create",
    expiresAtUnixMs: 5_000,
    mode: "new",
    requestedAtUnixMs: 100,
    requestId: "activate-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  });
  recorder.observeCommand({
    agentSessionId: "session-1",
    agentTargetId: "local:codex",
    clientSubmitId: "submit-create",
    commandId: "activate:activate-1",
    correlationId: "activate-1",
    mode: "new",
    type: "session/activate",
    workspaceId: "workspace-1"
  });
  recorder.observeIntent({
    commandId: "activate:activate-1",
    commandType: "session/activate",
    correlationId: "activate-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { id: "session-1" }
  });

  await recorder.seal();

  assert.deepEqual(
    appended.map((event) => [event.kind, event.type, event.correlationId]),
    [
      ["intent", "activation/requested", "submit-create"],
      ["effect", "session/activate", "submit-create"]
    ]
  );
});

test("records plan feedback and its send effect as one replayable chain", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    },
    nowUnixMs: () => 100
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });
  recorder.observeIntent({
    agentSessionId: "session-1",
    clientSubmitId: "plan-feedback-1",
    content: [{ text: "Add verification", type: "text" }],
    expiresAtUnixMs: 5_000,
    requestedAtUnixMs: 90,
    requestId: "turn-plan-1",
    turnId: "turn-plan-1",
    type: "plan/feedbackRequested",
    workspaceId: "workspace-1"
  });
  recorder.observeCommand({
    agentSessionId: "session-1",
    clientSubmitId: "plan-feedback-1",
    commandId: "submit:send:plan-feedback-1",
    content: [{ text: "Add verification", type: "text" }],
    correlationId: "plan-feedback-1",
    promptId: "plan-feedback-1",
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  });
  recorder.observeIntent({
    commandId: "submit:send:plan-feedback-1",
    commandType: "queue/sendPrompt",
    correlationId: "plan-feedback-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { turnId: "turn-plan-2" }
  });

  await recorder.seal();

  assert.deepEqual(
    appended.map((event) => [event.kind, event.type, event.correlationId]),
    [
      ["intent", "plan/feedbackRequested", "plan-feedback-1"],
      ["effect", "queue/sendPrompt", "plan-feedback-1"]
    ]
  );
  assert.equal(appended[1]?.causedByEventId, appended[0]?.eventId);
});

test("records lifecycle intents through their existing Engine effects", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  const cases: readonly [EngineIntent, EngineExternalCommand][] = [
    [
      {
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-create",
        expiresAtUnixMs: 5_000,
        mode: "new",
        requestedAtUnixMs: 100,
        requestId: "activate-1",
        type: "activation/requested",
        workspaceId: "workspace-1"
      },
      {
        agentSessionId: "session-1",
        agentTargetId: "local:codex",
        clientSubmitId: "submit-create",
        commandId: "activate:activate-1",
        correlationId: "activate-1",
        mode: "new",
        type: "session/activate",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        agentSessionId: "session-1",
        awaitingTurnExpiresAtUnixMs: 5_000,
        commandId: "stop-1",
        type: "session/stopRequested",
        workspaceId: "workspace-1"
      },
      {
        agentSessionId: "session-1",
        commandId: "stop-1",
        turnId: "turn-1",
        type: "turn/cancel",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        action: "set",
        agentSessionId: "session-1",
        clientSubmitId: "goal-submit-1",
        commandId: "goal-command-1",
        objective: "ship it",
        requestedAtUnixMs: 1,
        timeoutMs: 30_000,
        type: "goal/controlRequested",
        workspaceId: "workspace-1"
      },
      {
        action: "set",
        agentSessionId: "session-1",
        clientSubmitId: "goal-submit-1",
        commandId: "goal-command-1",
        correlationId: "goal-submit-1",
        objective: "ship it",
        type: "goal/control",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        action: "approve",
        agentSessionId: "session-1",
        commandId: "respond-1",
        requestId: "request-1",
        turnId: "turn-1",
        type: "interaction/responseRequested",
        workspaceId: "workspace-1"
      },
      {
        action: "approve",
        agentSessionId: "session-1",
        commandId: "respond-1",
        correlationId: "9:session-16:turn-1request-1",
        requestId: "request-1",
        turnId: "turn-1",
        type: "interaction/respond",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        action: "implement",
        agentSessionId: "session-1",
        commandId: "plan-1",
        idempotencyKey: "decision-1",
        promptKind: "plan-implementation",
        requestId: "turn-1",
        turnId: "turn-1",
        type: "plan/decisionRequested",
        workspaceId: "workspace-1"
      },
      {
        action: "implement",
        agentSessionId: "session-1",
        commandId: "plan-1",
        correlationId: "9:session-16:turn-1turn-1",
        idempotencyKey: "decision-1",
        promptKind: "plan-implementation",
        requestId: "turn-1",
        turnId: "turn-1",
        type: "plan/submitDecision",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        agentSessionId: "session-1",
        awaitingTurnExpiresAtUnixMs: 5_000,
        commandId: "cancel-1",
        type: "session/cancelRequested",
        workspaceId: "workspace-1"
      },
      {
        agentSessionId: "session-1",
        commandId: "cancel-1",
        turnId: "turn-1",
        type: "turn/cancel",
        workspaceId: "workspace-1"
      }
    ],
    [
      {
        agentSessionId: "session-1",
        commandId: "settings-1",
        settings: { planMode: true },
        type: "session/settingsUpdateRequested",
        workspaceId: "workspace-1"
      },
      {
        agentSessionId: "session-1",
        commandId: "settings-1",
        correlationId: "session-1",
        settings: { planMode: true },
        type: "session/updateSettings",
        workspaceId: "workspace-1"
      }
    ]
  ];

  for (const [intent, command] of cases) {
    recorder.observeIntent(intent);
    recorder.observeCommand(command);
    recorder.observeIntent({
      commandId: command.commandId,
      commandType: command.type,
      ...("correlationId" in command
        ? { correlationId: command.correlationId }
        : {}),
      outcome: "succeeded",
      type: "engine/commandResult"
    });
  }
  await recorder.seal();

  assert.deepEqual(
    appended.map((event) => [event.kind, event.type]),
    cases.flatMap(([intent, command]) => [
      ["intent", intent.type],
      ["effect", command.type]
    ])
  );
  for (let index = 0; index < appended.length; index += 2) {
    assert.equal(
      appended[index + 1]?.causedByEventId,
      appended[index]?.eventId
    );
  }
});

test("flushes replayable events before the recording is sealed", async () => {
  const appended = deferred<void>();
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append() {
        appended.resolve();
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeIntent(queueRemoved("prompt-1"));

  await appended.promise;
  recorder.discard();
});

test("filters runtime intents and effects that cannot drive replay", async () => {
  const batches: readonly AgentSessionActivityEvent[][] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        (batches as AgentSessionActivityEvent[][]).push(input.events.slice());
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeIntent({
    status: "connected",
    type: "engine/connectionChanged"
  });
  recorder.observeCommand({
    commandId: "probe-1",
    type: "engine/probe"
  });
  recorder.observeIntent({
    commandId: "probe-1",
    commandType: "engine/probe",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  await recorder.seal();
  assert.deepEqual(batches, []);
});

test("skips declared engine-internal settings commands without failing seal", async () => {
  const batches: readonly AgentSessionActivityEvent[][] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        (batches as AgentSessionActivityEvent[][]).push(input.events.slice());
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  // Reducer-internal settings continuations have no host-dispatched cause by
  // design and are declared in the interaction contract instead of recorded.
  for (const commandId of [
    "activation-settings:activation-1",
    "prompt:settings:queue:send:1"
  ]) {
    recorder.observeCommand({
      agentSessionId: "session-1",
      commandId,
      correlationId: "session-1",
      settings: { planMode: true },
      type: "session/updateSettings",
      workspaceId: "workspace-1"
    });
    recorder.observeIntent({
      commandId,
      commandType: "session/updateSettings",
      correlationId: "session-1",
      outcome: "succeeded",
      type: "engine/commandResult"
    });
  }

  assert.deepEqual(recorder.getRecordingDefects(), []);
  await recorder.seal();
  assert.deepEqual(batches, []);
});

test("seal fails closed for a replayable command without a recorded cause", async () => {
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append() {}
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeCommand({
    agentSessionId: "session-1",
    clientSubmitId: "submit-without-intent",
    commandId: "send-without-intent",
    content: [{ text: "orphan", type: "text" }],
    correlationId: "submit-without-intent",
    promptId: "submit-without-intent",
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  });
  recorder.observeIntent({
    commandId: "send-without-intent",
    commandType: "queue/sendPrompt",
    correlationId: "submit-without-intent",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  assert.deepEqual(recorder.getRecordingDefects(), [
    {
      commandId: "send-without-intent",
      commandType: "queue/sendPrompt",
      correlationId: "submit-without-intent",
      reason: "uncorrelated-command"
    }
  ]);
  await assert.rejects(
    recorder.seal(),
    /uncorrelated-command commandType=queue\/sendPrompt commandId=send-without-intent correlationId=submit-without-intent/
  );
  recorder.discard();
});

test("seal fails closed for an effect-bearing command still awaiting its result", async () => {
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append() {}
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeIntent({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "keep going", type: "text" }],
    expiresAtUnixMs: 5_000,
    requestedAtUnixMs: 90,
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  recorder.observeCommand({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    commandId: "submit:send:submit-1",
    content: [{ text: "keep going", type: "text" }],
    correlationId: "submit-1",
    promptId: "submit-1",
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  });

  await assert.rejects(
    recorder.seal(),
    /unsettled-command commandType=queue\/sendPrompt commandId=submit:send:submit-1 correlationId=submit-1/
  );
  recorder.discard();
});

test("records the send-now cancel effect through its declared cancelCommandId", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeIntent({
    agentSessionId: "session-1",
    awaitingTurnExpiresAtUnixMs: 6_000,
    cancelCommandId: "send-now-cancel-1",
    promptId: "prompt-1",
    timeoutMs: 30_000,
    type: "queue/sendNowRequested"
  });
  recorder.observeCommand({
    agentSessionId: "session-1",
    commandId: "send-now-cancel-1",
    turnId: "turn-1",
    type: "turn/cancel",
    workspaceId: "workspace-1"
  });
  recorder.observeIntent({
    commandId: "send-now-cancel-1",
    commandType: "turn/cancel",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  await recorder.seal();

  assert.deepEqual(
    appended.map((event) => [event.kind, event.type, event.correlationId]),
    [
      ["intent", "queue/sendNowRequested", "prompt-1"],
      ["effect", "turn/cancel", "prompt-1"]
    ]
  );
  assert.equal(appended[1]?.causedByEventId, appended[0]?.eventId);
});

test("records the send_now submit cancel effect through its derived commandId", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });

  recorder.observeIntent({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "now", type: "text" }],
    expiresAtUnixMs: 5_000,
    requestedAtUnixMs: 90,
    routing: "send_now",
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  recorder.observeCommand({
    agentSessionId: "session-1",
    commandId: "submit:cancel:submit-1",
    turnId: "turn-1",
    type: "turn/cancel",
    workspaceId: "workspace-1"
  });
  recorder.observeIntent({
    commandId: "submit:cancel:submit-1",
    commandType: "turn/cancel",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  await recorder.seal();

  assert.deepEqual(
    appended.map((event) => [event.kind, event.type, event.correlationId]),
    [
      ["intent", "submit/requested", "submit-1"],
      ["effect", "turn/cancel", "submit-1"]
    ]
  );
  assert.equal(appended[1]?.causedByEventId, appended[0]?.eventId);
});

test("flush keeps events appended while an earlier batch is in flight", async () => {
  const firstWrite = deferred<void>();
  const batches: AgentSessionActivityEvent[][] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        batches.push(input.events.slice());
        if (batches.length === 1) {
          await firstWrite.promise;
        }
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });
  recorder.observeIntent(queueRemoved("prompt-1"));

  const flush = recorder.flush();
  recorder.observeIntent(queueRemoved("prompt-2"));
  firstWrite.resolve();
  await flush;

  assert.deepEqual(
    batches.map((batch) => batch.map((event) => event.sequence)),
    [[1], [2]]
  );
});

test("failed seal retains the batch for retry and stops new observations", async () => {
  let attempts = 0;
  const appended: AgentSessionActivityEvent[] = [];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        attempts += 1;
        if (attempts === 1) throw new Error("transport unavailable");
        appended.push(...input.events);
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });
  recorder.observeIntent(queueRemoved("prompt-1"));

  await assert.rejects(recorder.seal(), /transport unavailable/);
  recorder.observeIntent(queueRemoved("prompt-2"));
  await recorder.seal();

  assert.equal(attempts, 2);
  assert.deepEqual(
    appended.map((event) => event.payload.promptId),
    ["prompt-1"]
  );
});

test("buffers a clone instead of retaining mutable intent data", async () => {
  const appended: AgentSessionActivityEvent[] = [];
  const content = [{ text: "before", type: "text" as const }];
  const recorder = new AgentSessionActivityEventRecorder({
    appender: {
      async append(input) {
        appended.push(...input.events);
      }
    }
  });
  recorder.start({ recordingId: "recording-1", scopeId: "workspace-1" });
  recorder.observeIntent({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content,
    expiresAtUnixMs: 5_000,
    requestedAtUnixMs: 90,
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  content[0]!.text = "after";

  await recorder.seal();
  assert.deepEqual(appended[0]?.payload.content, [
    { text: "before", type: "text" }
  ]);
});

test("tuttid appender strips local sequence fields from the HTTP request", async () => {
  let received: unknown;
  const appender = createTuttidAgentSessionActivityEventAppender({
    tuttidClient: {
      async appendAgentSessionRecordingActivityEvents(...args) {
        received = args;
        return { acceptedThroughSequence: 1 };
      }
    },
    workspaceId: " workspace-1 "
  });

  await appender.append({
    events: [
      {
        agentSessionId: "session-1",
        eventId: "recording-1:activity:1",
        kind: "intent",
        occurredAtUnixMs: 100,
        payload: { promptId: "prompt-1" },
        schemaVersion: 3,
        scopeId: "workspace-1",
        sequence: 1,
        type: "queue/removed"
      }
    ],
    recordingId: "recording-1"
  });

  assert.deepEqual(received, [
    "workspace-1",
    "recording-1",
    {
      events: [
        {
          agentSessionId: "session-1",
          eventId: "recording-1:activity:1",
          kind: "intent",
          occurredAtUnixMs: 100,
          payload: { promptId: "prompt-1" },
          type: "queue/removed"
        }
      ]
    }
  ]);
});

function queueRemoved(promptId: string): EngineIntent {
  return {
    agentSessionId: "session-1",
    promptId,
    type: "queue/removed"
  };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve(value: T | PromiseLike<T>): void;
} {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
}
