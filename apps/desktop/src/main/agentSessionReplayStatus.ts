import { readFile, rename, rm, writeFile } from "node:fs/promises";
import type {
  DesktopGetAgentSessionReplayStatusInput,
  DesktopAgentSessionReplayPhase,
  DesktopAgentSessionReplayStatus,
  DesktopAgentSessionReplayTimingMode,
  DesktopSendAgentSessionReplayControlInput
} from "../shared/contracts/ipc.ts";

const replayPhases = new Set<DesktopAgentSessionReplayPhase>([
  "replaying",
  "verifying",
  "complete",
  "failed"
]);
const replayTimingModes = new Set<DesktopAgentSessionReplayTimingMode>([
  "realtime",
  "fast-forward"
]);

export async function readAgentSessionReplayStatus(
  input: DesktopGetAgentSessionReplayStatusInput,
  surfaceStatusPath = process.env.TUTTI_AGENT_SESSION_REPLAY_SURFACE_STATUS_PATH
): Promise<DesktopAgentSessionReplayStatus> {
  const cassetteId = input.cassetteId.trim();
  if (!cassetteId) {
    return { active: false };
  }
  const surfaceStatus = await readSurfaceStatus(surfaceStatusPath, cassetteId);
  if (surfaceStatus) {
    return surfaceStatus;
  }
  return { active: false };
}

export interface AgentSessionReplayCassetteStatusWriter {
  write(
    cassetteId: string,
    status: DesktopAgentSessionReplayStatus
  ): Promise<void>;
}

export function createAgentSessionReplayCassetteStatusWriter(
  surfaceStatusPath: string
): AgentSessionReplayCassetteStatusWriter {
  const statuses = new Map<string, DesktopAgentSessionReplayStatus>();
  let pending = Promise.resolve();
  let temporaryFileSequence = 0;
  return {
    write(cassetteId, status) {
      const normalizedCassetteId = cassetteId.trim();
      if (!normalizedCassetteId) {
        return Promise.reject(new Error("Replay Cassette id is required"));
      }
      const write = pending.then(async () => {
        statuses.set(normalizedCassetteId, structuredClone(status));
        temporaryFileSequence += 1;
        const temporaryPath = `${surfaceStatusPath}.${process.pid}.${temporaryFileSequence}.tmp`;
        try {
          await writeFile(
            temporaryPath,
            JSON.stringify({
              cassettes: Object.fromEntries(statuses),
              schemaVersion: 1
            }),
            { mode: 0o600 }
          );
          await rename(temporaryPath, surfaceStatusPath);
        } catch (error) {
          await rm(temporaryPath, { force: true }).catch(() => undefined);
          throw error;
        }
      });
      pending = write.catch(() => undefined);
      return write;
    }
  };
}

async function readSurfaceStatus(
  path: string | undefined,
  cassetteId: string
): Promise<DesktopAgentSessionReplayStatus | null> {
  if (!path?.trim()) return null;
  try {
    const parsed = JSON.parse(await readFile(path, "utf8")) as {
      cassettes?: Record<string, unknown>;
      schemaVersion?: unknown;
    };
    if (parsed.schemaVersion !== 1 || !parsed.cassettes) return null;
    return resolveReplayStatus(parsed.cassettes[cassetteId]);
  } catch {
    return null;
  }
}

function resolveReplayStatus(
  value: unknown
): DesktopAgentSessionReplayStatus | null {
  const parsed = value as {
    active?: unknown;
    cassetteId?: unknown;
    cassettes?: unknown;
    currentCheckpoint?: unknown;
    errorCause?: unknown;
    errorMessage?: unknown;
    paused?: unknown;
    phase?: unknown;
    targetCheckpoint?: unknown;
    timingMode?: unknown;
    totalDurationMs?: unknown;
    totalCheckpoints?: unknown;
  };
  if (
    !parsed ||
    typeof parsed !== "object" ||
    parsed.active !== true ||
    typeof parsed.phase !== "string" ||
    !replayPhases.has(parsed.phase as DesktopAgentSessionReplayPhase)
  ) {
    return null;
  }
  return {
    active: true,
    ...(typeof parsed.cassetteId === "string"
      ? { cassetteId: parsed.cassetteId }
      : {}),
    ...(Array.isArray(parsed.cassettes)
      ? {
          cassettes: parsed.cassettes.flatMap((cassette) => {
            const item = cassette as { id?: unknown; name?: unknown };
            return typeof item.id === "string" && typeof item.name === "string"
              ? [{ id: item.id, name: item.name }]
              : [];
          })
        }
      : {}),
    ...(isNonNegativeInteger(parsed.currentCheckpoint)
      ? { currentCheckpoint: parsed.currentCheckpoint }
      : {}),
    ...(isReplayFailureCause(parsed.errorCause)
      ? { errorCause: parsed.errorCause }
      : {}),
    ...(typeof parsed.errorMessage === "string" && parsed.errorMessage.trim()
      ? { errorMessage: parsed.errorMessage }
      : {}),
    ...(typeof parsed.paused === "boolean" ? { paused: parsed.paused } : {}),
    phase: parsed.phase as DesktopAgentSessionReplayPhase,
    ...(parsed.targetCheckpoint === null
      ? { targetCheckpoint: null }
      : isNonNegativeInteger(parsed.targetCheckpoint)
        ? { targetCheckpoint: parsed.targetCheckpoint }
        : {}),
    ...(typeof parsed.timingMode === "string" &&
    replayTimingModes.has(
      parsed.timingMode as DesktopAgentSessionReplayTimingMode
    )
      ? {
          timingMode: parsed.timingMode as DesktopAgentSessionReplayTimingMode
        }
      : {}),
    ...(isNonNegativeInteger(parsed.totalDurationMs)
      ? { totalDurationMs: parsed.totalDurationMs }
      : {}),
    ...(isNonNegativeInteger(parsed.totalCheckpoints)
      ? { totalCheckpoints: parsed.totalCheckpoints }
      : {})
  };
}

function isReplayFailureCause(
  value: unknown
): value is { code: string; message: string } {
  const cause = value as { code?: unknown; message?: unknown };
  return (
    Boolean(cause) &&
    typeof cause === "object" &&
    typeof cause.code === "string" &&
    Boolean(cause.code.trim()) &&
    typeof cause.message === "string" &&
    Boolean(cause.message.trim())
  );
}

export function createAgentSessionReplayControlWriter(
  controlPath = process.env.TUTTI_AGENT_SESSION_REPLAY_CONTROL_PATH
): (input: DesktopSendAgentSessionReplayControlInput) => Promise<void> {
  let pending = Promise.resolve();
  let temporaryFileSequence = 0;
  return (input) => {
    const write = pending.then(async () => {
      if (!controlPath?.trim()) {
        throw new Error("Replay control is unavailable");
      }
      const cassetteId = input.cassetteId.trim();
      if (!cassetteId) {
        throw new Error("Replay Cassette id is required");
      }
      const document = await readReplayControlDocument(controlPath);
      const revision = (document.cassettes[cassetteId]?.revision ?? 0) + 1;
      temporaryFileSequence += 1;
      const temporaryPath = `${controlPath}.${process.pid}.${temporaryFileSequence}.tmp`;
      try {
        await writeFile(
          temporaryPath,
          JSON.stringify({
            schemaVersion: 2,
            cassettes: {
              ...document.cassettes,
              [cassetteId]: {
                command: input.command,
                revision
              }
            }
          })
        );
        await rename(temporaryPath, controlPath);
      } catch (error) {
        await rm(temporaryPath, { force: true }).catch(() => undefined);
        throw error;
      }
    });
    pending = write.catch(() => undefined);
    return write;
  };
}

interface ReplayControlDocument {
  cassettes: Record<
    string,
    {
      command: DesktopSendAgentSessionReplayControlInput["command"];
      revision: number;
    }
  >;
  schemaVersion: 2;
}

async function readReplayControlDocument(
  controlPath: string
): Promise<ReplayControlDocument> {
  try {
    const parsed = JSON.parse(await readFile(controlPath, "utf8")) as {
      cassettes?: unknown;
      schemaVersion?: unknown;
    };
    if (
      parsed.schemaVersion !== 2 ||
      !parsed.cassettes ||
      typeof parsed.cassettes !== "object" ||
      Array.isArray(parsed.cassettes)
    ) {
      return { cassettes: {}, schemaVersion: 2 };
    }
    return {
      cassettes: parsed.cassettes as ReplayControlDocument["cassettes"],
      schemaVersion: 2
    };
  } catch {
    return { cassettes: {}, schemaVersion: 2 };
  }
}

function isNonNegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0;
}
