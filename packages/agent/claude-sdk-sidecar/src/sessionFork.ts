import { createHash } from "node:crypto";
import {
  forkSession,
  getSessionInfo,
  getSessionMessages
} from "@anthropic-ai/claude-agent-sdk";
import { readUserMessageNotificationText } from "./taskNotification.ts";

type SDKMessage = {
  type?: unknown;
  uuid?: unknown;
  session_id?: unknown;
  message?: unknown;
  parent_tool_use_id?: unknown;
  isSynthetic?: unknown;
  origin?: unknown;
};

type ForkInspectInput = {
  sessionId: string;
  cwd: string;
};

type ForkInput = ForkInspectInput & {
  providerTurnId: string;
  providerCheckpointMessageId: string;
  title: string;
};

type ClaudeForkSDK = {
  forkSession: typeof forkSession;
  getSessionMessages: typeof getSessionMessages;
  getSessionInfo: typeof getSessionInfo;
};

const defaultClaudeForkSDK: ClaudeForkSDK = {
  forkSession,
  getSessionMessages,
  getSessionInfo
};

type ClaudeForkStage = "source_lookup" | "provider_fork" | "child_verification";

export async function inspectClaudeForkCheckpoints(
  input: ForkInspectInput,
  sdk: ClaudeForkSDK = defaultClaudeForkSDK
): Promise<Record<string, unknown>> {
  requireIdentity(input.sessionId, "provider session id");
  const messages = (await sdk.getSessionMessages(
    input.sessionId,
    transcriptOptions(input.cwd)
  )) as SDKMessage[];
  return {
    providerTurnIds: rootProviderTurnIds(messages)
  };
}

export async function forkClaudeSession(
  input: ForkInput,
  sdk: ClaudeForkSDK = defaultClaudeForkSDK
): Promise<Record<string, unknown>> {
  let forkStarted = false;
  let stage: ClaudeForkStage = "source_lookup";
  try {
    return await forkClaudeSessionResolved(input, sdk, (nextStage) => {
      stage = nextStage;
      if (nextStage === "provider_fork") {
        forkStarted = true;
      }
    });
  } catch (error) {
    throw new ClaudeForkError(
      forkStarted ? "unknown" : "not_started",
      stage,
      error
    );
  }
}

async function forkClaudeSessionResolved(
  input: ForkInput,
  sdk: ClaudeForkSDK,
  onStage: (stage: ClaudeForkStage) => void
): Promise<Record<string, unknown>> {
  requireIdentity(input.sessionId, "provider session id");
  requireIdentity(input.providerTurnId, "provider turn id");
  const options = sdkOptions(input.cwd);
  const transcriptReadOptions = transcriptOptions(input.cwd);
  let checkpointId = input.providerCheckpointMessageId.trim();
  if (!checkpointId) {
    const sourceMessages = (await sdk.getSessionMessages(
      input.sessionId,
      transcriptReadOptions
    )) as SDKMessage[];
    checkpointId = checkpointForProviderTurn(
      sourceMessages,
      input.providerTurnId
    );
  }
  requireIdentity(checkpointId, "checkpoint message id");

  onStage("provider_fork");
  const forkResult = await sdk.forkSession(input.sessionId, {
    ...options,
    upToMessageId: checkpointId,
    ...(input.title.trim() ? { title: input.title.trim() } : {})
  });
  const childSessionId = messageIdentity(forkResult?.sessionId);
  requireUUID(childSessionId, "forked provider session id");
  if (childSessionId === input.sessionId) {
    throw new Error("forked provider session id equals source session id");
  }

  onStage("child_verification");
  const [childInfo, childMessages] = await Promise.all([
    sdk.getSessionInfo(childSessionId, options),
    sdk.getSessionMessages(childSessionId, transcriptReadOptions) as Promise<
      SDKMessage[]
    >
  ]);
  const childInfoSessionId = messageIdentity(childInfo?.sessionId);
  if (childInfoSessionId && childInfoSessionId !== childSessionId) {
    throw new Error("forked Claude session resolved to another session");
  }
  if (!childInfoSessionId && childMessages.length === 0) {
    throw new Error("forked Claude session is not independently discoverable");
  }
  const childBinding = latestProviderTurnBinding(childMessages);
  const targetProviderTurnId = childBinding.providerTurnId;
  const targetCheckpointId = childBinding.checkpointMessageId;
  const receipt = createHash("sha256")
    .update(
      JSON.stringify({
        sourceSessionId: input.sessionId,
        childSessionId,
        checkpointId,
        targetCheckpointId,
        sourceProviderTurnId: input.providerTurnId,
        targetProviderTurnId
      })
    )
    .digest("hex");
  return {
    providerSessionId: childSessionId,
    targetProviderTurnIds: [targetProviderTurnId],
    targetProviderCheckpointMessageId: targetCheckpointId,
    stateBindingMode: "provider_owned",
    stateBindingReceipt: `claude-sdk-fork-v2:${receipt}`,
    deliveryDisposition: "accepted"
  };
}

class ClaudeForkError extends Error {
  readonly deliveryDisposition: "not_started" | "unknown";
  readonly stage: ClaudeForkStage;

  constructor(
    deliveryDisposition: "not_started" | "unknown",
    stage: ClaudeForkStage,
    cause: unknown
  ) {
    super(
      `Claude SDK session fork failed at ${stage}: ${forkErrorMessage(cause)}`
    );
    this.deliveryDisposition = deliveryDisposition;
    this.stage = stage;
  }
}

function forkErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  if (typeof error === "string" && error.trim()) {
    return error.trim();
  }
  return "unknown error";
}

function checkpointForProviderTurn(
  messages: SDKMessage[],
  providerTurnId: string
): string {
  const selectedIndex = messages.findIndex(
    (message) =>
      isRootUserMessage(message) && messageIdentity(message) === providerTurnId
  );
  if (selectedIndex < 0) {
    throw new Error("selected provider turn is absent from Claude transcript");
  }
  const nextRootIndex = messages.findIndex(
    (message, index) => index > selectedIndex && isRootUserMessage(message)
  );
  const end = nextRootIndex < 0 ? messages.length : nextRootIndex;
  if (end <= selectedIndex) {
    throw new Error(
      "selected provider turn has no exact transcript checkpoint"
    );
  }
  const checkpointId = messageIdentity(messages[end - 1]);
  requireIdentity(checkpointId, "checkpoint message id");
  return checkpointId;
}

function latestProviderTurnBinding(messages: SDKMessage[]): {
  providerTurnId: string;
  checkpointMessageId: string;
} {
  let selectedIndex = -1;
  let providerTurnId = "";
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]!;
    if (!isRootUserMessage(message)) {
      continue;
    }
    const identity = messageIdentity(message);
    if (!identity) {
      continue;
    }
    selectedIndex = index;
    providerTurnId = identity;
    break;
  }
  requireIdentity(providerTurnId, "forked provider turn id");

  let checkpointMessageId = "";
  for (let index = messages.length - 1; index >= selectedIndex; index -= 1) {
    const identity = messageIdentity(messages[index]);
    if (identity) {
      checkpointMessageId = identity;
      break;
    }
  }
  requireIdentity(checkpointMessageId, "forked checkpoint message id");
  return { providerTurnId, checkpointMessageId };
}

function rootProviderTurnIds(messages: SDKMessage[]): string[] {
  const result: string[] = [];
  const seen = new Set<string>();
  for (const message of messages) {
    if (!isRootUserMessage(message)) {
      continue;
    }
    const identity = messageIdentity(message);
    if (!identity || seen.has(identity)) {
      throw new Error(
        "Claude transcript contains an invalid root user identity"
      );
    }
    seen.add(identity);
    result.push(identity);
  }
  return result;
}

function isRootUserMessage(message: SDKMessage): boolean {
  if (
    message?.type !== "user" ||
    message?.parent_tool_use_id ||
    message?.isSynthetic === true
  ) {
    return false;
  }
  const origin =
    message.origin && typeof message.origin === "object"
      ? (message.origin as { kind?: unknown })
      : undefined;
  if (origin?.kind === "coordinator") {
    return false;
  }
  return !readUserMessageNotificationText(
    message as { message?: { content?: unknown } }
  ).includes("<task-notification>");
}

function sdkOptions(cwd: string): { dir?: string } {
  const dir = cwd.trim();
  return dir ? { dir } : {};
}

function transcriptOptions(cwd: string): {
  dir?: string;
  includeSystemMessages: true;
} {
  return {
    ...sdkOptions(cwd),
    includeSystemMessages: true
  };
}

function messageIdentity(value: unknown): string {
  if (typeof value === "string") {
    return value.trim();
  }
  if (value && typeof value === "object" && "uuid" in value) {
    return messageIdentity((value as { uuid?: unknown }).uuid);
  }
  return "";
}

function requireIdentity(value: string, label: string): void {
  if (!value.trim()) {
    throw new Error(`${label} is required`);
  }
}

function requireUUID(value: string, label: string): void {
  requireIdentity(value, label);
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
      value.trim()
    )
  ) {
    throw new Error(`${label} must be a UUID`);
  }
}
