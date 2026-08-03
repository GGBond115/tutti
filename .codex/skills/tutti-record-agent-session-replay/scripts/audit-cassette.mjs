#!/usr/bin/env node
import { access, readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { verifyCassette } from "../../../../tools/scripts/agent-session-replay-runner/cassette.mjs";

const directory = resolve(process.argv[2] ?? "");
if (!process.argv[2]) {
  throw new Error("usage: audit-cassette.mjs <cassette-directory>");
}

const repoRoot = await resolveRepoRoot();
const activityContract = await readJSON(
  join(
    repoRoot,
    "packages",
    "agent",
    "session-replay",
    "activity-contract.json"
  )
);
if (activityContract.schemaVersion !== 1) {
  throw new Error(
    `Unsupported activity contract schemaVersion ${activityContract.schemaVersion}, want 1`
  );
}

const manifest = await verifyCassette(directory);
const [providerManifest, frameText, activityText, expectedState] =
  await Promise.all([
    readJSON(join(directory, "provider", "manifest.json")),
    readFile(join(directory, "provider", "frames.jsonl"), "utf8"),
    readFile(join(directory, "activity-events.jsonl"), "utf8"),
    readJSON(join(directory, "expected-state.json"))
  ]);

const frames = parseJSONLines(frameText);
const activities = parseJSONLines(activityText);
assertContinuous(
  frames.map((frame) => frame.globalSeq),
  "Provider global sequence"
);
assertContinuous(
  activities.map((event) => event.sequence),
  "Activity sequence"
);
const causalityViolations = auditActivityCausality(
  activities,
  activityContract
);
if (causalityViolations.length > 0) {
  throw new Error(
    `Activity causality audit failed with ${causalityViolations.length} violation(s):\n` +
      causalityViolations.map((violation) => `- ${violation}`).join("\n")
  );
}
for (const connection of providerManifest.connections ?? []) {
  assertContinuous(
    frames
      .filter((frame) => frame.connectionId === connection.connectionId)
      .map((frame) => frame.chunkSeq),
    `Provider ${connection.connectionId} sequence`
  );
}

const sessions = expectedState.agent?.sessions ?? [];
const result = {
  cassette: {
    id: manifest.id,
    name: manifest.name,
    providerTarget: manifest.agentTargetId,
    mode: manifest.mode,
    schemaVersion: manifest.schemaVersion,
    totalBytes: manifest.totalBytes
  },
  provider: {
    status: providerManifest.status,
    frameCount: providerManifest.frameCount,
    actualFrameCount: frames.length,
    framesSha256: providerManifest.framesSha256,
    connections: (providerManifest.connections ?? []).map((connection) => ({
      connectionId: connection.connectionId,
      provider: connection.provider,
      launchOrdinal: connection.launchOrdinal,
      captureOrigin: connection.captureOrigin
    }))
  },
  activities: activities.map((event) => ({
    sequence: event.sequence,
    kind: event.kind,
    type: event.type,
    optionId: event.payload?.optionId ?? null,
    action: event.payload?.action ?? null,
    requestId: event.payload?.requestId ?? null
  })),
  causality: {
    contractSchemaVersion: activityContract.schemaVersion,
    intentCount: activities.filter((event) => event.kind === "intent").length,
    effectCount: activities.filter((event) => event.kind === "effect").length,
    directStimulusCount: activities.filter(
      (event) => event.kind === "direct-stimulus"
    ).length
  },
  state: {
    sessions: sessions.map((session) => ({
      id: session.id,
      turns: (session.turns ?? []).map((turn) => ({
        id: turn.id,
        phase: turn.phase,
        outcome: turn.outcome ?? null
      })),
      interactions: (session.interactions ?? []).map((interaction) => ({
        requestId: interaction.requestId,
        kind: interaction.kind,
        status: interaction.status,
        optionId: interaction.output?.optionId ?? null,
        action: interaction.output?.action ?? null
      })),
      tools: (session.messages ?? [])
        .filter((message) => message.kind === "tool_call")
        .map((message) => ({
          status: message.status,
          exitCode: message.payload?.output?.exitCode ?? null
        })),
      finalAssistantText:
        [...(session.messages ?? [])]
          .reverse()
          .find((message) => message.kind === "text")?.payload?.text ?? null
    }))
  }
};

if (
  providerManifest.status !== "complete" ||
  providerManifest.frameCount !== frames.length
) {
  throw new Error("Provider cassette is incomplete");
}
process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);

async function readJSON(path) {
  return JSON.parse(await readFile(path, "utf8"));
}

function parseJSONLines(text) {
  return text
    .split(/\r?\n/u)
    .filter((line) => line.trim())
    .map((line) => JSON.parse(line));
}

async function resolveRepoRoot() {
  const scriptDirectory = dirname(fileURLToPath(import.meta.url));
  const root = resolve(scriptDirectory, "../../../..");
  for (const marker of ["pnpm-workspace.yaml", "package.json"]) {
    try {
      await access(join(root, marker));
      return root;
    } catch {
      // try the next marker
    }
  }
  throw new Error(
    `Repository root not found at ${root} (missing pnpm-workspace.yaml/package.json)`
  );
}

function auditActivityCausality(activities, contract) {
  const violations = [];
  const intents = contract.intents ?? {};
  const eventsById = new Map(activities.map((event) => [event.eventId, event]));
  const referencedIntentIds = new Set();

  for (const event of activities) {
    if (event.kind === "direct-stimulus") {
      if (event.causedByEventId != null) {
        violations.push(
          `${describeEvent(event)}: direct-stimulus must not have causedByEventId ${event.causedByEventId}`
        );
      }
      continue;
    }
    if (event.kind === "intent") {
      if (!intents[event.type]) {
        violations.push(
          `${describeEvent(event)}: intent type is not declared in activity-contract.json`
        );
      }
      continue;
    }
    if (event.kind !== "effect") {
      continue;
    }
    if (event.causedByEventId == null) {
      violations.push(
        `${describeEvent(event)}: effect is missing causedByEventId`
      );
      continue;
    }
    const cause = eventsById.get(event.causedByEventId);
    if (!cause) {
      violations.push(
        `${describeEvent(event)}: causedByEventId ${event.causedByEventId} does not exist`
      );
      continue;
    }
    if (cause.kind !== "intent") {
      violations.push(
        `${describeEvent(event)}: causedByEventId ${event.causedByEventId} is kind ${cause.kind}, want intent`
      );
      continue;
    }
    referencedIntentIds.add(cause.eventId);
    if (cause.sequence >= event.sequence) {
      violations.push(
        `${describeEvent(event)}: cause sequence ${cause.sequence} is not earlier than effect`
      );
    }
    if (
      event.correlationId != null &&
      cause.correlationId != null &&
      event.correlationId !== cause.correlationId
    ) {
      violations.push(
        `${describeEvent(event)}: correlationId conflicts with cause correlationId ${cause.correlationId}`
      );
    }
    const declaredEffects = intents[cause.type]?.effects;
    if (declaredEffects && !declaredEffects.includes(event.type)) {
      violations.push(
        `${describeEvent(event)}: effect type is not declared for intent ${cause.type} (allowed: ${declaredEffects.join(", ") || "none"})`
      );
    }
  }

  for (const event of activities) {
    if (event.kind !== "intent") {
      continue;
    }
    if (
      intents[event.type]?.requiresEffect &&
      !referencedIntentIds.has(event.eventId)
    ) {
      violations.push(
        `${describeEvent(event)}: intent requires at least one effect but none references it`
      );
    }
  }

  return violations;
}

function describeEvent(event) {
  return (
    `sequence=${event.sequence ?? "?"} eventId=${event.eventId ?? "?"} ` +
    `kind=${event.kind ?? "?"} type=${event.type ?? "?"} ` +
    `correlationId=${event.correlationId ?? "-"}`
  );
}

function assertContinuous(values, label) {
  values.forEach((value, index) => {
    const expected = index + 1;
    if (value !== expected) {
      throw new Error(
        `${label} is ${value} at index ${index}, want ${expected}`
      );
    }
  });
}
