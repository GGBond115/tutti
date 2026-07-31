import type { AgentSessionEngine } from "@tutti-os/agent-activity-core";
import {
  dismissPendingSubmission,
  resolvePendingSubmission,
  type PendingSubmission
} from "./pendingSubmission";

describe("resolvePendingSubmission", () => {
  it("reuses the exact identity when retrying an existing session submission", () => {
    const first = resolvePendingSubmission(null, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "continue"
    });

    expect(
      resolvePendingSubmission(first, {
        agentSessionId: "session-1",
        agentTargetId: "ignored-for-existing-session",
        creating: false,
        kind: "prompt",
        text: "continue"
      })
    ).toBe(first);
  });

  it("reuses both session and submit identity when retrying session creation", () => {
    const first = resolvePendingSubmission(null, {
      agentSessionId: null,
      agentTargetId: "target-1",
      creating: true,
      kind: "prompt",
      text: "start"
    });
    const retry = resolvePendingSubmission(first, {
      agentSessionId: null,
      agentTargetId: "target-1",
      creating: true,
      kind: "prompt",
      text: "start"
    });

    expect(retry).toBe(first);
    expect(retry.agentSessionId).not.toBe("");
    expect(retry.clientSubmitId).not.toBe("");
  });

  it("creates a new identity after the submission content changes", () => {
    const first = resolvePendingSubmission(null, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "first"
    });
    const changed = resolvePendingSubmission(first, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "second"
    });

    expect(changed).not.toBe(first);
    expect(changed.clientSubmitId).not.toBe(first.clientSubmitId);
  });

  it("does not reuse an identity across sessions", () => {
    const first = resolvePendingSubmission(null, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "continue"
    });
    const otherSession = resolvePendingSubmission(first, {
      agentSessionId: "session-2",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "continue"
    });

    expect(otherSession).not.toBe(first);
    expect(otherSession.agentSessionId).toBe("session-2");
  });

  it("does not reuse prompt identity for a Goal command", () => {
    const prompt = resolvePendingSubmission(null, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "prompt",
      text: "/goal ship it"
    });
    const goal = resolvePendingSubmission(prompt, {
      agentSessionId: "session-1",
      agentTargetId: null,
      creating: false,
      kind: "goalControl",
      text: "/goal ship it"
    });

    expect(goal).not.toBe(prompt);
    expect(goal.clientSubmitId).not.toBe(prompt.clientSubmitId);
  });
});

describe("dismissPendingSubmission", () => {
  it.each([
    [
      false,
      {
        clientSubmitId: "submit-1",
        type: "submit/dismissed"
      }
    ],
    [
      true,
      {
        requestId: "submit-1",
        type: "activation/dismissed"
      }
    ]
  ])("dispatches the exact creating=%s dismissal", (creating, expected) => {
    const intents: unknown[] = [];
    const engine = {
      dispatch(intent: unknown) {
        intents.push(intent);
      }
    } as AgentSessionEngine;
    const submission: PendingSubmission = {
      agentSessionId: "session-1",
      agentTargetId: creating ? "target-1" : null,
      clientSubmitId: "submit-1",
      creating,
      kind: "prompt",
      text: "hello"
    };

    dismissPendingSubmission(engine, submission);

    expect(intents).toEqual([expected]);
  });
});
