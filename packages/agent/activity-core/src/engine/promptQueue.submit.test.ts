import assert from "node:assert/strict";
import test from "node:test";
import type { SubmitRequestedIntent } from "./pendingIntents.types.ts";
import { normalizeQueuedPrompt } from "./promptQueue.prompt.ts";
import { queuedPromptFromSubmitIntent } from "./promptQueue.submit.ts";

function submit(
  clientSubmitId: string,
  promptId?: string
): SubmitRequestedIntent {
  return {
    agentSessionId: "session-1",
    clientSubmitId,
    ...(promptId ? { promptId } : {}),
    content: [{ type: "text", text: clientSubmitId }],
    expiresAtUnixMs: 10_000,
    requestedAtUnixMs: 1,
    type: "submit/requested",
    workspaceId: "workspace-1"
  };
}

test("new queued prompts have independent queue and delivery identities", () => {
  const first = queuedPromptFromSubmitIntent(submit("submit-1"), true);
  const second = queuedPromptFromSubmitIntent(submit("submit-2"), true);

  assert.equal(first.clientSubmitId, "submit-1");
  assert.equal(second.clientSubmitId, "submit-2");
  assert.notEqual(first.id, first.clientSubmitId);
  assert.notEqual(second.id, second.clientSubmitId);
  assert.notEqual(first.id, second.id);
});

test("an explicitly supplied queue identity survives replay", () => {
  const prompt = queuedPromptFromSubmitIntent(
    submit("submit-1", "prompt-1"),
    true
  );

  assert.equal(prompt.id, "prompt-1");
  assert.equal(prompt.clientSubmitId, "submit-1");
});

test("legacy clientSubmitId migration is confined to queue prompt decoding", () => {
  const prompt = normalizeQueuedPrompt({
    content: [{ type: "text", text: "legacy" }],
    createdAtUnixMs: 1,
    id: "legacy-prompt"
  });

  assert.equal(prompt?.id, "legacy-prompt");
  assert.equal(prompt?.clientSubmitId, "legacy-prompt");
});
